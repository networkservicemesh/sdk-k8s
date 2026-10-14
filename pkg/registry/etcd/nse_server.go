// Copyright (c) 2020-2021 Doc.ai and/or its affiliates.
//
// Copyright (c) 2022-2024 Cisco and/or its affiliates.
//
// Copyright (c) 2025-2026 OpenInfra Foundation Europe. All rights reserved.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package etcd

import (
	"container/list"
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/edwarnicke/serialize"
	"github.com/golang/protobuf/ptypes/empty"

	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/networkservicemesh/api/pkg/api/registry"
	"github.com/networkservicemesh/sdk/pkg/registry/core/next"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/networkservicemesh/sdk/pkg/tools/matchutils"

	v1 "github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/apis/networkservicemesh.io/v1"
	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned"
)

// nseUpdateChannelSize is the per-subscriber watch buffer size.
//
// It is a deliberate trade-off, not an inherited default:
//   - Larger: tolerates bigger event bursts before a slow watcher is declared
//     lagged, but costs more memory per concurrent watcher (bounded worst case
//     = nseUpdateChannelSize * sizeof(response) * number of watchers).
//   - Smaller: cheaper per watcher, but a watcher is declared lagged (and its
//     stream terminated with codes.ResourceExhausted) after a smaller burst,
//     forcing it to re-issue Find and perform a full unpaginated List resync.
//
// 64 matches the historical value and is adequate for current workloads. Revisit
// once a client actually acts on ResourceExhausted: today nothing in the sdk
// re-issues Find on that code, so the "re-list to resync" half of the contract
// is not yet realized by callers (the server merely stops leaking and signals).
const nseUpdateChannelSize = 64

type etcdNSERegistryServer struct {
	chainContext   context.Context
	deleteExecutor serialize.Executor
	client         versioned.Interface
	ns             string

	subscribers         *list.List
	subscribersExecutor serialize.Executor

	updateChannelSize int
}

// NewNetworkServiceEndpointRegistryServer creates new registry.NetworkServiceRegistryServer that is using etcd to store network services.
func NewNetworkServiceEndpointRegistryServer(chainContext context.Context, ns string, client versioned.Interface) registry.NetworkServiceEndpointRegistryServer {
	ret := &etcdNSERegistryServer{
		chainContext:      chainContext,
		client:            client,
		ns:                ns,
		subscribers:       list.New(),
		updateChannelSize: nseUpdateChannelSize,
	}

	go ret.watchRemoteStorage()

	return ret
}

func (n *etcdNSERegistryServer) watchRemoteStorage() {
	const minSleepTime = time.Millisecond * 50
	const maxSleepTime = minSleepTime * 5

	sleepTime := minSleepTime
	logger := log.FromContext(n.chainContext).WithField("etcdNSERegistryServer", "watchRemoteStorage")

	for n.chainContext.Err() == nil {
		timeoutSeconds := int64(time.Minute / time.Second)
		watcher, err := n.client.NetworkservicemeshV1().NetworkServiceEndpoints("").Watch(n.chainContext, metav1.ListOptions{
			TimeoutSeconds: &timeoutSeconds,
		})
		if err != nil {
			time.Sleep(sleepTime)
			sleepTime += minSleepTime
			sleepTime = min(sleepTime, maxSleepTime)
			continue
		}
		sleepTime = minSleepTime

		isWatcherFine := true
		for isWatcherFine {
			select {
			case <-n.chainContext.Done():
				return
			case event, ok := <-watcher.ResultChan():
				if !ok {
					isWatcherFine = false
					break
				}
				deleted := event.Type == watch.Deleted
				model, ok := event.Object.(*v1.NetworkServiceEndpoint)
				if !ok {
					logger.Errorf("event: %v", event)
					continue
				}
				item := (*registry.NetworkServiceEndpoint)(&model.Spec)
				if item.Name == "" {
					item.Name = model.GetName()
				}
				resp := &registry.NetworkServiceEndpointResponse{
					NetworkServiceEndpoint: item,
					Deleted:                deleted,
				}
				n.subscribersExecutor.AsyncExec(func() { n.sendEvent(resp) })
				if !deleted && item.ExpirationTime != nil && item.ExpirationTime.AsTime().Local().Before(time.Now()) {
					n.deleteExecutor.AsyncExec(func() {
						_ = n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Delete(n.chainContext, item.GetName(), metav1.DeleteOptions{
							Preconditions: &metav1.Preconditions{
								ResourceVersion: &model.ResourceVersion,
							},
						})
					})
				}
			}
		}
		watcher.Stop()
	}
}

func (n *etcdNSERegistryServer) sendEvent(resp *registry.NetworkServiceEndpointResponse) {
	// INVARIANT: must run inside subscribersExecutor. A node's channel is removed
	// from the list and closed in a single subscribersExecutor job (see
	// subscribeOnEvents), so the serialized order guarantees sendEvent never sends
	// on a closed channel.
	for curr := n.subscribers.Front(); curr != nil; curr = curr.Next() {
		node := curr.Value.(*nseSubscriberNode)

		// Non-blocking send: never block, never remove
		// Mark subscriber as lagged if buffer full
		select {
		case node.channel <- resp:
		default:
			// Channel buffer full - subscriber fell behind
			// Mark as lagged; client will get explicit error on next read
			node.lagged.Store(true)
		}
	}
}

func (n *etcdNSERegistryServer) Register(ctx context.Context, request *registry.NetworkServiceEndpoint) (*registry.NetworkServiceEndpoint, error) {
	meta := metav1.ObjectMeta{
		GenerateName: "nse-",
		Name:         request.GetName(),
		Namespace:    n.ns,
	}
	apiResp, err := n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Create(
		ctx,
		&v1.NetworkServiceEndpoint{
			ObjectMeta: meta,
			Spec:       *(*v1.NetworkServiceEndpointSpec)(request),
		},
		metav1.CreateOptions{},
	)

	err = errors.Wrapf(err, "failed to create a nse %s in a namespace %s", request.Name, n.ns)

	if apierrors.IsAlreadyExists(err) {
		nse, nseErr := n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Get(ctx, request.GetName(), metav1.GetOptions{})
		if nseErr != nil {
			err = errors.Wrapf(err, "failed to get a nse %s in a namespace %s, reason: %v", request.Name, n.ns, nseErr.Error())
		}
		if nse != nil {
			nse.Spec = *(*v1.NetworkServiceEndpointSpec)(request)
			apiResp, err = n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Update(ctx, nse, metav1.UpdateOptions{})
		}
	}
	if err != nil {
		return nil, err
	}
	ctx = withNSEVersion(ctx, apiResp.ResourceVersion)
	return next.NetworkServiceEndpointRegistryServer(ctx).Register(ctx, request)
}

func (n *etcdNSERegistryServer) Find(query *registry.NetworkServiceEndpointQuery, s registry.NetworkServiceEndpointRegistry_FindServer) error {
	items, err := n.client.NetworkservicemeshV1().NetworkServiceEndpoints("").List(s.Context(), metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to get a list of NetworkServiceEndpoints")
	}
	for i := 0; i < len(items.Items); i++ {
		crd := &items.Items[i]
		nse := (*registry.NetworkServiceEndpoint)(&crd.Spec)
		if nse.Name == "" {
			nse.Name = items.Items[i].Name
		}
		if nse.ExpirationTime != nil && nse.ExpirationTime.AsTime().Local().Before(time.Now()) {
			n.deleteExecutor.AsyncExec(func() {
				_ = n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Delete(n.chainContext, nse.GetName(), metav1.DeleteOptions{
					Preconditions: &metav1.Preconditions{
						ResourceVersion: &crd.ResourceVersion,
					},
				})
			})
			continue
		}
		if matchutils.MatchNetworkServiceEndpoints(query.NetworkServiceEndpoint, nse) {
			err := s.Send(&registry.NetworkServiceEndpointResponse{NetworkServiceEndpoint: nse})
			if err != nil {
				return errors.Wrapf(err, "NetworkServiceEndpointRegistry find server failed to send a response %s", nse.String())
			}
		}
	}
	if query.Watch {
		var watchCtx, cancel = context.WithCancel(s.Context())
		defer cancel()
		if err := n.watch(watchCtx, query, s); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}
	return next.NetworkServiceEndpointRegistryServer(s.Context()).Find(query, s)
}

func (n *etcdNSERegistryServer) Unregister(ctx context.Context, request *registry.NetworkServiceEndpoint) (*empty.Empty, error) {
	resp, err := next.NetworkServiceEndpointRegistryServer(ctx).Unregister(ctx, request)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	var version *string
	if v, ok := nseVersionFromContext(ctx); ok {
		version = &v
	}

	err = n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Delete(
		ctx,
		request.Name,
		metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{
				ResourceVersion: version,
			},
		})
	if err != nil {
		log.FromContext(ctx).Warnf("failed to delete a NetworkServiceEndpoints %s in a namespace %s, cause: %v", request.Name, n.ns, err.Error())
	}

	return resp, nil
}

func (n *etcdNSERegistryServer) subscribeOnEvents(ctx context.Context) (node *nseSubscriberNode, ch <-chan *registry.NetworkServiceEndpointResponse) {
	ret := make(chan *registry.NetworkServiceEndpointResponse, n.updateChannelSize)
	node = &nseSubscriberNode{
		channel: ret,
	}
	var listNode *list.Element
	var wg sync.WaitGroup
	var closeOnce sync.Once

	wg.Add(1)
	n.subscribersExecutor.AsyncExec(func() {
		defer wg.Done()
		listNode = n.subscribers.PushBack(node)
	})
	wg.Wait() // Block until subscriber is registered

	// Cleanup on context cancel (normal client disconnect).
	// Remove and close run together inside subscribersExecutor so they are
	// serialized against sendEvent: once this job runs, the node is gone from
	// the list before the channel is closed, and no other sendEvent job runs
	// concurrently. This prevents a send on a closed channel.
	go func() {
		<-ctx.Done()
		n.subscribersExecutor.AsyncExec(func() {
			n.subscribers.Remove(listNode)
			closeOnce.Do(func() { close(ret) })
		})
	}()

	ch = ret
	return node, ch
}

// nseSubscriberNode wraps NetworkServiceEndpoint subscriber channel with lagged flag
type nseSubscriberNode struct {
	channel chan *registry.NetworkServiceEndpointResponse
	lagged  atomic.Bool
}

func (n *etcdNSERegistryServer) watch(ctx context.Context, q *registry.NetworkServiceEndpointQuery, s registry.NetworkServiceEndpointRegistry_FindServer) error {
	subNode, ch := n.subscribeOnEvents(ctx)

	for update := range ch {
		// Check if subscriber fell behind
		if subNode.lagged.Load() {
			// Signal the client to re-issue Find and re-list to resync (akin to
			// Kubernetes 410 Gone). NOTE: no sdk client acts on this code yet, so
			// the resync half of the contract is not realized by callers today;
			// the immediate benefit is that the server stops leaking memory for
			// stuck subscribers. See nseUpdateChannelSize for burst sizing.
			return status.Error(codes.ResourceExhausted, "subscriber fell behind; re-issue Find to resync")
		}

		if matchutils.MatchNetworkServiceEndpoints(q.GetNetworkServiceEndpoint(), update.GetNetworkServiceEndpoint()) {
			if err := s.Send(update); err != nil {
				return err
			}
		}
	}
	return nil
}

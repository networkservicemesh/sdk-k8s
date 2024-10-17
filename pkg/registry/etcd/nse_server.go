// Copyright (c) 2020-2021 Doc.ai and/or its affiliates.
//
// Copyright (c) 2022-2024 Cisco and/or its affiliates.
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
	"time"

	"github.com/edwarnicke/serialize"
	"github.com/golang/protobuf/ptypes/empty"

	"github.com/pkg/errors"
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
		updateChannelSize: 64,
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
	for curr := n.subscribers.Front(); curr != nil; curr = curr.Next() {
		curr.Value.(chan *registry.NetworkServiceEndpointResponse) <- resp
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

func (n *etcdNSERegistryServer) subscribeOnEvents(ctx context.Context) <-chan *registry.NetworkServiceEndpointResponse {
	var ret = make(chan *registry.NetworkServiceEndpointResponse, n.updateChannelSize)
	var node *list.Element

	n.subscribersExecutor.AsyncExec(func() {
		node = n.subscribers.PushBack(ret)
	})

	go func() {
		<-ctx.Done()

		n.subscribersExecutor.AsyncExec(func() {
			n.subscribers.Remove(node)
			close(ret)
		})

	}()

	return ret
}

func (n *etcdNSERegistryServer) watch(ctx context.Context, q *registry.NetworkServiceEndpointQuery, s registry.NetworkServiceEndpointRegistry_FindServer) error {
	for update := range n.subscribeOnEvents(ctx) {
		if matchutils.MatchNetworkServiceEndpoints(q.GetNetworkServiceEndpoint(), update.GetNetworkServiceEndpoint()) {
			if err := s.Send(update); err != nil {
				return err
			}
		}
	}
	return nil
}

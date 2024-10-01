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
	"sync"
	"time"

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

type etcdNSRegistryServer struct {
	chainContext context.Context
	client       versioned.Interface
	ns           string

	subscribers      *list.List
	subscribersMutex sync.Mutex

	updateChannelSize int
}

// NewNetworkServiceRegistryServer creates new registry.NetworkServiceRegistryServer that is using etcd to store network services.
func NewNetworkServiceRegistryServer(chainContext context.Context, ns string, client versioned.Interface) registry.NetworkServiceRegistryServer {
	ret := &etcdNSRegistryServer{
		chainContext:      chainContext,
		client:            client,
		ns:                ns,
		subscribers:       list.New(),
		updateChannelSize: 64,
	}

	go ret.watchRemoteStorage()

	return ret
}

func (n *etcdNSRegistryServer) watchRemoteStorage() {
	const minSleepTime = 10 * time.Millisecond
	const maxSleepTime = 250 * time.Millisecond

	sleepTime := minSleepTime
	logger := log.FromContext(n.chainContext).WithField("etcdNSRegistryServer", "watchRemoteStorage")

	for n.chainContext.Err() == nil {
		timeoutSeconds := int64(time.Minute / time.Second)
		watcher, err := n.client.NetworkservicemeshV1().NetworkServices("").Watch(n.chainContext, metav1.ListOptions{
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
				model, ok := event.Object.(*v1.NetworkService)
				if !ok {
					logger.Errorf("event: %v", event)
					continue
				}
				item := (*registry.NetworkService)(&model.Spec)
				if item.Name == "" {
					item.Name = model.GetName()
				}
				resp := &registry.NetworkServiceResponse{
					NetworkService: item,
					Deleted:        deleted,
				}
				n.sendEvent(resp)
			}
		}
		watcher.Stop()
	}
}

func (n *etcdNSRegistryServer) sendEvent(resp *registry.NetworkServiceResponse) {
	n.subscribersMutex.Lock()
	for curr := n.subscribers.Front(); curr != nil; curr = curr.Next() {
		select {
		case curr.Value.(chan *registry.NetworkServiceResponse) <- resp:
		default:
		}
	}
	n.subscribersMutex.Unlock()
}

func (n *etcdNSRegistryServer) Register(ctx context.Context, request *registry.NetworkService) (*registry.NetworkService, error) {
	meta := metav1.ObjectMeta{
		GenerateName: "netsvc-",
		Name:         request.GetName(),
		Namespace:    n.ns,
	}
	_, err := n.client.NetworkservicemeshV1().NetworkServices(n.ns).Create(
		ctx,
		&v1.NetworkService{
			ObjectMeta: meta,
			Spec:       *(*v1.NetworkServiceSpec)(request),
		},
		metav1.CreateOptions{},
	)

	err = errors.Wrapf(err, "failed to create a netsvc %s in a namespace %s", request.Name, n.ns)

	if apierrors.IsAlreadyExists(err) {
		netsvc, nseErr := n.client.NetworkservicemeshV1().NetworkServices(n.ns).Get(ctx, request.GetName(), metav1.GetOptions{})
		if nseErr != nil {
			err = errors.Wrapf(err, "failed to get a netsvc %s in a namespace %s, reason: %v", request.Name, n.ns, nseErr.Error())
		}
		if netsvc != nil {
			netsvc.Spec = *(*v1.NetworkServiceSpec)(request)
			_, err = n.client.NetworkservicemeshV1().NetworkServices(n.ns).Update(ctx, netsvc, metav1.UpdateOptions{})
			if err != nil {
				return nil, errors.Wrapf(err, "failed to update a pod %s in a namespace %s", netsvc.Name, n.ns)
			}
			return next.NetworkServiceRegistryServer(ctx).Register(ctx, request)
		}
	}
	if err != nil {
		return nil, err
	}

	return next.NetworkServiceRegistryServer(ctx).Register(ctx, request)
}

func (n *etcdNSRegistryServer) Find(query *registry.NetworkServiceQuery, s registry.NetworkServiceRegistry_FindServer) error {
	items, err := n.client.NetworkservicemeshV1().NetworkServices("").List(s.Context(), metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to get a list of NetworkServices")
	}
	for i := 0; i < len(items.Items); i++ {
		crd := &items.Items[i]
		netsvc := (*registry.NetworkService)(&crd.Spec)
		if netsvc.Name == "" {
			netsvc.Name = items.Items[i].Name
		}
		if matchutils.MatchNetworkServices(query.NetworkService, netsvc) {
			err := s.Send(&registry.NetworkServiceResponse{NetworkService: netsvc})
			if err != nil {
				return errors.Wrapf(err, "NetworkServiceRegistry find server failed to send a response %s", netsvc.String())
			}
		}
	}
	if query.Watch {
		if err := n.watch(s.Context(), query, s); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}
	return next.NetworkServiceRegistryServer(s.Context()).Find(query, s)
}

func (n *etcdNSRegistryServer) Unregister(ctx context.Context, request *registry.NetworkService) (*empty.Empty, error) {
	resp, err := next.NetworkServiceRegistryServer(ctx).Unregister(ctx, request)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	err = n.client.NetworkservicemeshV1().NetworkServices(n.ns).Delete(
		ctx,
		request.Name,
		metav1.DeleteOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to delete a NetworkServices %s in a namespace %s", request.Name, n.ns)
	}
	return resp, nil
}

func (n *etcdNSRegistryServer) subscribeOnEvents(ctx context.Context) <-chan *registry.NetworkServiceResponse {
	var ret = make(chan *registry.NetworkServiceResponse, n.updateChannelSize)

	n.subscribersMutex.Lock()
	var node = n.subscribers.PushBack(ret)
	n.subscribersMutex.Unlock()

	go func() {
		<-ctx.Done()

		n.subscribersMutex.Lock()
		n.subscribers.Remove(node)
		n.subscribersMutex.Unlock()

		close(ret)
	}()

	return ret
}

func (n *etcdNSRegistryServer) watch(ctx context.Context, q *registry.NetworkServiceQuery, s registry.NetworkServiceRegistry_FindServer) error {
	for update := range n.subscribeOnEvents(ctx) {
		if matchutils.MatchNetworkServices(q.GetNetworkService(), update.GetNetworkService()) {
			if err := s.Send(update); err != nil {
				return err
			}
		}
	}
	return nil
}

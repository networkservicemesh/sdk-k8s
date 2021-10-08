// Copyright (c) 2020-2021 Doc.ai and/or its affiliates.
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
	"context"
	"errors"
	"io"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/networkservicemesh/api/pkg/api/registry"
	"github.com/networkservicemesh/sdk/pkg/registry/core/next"
	"github.com/networkservicemesh/sdk/pkg/tools/matchutils"

	v1 "github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/apis/networkservicemesh.io/v1"
	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned"
)

type etcdNSRegistryServer struct {
	chainContext context.Context
	client       versioned.Interface
	ns           string
}

func (n *etcdNSRegistryServer) Register(ctx context.Context, request *registry.NetworkService) (*registry.NetworkService, error) {
	resp, err := next.NetworkServiceRegistryServer(ctx).Register(ctx, request)
	if err != nil {
		return nil, err
	}
	apiResp, err := n.client.NetworkservicemeshV1().NetworkServices(n.ns).Create(
		ctx,
		&v1.NetworkService{
			ObjectMeta: metav1.ObjectMeta{
				Name: request.Name,
			},
			Spec: *(*v1.NetworkServiceSpec)(resp),
		},
		metav1.CreateOptions{},
	)
	if apierrors.IsAlreadyExists(err) {
		var exist *v1.NetworkService
		exist, err = n.client.NetworkservicemeshV1().NetworkServices("").Get(ctx, request.Name, metav1.GetOptions{})
		if err == nil {
			exist.Spec = *(*v1.NetworkServiceSpec)(request)
			apiResp, err = n.client.NetworkservicemeshV1().NetworkServices(n.ns).Update(ctx, exist, metav1.UpdateOptions{})
		}
	}
	if err != nil {
		return nil, err
	}
	apiResp.Spec.DeepCopyInto((*v1.NetworkServiceSpec)(request))
	request.Name = apiResp.Name

	return (*registry.NetworkService)(&apiResp.Spec), nil
}

func (n *etcdNSRegistryServer) watch(query *registry.NetworkServiceQuery, s registry.NetworkServiceRegistry_FindServer) error {
	var watchErr error
	for watchErr == nil {
		timeoutSeconds := int64(time.Minute / time.Second)
		watcher, err := n.client.NetworkservicemeshV1().NetworkServices("").Watch(s.Context(), metav1.ListOptions{
			TimeoutSeconds: &timeoutSeconds,
		})
		if err != nil {
			return err
		}

		watchErr = n.handleWatcher(watcher, query, s)

		watcher.Stop()
	}
	return watchErr
}

func (n *etcdNSRegistryServer) handleWatcher(
	watcher watch.Interface,
	query *registry.NetworkServiceQuery,
	s registry.NetworkServiceRegistry_FindServer,
) error {
	logger := log.FromContext(n.chainContext).WithField("etcdNSRegistryServer", "handleWatcher")

	var event watch.Event
	for watcherOpened := true; watcherOpened; {
		select {
		case <-s.Context().Done():
			return s.Context().Err()
		case event, watcherOpened = <-watcher.ResultChan():
			if !watcherOpened {
				logger.Warn("watcher is closed, retrying")
				continue
			}
			if event.Type != watch.Added {
				continue
			}
			model := event.Object.(*v1.NetworkService)
			item := (*registry.NetworkService)(&model.Spec)
			if matchutils.MatchNetworkServices(query.NetworkService, item) {
				err := s.Send(item)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (n *etcdNSRegistryServer) Find(query *registry.NetworkServiceQuery, s registry.NetworkServiceRegistry_FindServer) error {
	list, err := n.client.NetworkservicemeshV1().NetworkServices("").List(s.Context(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	for i := 0; i < len(list.Items); i++ {
		item := (*registry.NetworkService)(&list.Items[i].Spec)
		if item.Name == "" {
			item.Name = list.Items[i].Name
		}
		if matchutils.MatchNetworkServices(query.NetworkService, item) {
			err := s.Send(item)
			if err != nil {
				return err
			}
		}
	}
	if query.Watch {
		if err := n.watch(query, s); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}
	return next.NetworkServiceRegistryServer(s.Context()).Find(query, s)
}

func (n *etcdNSRegistryServer) Unregister(ctx context.Context, request *registry.NetworkService) (*empty.Empty, error) {
	resp, err := next.NetworkServiceRegistryServer(ctx).Unregister(ctx, request)
	if err != nil {
		return nil, err
	}
	err = n.client.NetworkservicemeshV1().NetworkServices("").Delete(
		ctx,
		request.Name,
		metav1.DeleteOptions{},
	)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// NewNetworkServiceRegistryServer creates new registry.NetworkServiceRegistryServer that is using etcd to store network services.
func NewNetworkServiceRegistryServer(chainContext context.Context, ns string, client versioned.Interface) registry.NetworkServiceRegistryServer {
	return &etcdNSRegistryServer{
		chainContext: chainContext,
		client:       client,
		ns:           ns,
	}
}

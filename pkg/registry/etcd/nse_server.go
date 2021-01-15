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

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/networkservicemesh/api/pkg/api/registry"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	v1 "github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/apis/networkservicemesh.io/v1"
	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned"
	"github.com/networkservicemesh/sdk/pkg/registry/core/next"
	"github.com/networkservicemesh/sdk/pkg/tools/matchutils"
)

type etcdNSERegistryServer struct {
	chainContext context.Context
	client       versioned.Interface
	ns           string
}

func (n *etcdNSERegistryServer) Register(ctx context.Context, request *registry.NetworkServiceEndpoint) (*registry.NetworkServiceEndpoint, error) {
	resp, err := next.NetworkServiceEndpointRegistryServer(ctx).Register(ctx, request)
	if err != nil {
		return nil, err
	}
	meta := metav1.ObjectMeta{}
	if request.Name == "" {
		meta.GenerateName = "nse-"
	} else {
		meta.Name = request.Name
	}
	apiResp, err := n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Create(
		ctx,
		&v1.NetworkServiceEndpoint{
			ObjectMeta: meta,
			Spec:       *(*v1.NetworkServiceEndpointSpec)(resp),
		},
		metav1.CreateOptions{},
	)
	if apierrors.IsAlreadyExists(err) {
		var exist *v1.NetworkServiceEndpoint
		exist, err = n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Get(ctx, request.Name, metav1.GetOptions{})
		if err == nil {
			exist.Spec = *(*v1.NetworkServiceEndpointSpec)(request)
			apiResp, err = n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Update(ctx, exist, metav1.UpdateOptions{})
		}
	}
	if err != nil {
		return nil, err
	}

	return (*registry.NetworkServiceEndpoint)(&apiResp.Spec), nil
}

func (n *etcdNSERegistryServer) Find(query *registry.NetworkServiceEndpointQuery, s registry.NetworkServiceEndpointRegistry_FindServer) error {
	list, err := n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).List(s.Context(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	for i := 0; i < len(list.Items); i++ {
		item := (*registry.NetworkServiceEndpoint)(&list.Items[i].Spec)
		if matchutils.MatchNetworkServiceEndpoints(query.NetworkServiceEndpoint, item) {
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
	return next.NetworkServiceEndpointRegistryServer(s.Context()).Find(query, s)
}

func (n *etcdNSERegistryServer) Unregister(ctx context.Context, request *registry.NetworkServiceEndpoint) (*empty.Empty, error) {
	resp, err := next.NetworkServiceEndpointRegistryServer(ctx).Unregister(ctx, request)
	if err != nil {
		return nil, err
	}
	err = n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Delete(
		ctx,
		request.Name,
		metav1.DeleteOptions{})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (n *etcdNSERegistryServer) watch(query *registry.NetworkServiceEndpointQuery, s registry.NetworkServiceEndpointRegistry_FindServer) error {
	watcher, err := n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Watch(s.Context(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	for {
		select {
		case <-n.chainContext.Done():
			return n.chainContext.Err()
		case <-s.Context().Done():
			return s.Context().Err()
		case event := <-watcher.ResultChan():
			model := event.Object.(*v1.NetworkServiceEndpoint)
			item := (*registry.NetworkServiceEndpoint)(&model.Spec)
			if event.Type == watch.Deleted {
				item.ExpirationTime.Seconds = -1
			}
			if matchutils.MatchNetworkServiceEndpoints(query.NetworkServiceEndpoint, item) {
				err := s.Send(item)
				if err != nil {
					return err
				}
			}
		}
	}
}

// NewNetworkServiceEndpointRegistryServer creates new registry.NetworkServiceRegistryServer that is using etcd to store network services.
func NewNetworkServiceEndpointRegistryServer(chainContext context.Context, ns string, client versioned.Interface) registry.NetworkServiceEndpointRegistryServer {
	return &etcdNSERegistryServer{
		chainContext: chainContext,
		client:       client,
		ns:           ns,
	}
}

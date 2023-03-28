// Copyright (c) 2020-2021 Doc.ai and/or its affiliates.
//
// Copyright (c) 2022-2023 Cisco and/or its affiliates.
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
	"io"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/networkservicemesh/api/pkg/api/registry"
	"github.com/networkservicemesh/sdk/pkg/registry/core/next"
	"github.com/networkservicemesh/sdk/pkg/tools/matchutils"

	v1 "github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/apis/networkservicemesh.io/v1"
	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned"
)

type etcdNSERegistryServer struct {
	chainContext context.Context
	client       versioned.Interface
	versions     sync.Map
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
	err = errors.Wrapf(err, "failed to create a pod %s in a namespace %s", resp.Name, n.ns)
	if apierrors.IsAlreadyExists(err) {
		var nse *v1.NetworkServiceEndpoint
		list, erro := n.client.NetworkservicemeshV1().NetworkServiceEndpoints("").List(ctx, metav1.ListOptions{})
		if erro != nil {
			return nil, errors.Wrap(erro, "failed to get a list of NetworkServiceEndpoints")
		}
		for i := 0; i < len(list.Items); i++ {
			item := (*registry.NetworkServiceEndpoint)(&list.Items[i].Spec)
			if item.Name == "" {
				item.Name = list.Items[i].Name
			}
			if request.Name == item.Name {
				list.Items[i].Spec = *(*v1.NetworkServiceEndpointSpec)(request)
				nse = &list.Items[i]
			}
		}

		if nse != nil {
			apiResp, err = n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Update(ctx, nse, metav1.UpdateOptions{})
			err = errors.Wrapf(err, "failed to update a pod %s in a namespace %s", nse.Name, n.ns)
		}
	}
	if err != nil {
		return nil, err
	}

	n.versions.Store(apiResp.Spec.Name, apiResp.ResourceVersion)

	return (*registry.NetworkServiceEndpoint)(&apiResp.Spec), nil
}

func (n *etcdNSERegistryServer) Find(query *registry.NetworkServiceEndpointQuery, s registry.NetworkServiceEndpointRegistry_FindServer) error {
	list, err := n.client.NetworkservicemeshV1().NetworkServiceEndpoints("").List(s.Context(), metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to get a list of NetworkServiceEndpoints")
	}
	for i := 0; i < len(list.Items); i++ {
		item := (*registry.NetworkServiceEndpoint)(&list.Items[i].Spec)
		if item.Name == "" {
			item.Name = list.Items[i].Name
		}
		if item.ExpirationTime != nil && item.ExpirationTime.AsTime().Local().Before(time.Now()) {
			go func() {
				_ = n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Delete(n.chainContext, item.Name, metav1.DeleteOptions{
					Preconditions: &metav1.Preconditions{
						ResourceVersion: &list.Items[i].ResourceVersion,
					},
				})
			}()
			continue
		}
		if matchutils.MatchNetworkServiceEndpoints(query.NetworkServiceEndpoint, item) {
			err := s.Send(&registry.NetworkServiceEndpointResponse{NetworkServiceEndpoint: item})
			if err != nil {
				return errors.Wrapf(err, "NetworkServiceEndpointRegistry find server failed to send a response %s", item.String())
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
		return nil, errors.WithStack(err)
	}

	if v, ok := n.versions.Load(request.Name); ok {
		version := v.(string)
		err = n.client.NetworkservicemeshV1().NetworkServiceEndpoints(n.ns).Delete(
			ctx,
			request.Name,
			metav1.DeleteOptions{
				Preconditions: &metav1.Preconditions{
					ResourceVersion: &version,
				},
			})
		if err != nil {
			return nil, errors.Wrapf(err, "failed to delete a NetworkServiceEndpoints %s in a namespace %s", request.Name, n.ns)
		}
	}
	return resp, nil
}

func (n *etcdNSERegistryServer) watch(query *registry.NetworkServiceEndpointQuery, s registry.NetworkServiceEndpointRegistry_FindServer) error {
	logger := log.FromContext(n.chainContext).WithField("etcdNSERegistryServer", "watch")
	var watchErr error
	for watchErr == nil {
		timeoutSeconds := int64(time.Minute / time.Second)
		watcher, err := n.client.NetworkservicemeshV1().NetworkServiceEndpoints("").Watch(s.Context(), metav1.ListOptions{
			TimeoutSeconds: &timeoutSeconds,
		})
		if err != nil {
			return errors.Wrap(err, "failed to get a watch.Interface for a requested networkServiceEndpoints")
		}

		watchErr = n.handleWatcher(watcher, query, s)

		watcher.Stop()
	}

	// If the watch timed out, return nil to close the stream
	// cleanly.
	if errors.Is(watchErr, context.Canceled) {
		logger.Debug("watch timed out, returning nil")
		return nil
	}
	// If something else went wrong, return the error.
	return watchErr
}

func (n *etcdNSERegistryServer) handleWatcher(
	watcher watch.Interface,
	query *registry.NetworkServiceEndpointQuery,
	s registry.NetworkServiceEndpointRegistry_FindServer,
) error {
	logger := log.FromContext(n.chainContext).WithField("etcdNSERegistryServer", "handleWatcher")

	var event watch.Event
	for watcherOpened := true; watcherOpened; {
		select {
		case <-n.chainContext.Done():
			return errors.Wrap(n.chainContext.Err(), "application context is done")
		case <-s.Context().Done():
			return errors.Wrap(s.Context().Err(), "find context is done")
		case event, watcherOpened = <-watcher.ResultChan():
			if !watcherOpened {
				logger.Warn("watcher is closed, retrying")
				continue
			}
			model, ok := event.Object.(*v1.NetworkServiceEndpoint)
			if !ok {
				logger.Errorf("event: %v", event)
				continue
			}
			item := (*registry.NetworkServiceEndpoint)(&model.Spec)

			nseResp := &registry.NetworkServiceEndpointResponse{NetworkServiceEndpoint: item}
			if event.Type == watch.Deleted {
				nseResp.Deleted = true
			}

			if matchutils.MatchNetworkServiceEndpoints(query.NetworkServiceEndpoint, item) {
				err := s.Send(nseResp)
				if err != nil {
					return errors.Wrapf(err, "NetworkServiceEndpointRegistry find server failed to send a response %s", nseResp.String())
				}
			}
		}
	}
	return nil
}

// NewNetworkServiceEndpointRegistryServer creates new registry.NetworkServiceRegistryServer that is using etcd to store network services.
func NewNetworkServiceEndpointRegistryServer(chainContext context.Context, ns string, client versioned.Interface) registry.NetworkServiceEndpointRegistryServer {
	return &etcdNSERegistryServer{
		chainContext: chainContext,
		client:       client,
		ns:           ns,
	}
}

// Copyright (c) 2020 Doc.ai and/or its affiliates.
//
// Copyright (c) 2020 Cisco and/or its affiliates.
//
// Copyright (c) 2020 VMware, Inc.
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

// Code generated by informer-gen. DO NOT EDIT.

package v1

import (
	"context"
	time "time"

	networkservicemeshiov1 "github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/apis/networkservicemesh.io/v1"
	versioned "github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned"
	internalinterfaces "github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/informers/externalversions/internalinterfaces"
	v1 "github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/listers/networkservicemesh.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// NetworkServiceEndpointInformer provides access to a shared informer and lister for
// NetworkServiceEndpoints.
type NetworkServiceEndpointInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.NetworkServiceEndpointLister
}

type networkServiceEndpointInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewNetworkServiceEndpointInformer constructs a new informer for NetworkServiceEndpoint type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewNetworkServiceEndpointInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredNetworkServiceEndpointInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredNetworkServiceEndpointInformer constructs a new informer for NetworkServiceEndpoint type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredNetworkServiceEndpointInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.NetworkservicemeshV1().NetworkServiceEndpoints(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.NetworkservicemeshV1().NetworkServiceEndpoints(namespace).Watch(context.TODO(), options)
			},
		},
		&networkservicemeshiov1.NetworkServiceEndpoint{},
		resyncPeriod,
		indexers,
	)
}

func (f *networkServiceEndpointInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredNetworkServiceEndpointInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *networkServiceEndpointInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&networkservicemeshiov1.NetworkServiceEndpoint{}, f.defaultInformer)
}

func (f *networkServiceEndpointInformer) Lister() v1.NetworkServiceEndpointLister {
	return v1.NewNetworkServiceEndpointLister(f.Informer().GetIndexer())
}

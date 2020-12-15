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

// Code generated by lister-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/apis/networkservicemesh.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// NetworkServiceEndpointLister helps list NetworkServiceEndpoints.
// All objects returned here must be treated as read-only.
type NetworkServiceEndpointLister interface {
	// List lists all NetworkServiceEndpoints in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1.NetworkServiceEndpoint, err error)
	// NetworkServiceEndpoints returns an object that can list and get NetworkServiceEndpoints.
	NetworkServiceEndpoints(namespace string) NetworkServiceEndpointNamespaceLister
	NetworkServiceEndpointListerExpansion
}

// networkServiceEndpointLister implements the NetworkServiceEndpointLister interface.
type networkServiceEndpointLister struct {
	indexer cache.Indexer
}

// NewNetworkServiceEndpointLister returns a new NetworkServiceEndpointLister.
func NewNetworkServiceEndpointLister(indexer cache.Indexer) NetworkServiceEndpointLister {
	return &networkServiceEndpointLister{indexer: indexer}
}

// List lists all NetworkServiceEndpoints in the indexer.
func (s *networkServiceEndpointLister) List(selector labels.Selector) (ret []*v1.NetworkServiceEndpoint, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.NetworkServiceEndpoint))
	})
	return ret, err
}

// NetworkServiceEndpoints returns an object that can list and get NetworkServiceEndpoints.
func (s *networkServiceEndpointLister) NetworkServiceEndpoints(namespace string) NetworkServiceEndpointNamespaceLister {
	return networkServiceEndpointNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// NetworkServiceEndpointNamespaceLister helps list and get NetworkServiceEndpoints.
// All objects returned here must be treated as read-only.
type NetworkServiceEndpointNamespaceLister interface {
	// List lists all NetworkServiceEndpoints in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1.NetworkServiceEndpoint, err error)
	// Get retrieves the NetworkServiceEndpoint from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1.NetworkServiceEndpoint, error)
	NetworkServiceEndpointNamespaceListerExpansion
}

// networkServiceEndpointNamespaceLister implements the NetworkServiceEndpointNamespaceLister
// interface.
type networkServiceEndpointNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all NetworkServiceEndpoints in the indexer for a given namespace.
func (s networkServiceEndpointNamespaceLister) List(selector labels.Selector) (ret []*v1.NetworkServiceEndpoint, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.NetworkServiceEndpoint))
	})
	return ret, err
}

// Get retrieves the NetworkServiceEndpoint from the indexer for a given namespace and name.
func (s networkServiceEndpointNamespaceLister) Get(name string) (*v1.NetworkServiceEndpoint, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1.Resource("networkserviceendpoint"), name)
	}
	return obj.(*v1.NetworkServiceEndpoint), nil
}

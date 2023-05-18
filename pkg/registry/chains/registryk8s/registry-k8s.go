// Copyright (c) 2020-2022 Doc.ai and/or its affiliates.
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

package registryk8s

import (
	"context"
	"net/url"
	"time"

	"google.golang.org/grpc"

	"github.com/networkservicemesh/api/pkg/api/registry"
	registryserver "github.com/networkservicemesh/sdk/pkg/registry"
	registryauthorize "github.com/networkservicemesh/sdk/pkg/registry/common/authorize"
	"github.com/networkservicemesh/sdk/pkg/registry/common/clientconn"
	"github.com/networkservicemesh/sdk/pkg/registry/common/clienturl"
	"github.com/networkservicemesh/sdk/pkg/registry/common/connect"
	"github.com/networkservicemesh/sdk/pkg/registry/common/dial"
	"github.com/networkservicemesh/sdk/pkg/registry/common/expire"
	"github.com/networkservicemesh/sdk/pkg/registry/common/grpcmetadata"
	"github.com/networkservicemesh/sdk/pkg/registry/common/setpayload"
	"github.com/networkservicemesh/sdk/pkg/registry/common/setregistrationtime"
	"github.com/networkservicemesh/sdk/pkg/registry/common/updatepath"
	"github.com/networkservicemesh/sdk/pkg/registry/core/chain"
	"github.com/networkservicemesh/sdk/pkg/registry/switchcase"
	"github.com/networkservicemesh/sdk/pkg/registry/utils/metadata"
	"github.com/networkservicemesh/sdk/pkg/tools/interdomain"
	"github.com/networkservicemesh/sdk/pkg/tools/token"

	"github.com/networkservicemesh/sdk/pkg/registry/common/begin"

	"github.com/networkservicemesh/sdk-k8s/pkg/registry/etcd"
	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned"
)

// Config contains configuration parameters for registry.Registry based on k8s client
type Config struct {
	Namespace        string        `default:"default" desc:"namespace where is deployed registry-k8s instance" split_words:"true"`
	ProxyRegistryURL *url.URL      `desc:"url to the proxy registry that handles this domain" split_words:"true"`
	ExpirePeriod     time.Duration `default:"1m" desc:"period to check expired NSEs" split_words:"true"`
	ChainCtx         context.Context
	ClientSet        versioned.Interface
}

type serverOptions struct {
	authorizeNSRegistryServer  registry.NetworkServiceRegistryServer
	authorizeNSRegistryClient  registry.NetworkServiceRegistryClient
	authorizeNSERegistryServer registry.NetworkServiceEndpointRegistryServer
	authorizeNSERegistryClient registry.NetworkServiceEndpointRegistryClient
	dialOptions                []grpc.DialOption
}

// Option modifies server option value
type Option func(o *serverOptions)

// WithDialOptions sets grpc.DialOptions for the client
func WithDialOptions(dialOptions ...grpc.DialOption) Option {
	return func(o *serverOptions) {
		o.dialOptions = dialOptions
	}
}

// WithAuthorizeNSRegistryServer sets server authorization NetworkServiceRegistry chain element
func WithAuthorizeNSRegistryServer(authorizeNSRegistryServer registry.NetworkServiceRegistryServer) Option {
	if authorizeNSRegistryServer == nil {
		panic("authorizeNSRegistryServer cannot be nil")
	}
	return func(o *serverOptions) {
		o.authorizeNSRegistryServer = authorizeNSRegistryServer
	}
}

// WithAuthorizeNSERegistryServer sets server authorization NetworkServiceEndpointRegistry chain element
func WithAuthorizeNSERegistryServer(authorizeNSERegistryServer registry.NetworkServiceEndpointRegistryServer) Option {
	if authorizeNSERegistryServer == nil {
		panic("authorizeNSERegistryServer cannot be nil")
	}
	return func(o *serverOptions) {
		o.authorizeNSERegistryServer = authorizeNSERegistryServer
	}
}

// WithAuthorizeNSRegistryClient sets client authorization NetworkServiceRegistry chain element
func WithAuthorizeNSRegistryClient(authorizeNSRegistryClient registry.NetworkServiceRegistryClient) Option {
	if authorizeNSRegistryClient == nil {
		panic("authorizeNSRegistryClient cannot be nil")
	}
	return func(o *serverOptions) {
		o.authorizeNSRegistryClient = authorizeNSRegistryClient
	}
}

// WithAuthorizeNSERegistryClient sets client authorization NetworkServiceEndpointRegistry chain element
func WithAuthorizeNSERegistryClient(authorizeNSERegistryClient registry.NetworkServiceEndpointRegistryClient) Option {
	if authorizeNSERegistryClient == nil {
		panic("authorizeNSERegistryClient cannot be nil")
	}
	return func(o *serverOptions) {
		o.authorizeNSERegistryClient = authorizeNSERegistryClient
	}
}

// NewServer creates new registry server based on k8s etcd db storage
func NewServer(config *Config, tokenGenerator token.GeneratorFunc, options ...Option) registryserver.Registry {
	opts := &serverOptions{
		authorizeNSRegistryServer:  registryauthorize.NewNetworkServiceRegistryServer(registryauthorize.Any()),
		authorizeNSERegistryServer: registryauthorize.NewNetworkServiceEndpointRegistryServer(registryauthorize.Any()),
		authorizeNSRegistryClient:  registryauthorize.NewNetworkServiceRegistryClient(registryauthorize.Any()),
		authorizeNSERegistryClient: registryauthorize.NewNetworkServiceEndpointRegistryClient(registryauthorize.Any()),
	}
	for _, opt := range options {
		opt(opts)
	}

	nseChain := chain.NewNetworkServiceEndpointRegistryServer(
		grpcmetadata.NewNetworkServiceEndpointRegistryServer(),
		updatepath.NewNetworkServiceEndpointRegistryServer(tokenGenerator),
		opts.authorizeNSERegistryServer,
		begin.NewNetworkServiceEndpointRegistryServer(),
		metadata.NewNetworkServiceEndpointServer(),
		switchcase.NewNetworkServiceEndpointRegistryServer(switchcase.NSEServerCase{
			Condition: func(c context.Context, nse *registry.NetworkServiceEndpoint) bool {
				if interdomain.Is(nse.GetName()) {
					return true
				}
				for _, ns := range nse.GetNetworkServiceNames() {
					if interdomain.Is(ns) {
						return true
					}
				}
				return false
			},
			Action: chain.NewNetworkServiceEndpointRegistryServer(
				connect.NewNetworkServiceEndpointRegistryServer(
					chain.NewNetworkServiceEndpointRegistryClient(
						begin.NewNetworkServiceEndpointRegistryClient(),
						clienturl.NewNetworkServiceEndpointRegistryClient(config.ProxyRegistryURL),
						clientconn.NewNetworkServiceEndpointRegistryClient(),
						opts.authorizeNSERegistryClient,
						grpcmetadata.NewNetworkServiceEndpointRegistryClient(),
						dial.NewNetworkServiceEndpointRegistryClient(config.ChainCtx,
							dial.WithDialOptions(opts.dialOptions...),
						),
						connect.NewNetworkServiceEndpointRegistryClient(),
					),
				),
			),
		},
			switchcase.NSEServerCase{
				Condition: func(c context.Context, nse *registry.NetworkServiceEndpoint) bool { return true },
				Action: chain.NewNetworkServiceEndpointRegistryServer(
					setregistrationtime.NewNetworkServiceEndpointRegistryServer(),
					expire.NewNetworkServiceEndpointRegistryServer(config.ChainCtx, expire.WithDefaultExpiration(config.ExpirePeriod)),
					etcd.NewNetworkServiceEndpointRegistryServer(config.ChainCtx, config.Namespace, config.ClientSet),
				),
			},
		),
	)
	nsChain := chain.NewNetworkServiceRegistryServer(
		grpcmetadata.NewNetworkServiceRegistryServer(),
		updatepath.NewNetworkServiceRegistryServer(tokenGenerator),
		opts.authorizeNSRegistryServer,
		metadata.NewNetworkServiceServer(),
		setpayload.NewNetworkServiceRegistryServer(),
		switchcase.NewNetworkServiceRegistryServer(
			switchcase.NSServerCase{
				Condition: func(c context.Context, ns *registry.NetworkService) bool {
					return interdomain.Is(ns.GetName())
				},
				Action: connect.NewNetworkServiceRegistryServer(
					chain.NewNetworkServiceRegistryClient(
						clienturl.NewNetworkServiceRegistryClient(config.ProxyRegistryURL),
						begin.NewNetworkServiceRegistryClient(),
						clientconn.NewNetworkServiceRegistryClient(),
						opts.authorizeNSRegistryClient,
						grpcmetadata.NewNetworkServiceRegistryClient(),
						dial.NewNetworkServiceRegistryClient(config.ChainCtx,
							dial.WithDialOptions(opts.dialOptions...),
						),
						connect.NewNetworkServiceRegistryClient(),
					),
				),
			},
			switchcase.NSServerCase{
				Condition: func(c context.Context, ns *registry.NetworkService) bool {
					return true
				},
				Action: etcd.NewNetworkServiceRegistryServer(config.ChainCtx, config.Namespace, config.ClientSet),
			},
		),
	)

	return registryserver.NewServer(nsChain, nseChain)
}

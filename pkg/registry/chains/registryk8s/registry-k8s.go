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

package registryk8s

import (
	"context"
	"net/url"
	"time"

	"google.golang.org/grpc"

	"github.com/networkservicemesh/api/pkg/api/registry"
	registryserver "github.com/networkservicemesh/sdk/pkg/registry"
	"github.com/networkservicemesh/sdk/pkg/registry/common/clientconn"
	"github.com/networkservicemesh/sdk/pkg/registry/common/clienturl"
	"github.com/networkservicemesh/sdk/pkg/registry/common/connect"
	"github.com/networkservicemesh/sdk/pkg/registry/common/dial"
	"github.com/networkservicemesh/sdk/pkg/registry/common/expire"
	"github.com/networkservicemesh/sdk/pkg/registry/common/setpayload"
	"github.com/networkservicemesh/sdk/pkg/registry/common/setregistrationtime"
	"github.com/networkservicemesh/sdk/pkg/registry/core/chain"
	"github.com/networkservicemesh/sdk/pkg/registry/switchcase"
	"github.com/networkservicemesh/sdk/pkg/tools/interdomain"

	"github.com/networkservicemesh/sdk-k8s/pkg/registry/etcd"
	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned"
	"github.com/networkservicemesh/sdk/pkg/registry/common/begin"
)

// Config contains configuration parameters for registry.Registry based on k8s client
type Config struct {
	Namespace        string        `default:"default" desc:"namespace where is deployed registry-k8s instance" split_words:"true"`
	ProxyRegistryURL *url.URL      `desc:"url to the proxy registry that handles this domain" split_words:"true"`
	ExpirePeriod     time.Duration `default:"1m" desc:"period to check expired NSEs" split_words:"true"`
	ChainCtx         context.Context
	ClientSet        versioned.Interface
}

// NewServer creates new registry server based on k8s etcd db storage
func NewServer(config *Config, dialOptions ...grpc.DialOption) registryserver.Registry {
	nseChain := chain.NewNetworkServiceEndpointRegistryServer(
		begin.NewNetworkServiceEndpointRegistryServer(),
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
						dial.NewNetworkServiceEndpointRegistryClient(config.ChainCtx,
							dial.WithDialOptions(dialOptions...),
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
					expire.NewNetworkServiceEndpointRegistryServer(config.ChainCtx, config.ExpirePeriod),
					etcd.NewNetworkServiceEndpointRegistryServer(config.ChainCtx, config.Namespace, config.ClientSet),
				),
			},
		),
	)
	nsChain := chain.NewNetworkServiceRegistryServer(
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
						dial.NewNetworkServiceRegistryClient(config.ChainCtx,
							dial.WithDialOptions(dialOptions...),
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

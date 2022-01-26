// Copyright (c) 2020-2021 Doc.ai and/or its affiliates.
//
// Copyright (c) 2022 Cisco and/or its affiliates.
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

package registryk8s_test

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"google.golang.org/grpc"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/cls"
	kernelmech "github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	"github.com/networkservicemesh/api/pkg/api/registry"
	registryserver "github.com/networkservicemesh/sdk/pkg/registry"
	"github.com/networkservicemesh/sdk/pkg/registry/core/adapters"
	"github.com/networkservicemesh/sdk/pkg/tools/sandbox"

	"github.com/networkservicemesh/sdk-k8s/pkg/registry/chains/registryk8s"
	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned/fake"
)

// This is started as a daemon in k8s.io/klog/v2 init()
var ignoreKLogDaemon = goleak.IgnoreTopFunction("k8s.io/klog/v2.(*loggingT).flushDaemon")

func TestNSMGR_LocalUsecase(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t, ignoreKLogDaemon) })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	domain := sandbox.NewBuilder(ctx, t).
		SetNodesCount(1).
		SetRegistrySupplier(supplyK8sRegistry).
		SetRegistryProxySupplier(nil).
		Build()

	nsRegistryClient := domain.NewNSRegistryClient(ctx, sandbox.GenerateTestToken)

	nseReg := &registry.NetworkServiceEndpoint{
		Name:                "final-endpoint",
		NetworkServiceNames: []string{"my-service-remote"},
	}

	_, err := nsRegistryClient.Register(ctx, &registry.NetworkService{Name: "my-service-remote"})
	require.NoError(t, err)

	domain.Nodes[0].NewEndpoint(ctx, nseReg, sandbox.GenerateTestToken)

	nsc := domain.Nodes[0].NewClient(ctx, sandbox.GenerateTestToken)

	request := &networkservice.NetworkServiceRequest{
		MechanismPreferences: []*networkservice.Mechanism{
			{Cls: cls.LOCAL, Type: kernelmech.MECHANISM},
		},
		Connection: &networkservice.Connection{
			Id:             "1",
			NetworkService: "my-service-remote",
			Context:        &networkservice.ConnectionContext{},
		},
	}

	conn, err := nsc.Request(ctx, request.Clone())
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, 4, len(conn.Path.PathSegments))

	// Simulate refresh from client.

	refreshRequest := request.Clone()
	refreshRequest.Connection = conn.Clone()

	conn2, err := nsc.Request(ctx, refreshRequest)
	require.NoError(t, err)
	require.NotNil(t, conn2)
	require.Equal(t, 4, len(conn2.Path.PathSegments))

	// Close.

	e, err := nsc.Close(ctx, conn)
	require.NoError(t, err)
	require.NotNil(t, e)
}

func TestNSMGR_RemoteUsecase(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t, ignoreKLogDaemon) })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	domain := sandbox.NewBuilder(ctx, t).
		SetNodesCount(2).
		SetRegistrySupplier(supplyK8sRegistry).
		SetRegistryProxySupplier(nil).
		Build()

	nsRegistryClient := domain.NewNSRegistryClient(ctx, sandbox.GenerateTestToken)

	nseReg := &registry.NetworkServiceEndpoint{
		Name:                "final-endpoint",
		NetworkServiceNames: []string{"my-service-remote"},
	}

	_, err := nsRegistryClient.Register(ctx, &registry.NetworkService{Name: "my-service-remote"})
	require.NoError(t, err)

	domain.Nodes[0].NewEndpoint(ctx, nseReg, sandbox.GenerateTestToken)

	request := &networkservice.NetworkServiceRequest{
		MechanismPreferences: []*networkservice.Mechanism{
			{Cls: cls.LOCAL, Type: kernelmech.MECHANISM},
		},
		Connection: &networkservice.Connection{
			Id:             "1",
			NetworkService: "my-service-remote",
			Context:        &networkservice.ConnectionContext{},
		},
	}

	nsc := domain.Nodes[1].NewClient(ctx, sandbox.GenerateTestToken)

	conn, err := nsc.Request(ctx, request.Clone())
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, 6, len(conn.Path.PathSegments))

	// Simulate refresh from client.

	refreshRequest := request.Clone()
	refreshRequest.Connection = conn.Clone()

	conn, err = nsc.Request(ctx, refreshRequest)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, 6, len(conn.Path.PathSegments))

	// Close.

	e, err := nsc.Close(ctx, conn)
	require.NoError(t, err)
	require.NotNil(t, e)
}

func TestNSMGR_InterdomainUseCase(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t, ignoreKLogDaemon) })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	var dnsServer = new(sandbox.FakeDNSResolver)

	cluster1 := sandbox.NewBuilder(ctx, t).
		SetNodesCount(1).
		SetDNSResolver(dnsServer).
		SetDNSDomainName("cluster1").
		SetRegistrySupplier(supplyK8sRegistry).
		Build()

	cluster2 := sandbox.NewBuilder(ctx, t).
		SetNodesCount(1).
		SetDNSDomainName("cluster2").
		SetRegistrySupplier(supplyK8sRegistry).
		SetDNSResolver(dnsServer).
		Build()

	nsRegistryClient := cluster2.NewNSRegistryClient(ctx, sandbox.GenerateTestToken)

	nsReg := &registry.NetworkService{
		Name: "my-service-interdomain",
	}

	_, err := nsRegistryClient.Register(ctx, nsReg)
	require.NoError(t, err)

	nseReg := &registry.NetworkServiceEndpoint{
		Name:                "final-endpoint",
		NetworkServiceNames: []string{nsReg.Name},
	}

	cluster2.Nodes[0].NewEndpoint(ctx, nseReg, sandbox.GenerateTestToken)

	nsc := cluster1.Nodes[0].NewClient(ctx, sandbox.GenerateTestToken)

	request := &networkservice.NetworkServiceRequest{
		MechanismPreferences: []*networkservice.Mechanism{
			{Cls: cls.LOCAL, Type: kernelmech.MECHANISM},
		},
		Connection: &networkservice.Connection{
			Id:             "1",
			NetworkService: fmt.Sprint(nsReg.Name, "@", cluster2.Name),
			Context:        &networkservice.ConnectionContext{},
		},
	}

	conn, err := nsc.Request(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, conn)

	require.Equal(t, 8, len(conn.Path.PathSegments))

	// Simulate refresh from client.

	refreshRequest := request.Clone()
	refreshRequest.Connection = conn.Clone()

	conn, err = nsc.Request(ctx, refreshRequest)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, 8, len(conn.Path.PathSegments))

	// Close
	_, err = nsc.Close(ctx, conn)
	require.NoError(t, err)
}

func TestNSMGR_FloatingInterdomainUseCase(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t, ignoreKLogDaemon) })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	var dnsServer = new(sandbox.FakeDNSResolver)

	cluster1 := sandbox.NewBuilder(ctx, t).
		SetNodesCount(1).
		SetDNSResolver(dnsServer).
		SetRegistrySupplier(supplyK8sRegistry).
		SetDNSDomainName("cluster1").
		Build()

	cluster2 := sandbox.NewBuilder(ctx, t).
		SetNodesCount(1).
		SetDNSDomainName("cluster2").
		SetRegistrySupplier(supplyK8sRegistry).
		SetDNSResolver(dnsServer).
		Build()

	floating := sandbox.NewBuilder(ctx, t).
		SetNodesCount(0).
		SetDNSDomainName("floating.domain").
		SetDNSResolver(dnsServer).
		SetNSMgrProxySupplier(nil).
		SetRegistrySupplier(supplyK8sRegistry).
		SetRegistryProxySupplier(nil).
		Build()

	nsRegistryClient := cluster2.NewNSRegistryClient(ctx, sandbox.GenerateTestToken)

	nsReg := &registry.NetworkService{
		Name: "my-service-interdomain@" + floating.Name,
	}

	_, err := nsRegistryClient.Register(ctx, nsReg)
	require.NoError(t, err)

	nseReg := &registry.NetworkServiceEndpoint{
		Name:                "final-endpoint@" + floating.Name,
		NetworkServiceNames: []string{"my-service-interdomain"},
	}

	cluster2.Nodes[0].NewEndpoint(ctx, nseReg, sandbox.GenerateTestToken)

	c := adapters.NetworkServiceEndpointServerToClient(cluster2.Nodes[0].NSMgr.NetworkServiceEndpointRegistryServer())

	s, err := c.Find(ctx, &registry.NetworkServiceEndpointQuery{NetworkServiceEndpoint: &registry.NetworkServiceEndpoint{
		Name: "final-endpoint@" + floating.Name,
	}})

	require.NoError(t, err)

	list := registry.ReadNetworkServiceEndpointList(s)

	require.Len(t, list, 1)

	nsc := cluster1.Nodes[0].NewClient(ctx, sandbox.GenerateTestToken)

	request := &networkservice.NetworkServiceRequest{
		MechanismPreferences: []*networkservice.Mechanism{
			{Cls: cls.LOCAL, Type: kernelmech.MECHANISM},
		},
		Connection: &networkservice.Connection{
			Id:             "1",
			NetworkService: fmt.Sprint(nsReg.Name),
			Context:        &networkservice.ConnectionContext{},
		},
	}

	conn, err := nsc.Request(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, conn)

	require.Equal(t, 8, len(conn.Path.PathSegments))

	// Simulate refresh from client.

	refreshRequest := request.Clone()
	refreshRequest.Connection = conn.Clone()

	conn, err = nsc.Request(ctx, refreshRequest)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, 8, len(conn.Path.PathSegments))

	// Close
	_, err = nsc.Close(ctx, conn)
	require.NoError(t, err)
}

func supplyK8sRegistry(ctx context.Context, expireDuration time.Duration, proxyRegistryURL *url.URL, options ...grpc.DialOption) registryserver.Registry {
	return registryk8s.NewServer(&registryk8s.Config{
		ChainCtx:         ctx,
		Namespace:        "default",
		ClientSet:        fake.NewSimpleClientset(),
		ExpirePeriod:     expireDuration,
		ProxyRegistryURL: proxyRegistryURL,
	}, options...)
}

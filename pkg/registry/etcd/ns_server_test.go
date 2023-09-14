// Copyright (c) 2021 Doc.ai and/or its affiliates.
//
// Copyright (c) 2023 Cisco and/or its affiliates.
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

package etcd_test

import (
	"context"
	"testing"
	"time"

	"github.com/networkservicemesh/api/pkg/api/registry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/networkservicemesh/sdk/pkg/registry/core/adapters"

	"github.com/networkservicemesh/sdk-k8s/pkg/registry/etcd"
	v1 "github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/apis/networkservicemesh.io/v1"
	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned/fake"
)

func Test_NSReRegister(t *testing.T) {
	s := etcd.NewNetworkServiceRegistryServer(context.Background(), "", fake.NewSimpleClientset())
	_, err := s.Register(context.Background(), &registry.NetworkService{Name: "ns-1"})
	require.NoError(t, err)
	_, err = s.Register(context.Background(), &registry.NetworkService{Name: "ns-1", Payload: "IP"})
	require.NoError(t, err)
}

func Test_K8sNSRegistry_ShouldMatchMetadataToName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var myClientset = fake.NewSimpleClientset()

	_, err := myClientset.NetworkservicemeshV1().NetworkServices("").Create(ctx, &v1.NetworkService{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ns-1",
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	s := etcd.NewNetworkServiceRegistryServer(ctx, "", myClientset)
	c := adapters.NetworkServiceServerToClient(s)

	stream, err := c.Find(ctx, &registry.NetworkServiceQuery{
		NetworkService: &registry.NetworkService{
			Name: "ns-1",
		},
	})
	require.NoError(t, err)

	nseResp, err := stream.Recv()
	require.NoError(t, err)

	require.Equal(t, "ns-1", nseResp.NetworkService.Name)
}

func Test_K8sNSRegistry_Find(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var myClientset = fake.NewSimpleClientset()
	_, err := myClientset.NetworkservicemeshV1().NetworkServices("some namespace").Create(ctx, &v1.NetworkService{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ns-1",
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	c := adapters.NetworkServiceServerToClient(etcd.NewNetworkServiceRegistryServer(ctx, "", myClientset))
	stream, err := c.Find(ctx, &registry.NetworkServiceQuery{
		NetworkService: &registry.NetworkService{
			Name: "ns-1",
		},
	})
	require.NoError(t, err)

	nseResp, err := stream.Recv()
	require.NoError(t, err)

	require.Equal(t, "ns-1", nseResp.NetworkService.Name)
}

func Test_K8sNSRegistry_FindWatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var myClientset = fake.NewSimpleClientset()
	s := etcd.NewNetworkServiceRegistryServer(ctx, "", myClientset)

	// Start watching
	c := adapters.NetworkServiceServerToClient(s)
	stream, err := c.Find(ctx, &registry.NetworkServiceQuery{
		NetworkService: &registry.NetworkService{
			Name: "ns-1",
		},
		Watch: true,
	})
	require.NoError(t, err)

	// Register
	ns := registry.NetworkService{Name: "ns-1"}
	_, err = s.Register(ctx, &ns)
	require.NoError(t, err)

	nsResp, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "ns-1", nsResp.NetworkService.Name)

	// NS reregisteration. We shouldn't get any updates
	_, err = s.Register(ctx, ns.Clone())
	require.NoError(t, err)

	// Update NS again - add payload
	updatedNS := ns.Clone()
	updatedNS.Payload = "IPPayload"
	_, err = myClientset.NetworkservicemeshV1().NetworkServices("").Update(ctx, &v1.NetworkService{
		Spec: v1.NetworkServiceSpec(*updatedNS.Clone()),
		ObjectMeta: metav1.ObjectMeta{
			Name:            updatedNS.Name,
			ResourceVersion: "2",
		},
	}, metav1.UpdateOptions{})
	require.NoError(t, err)

	// We should receive only the last update
	nsResp, err = stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "IPPayload", nsResp.NetworkService.Payload)
}

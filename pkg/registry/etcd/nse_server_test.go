// Copyright (c) 2021 Doc.ai and/or its affiliates.
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

package etcd_test

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/networkservicemesh/api/pkg/api/registry"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/networkservicemesh/sdk/pkg/registry/core/adapters"

	"github.com/networkservicemesh/sdk-k8s/pkg/registry/etcd"
	v1 "github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/apis/networkservicemesh.io/v1"
	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned/fake"
)

func Test_NSEReRegister(t *testing.T) {
	s := etcd.NewNetworkServiceEndpointRegistryServer(context.Background(), "", fake.NewSimpleClientset())
	_, err := s.Register(context.Background(), &registry.NetworkServiceEndpoint{Name: "nse-1"})
	require.NoError(t, err)
	_, err = s.Register(context.Background(), &registry.NetworkServiceEndpoint{Name: "nse-1", NetworkServiceNames: []string{"ns-1"}})
	require.NoError(t, err)
}

func Test_K8sNSERegistry_ShouldMatchMetadataToName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var myClientset = fake.NewSimpleClientset()
	_, err := myClientset.NetworkservicemeshV1().NetworkServiceEndpoints("default").Create(ctx, &v1.NetworkServiceEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nse-1",
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	s := etcd.NewNetworkServiceEndpointRegistryServer(ctx, "default", myClientset)
	c := adapters.NetworkServiceEndpointServerToClient(s)
	stream, err := c.Find(ctx, &registry.NetworkServiceEndpointQuery{
		NetworkServiceEndpoint: &registry.NetworkServiceEndpoint{
			Name: "nse-1",
		},
	})
	require.NoError(t, err)

	nseResp, err := stream.Recv()
	require.NoError(t, err)

	require.Equal(t, "nse-1", nseResp.NetworkServiceEndpoint.Name)
}

func Test_K8sNSERegistry_Find(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var myClientset = fake.NewSimpleClientset()
	_, err := myClientset.NetworkservicemeshV1().NetworkServiceEndpoints("some namespace").Create(ctx, &v1.NetworkServiceEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nse-1",
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	c := adapters.NetworkServiceEndpointServerToClient(etcd.NewNetworkServiceEndpointRegistryServer(ctx, "", myClientset))
	stream, err := c.Find(ctx, &registry.NetworkServiceEndpointQuery{
		NetworkServiceEndpoint: &registry.NetworkServiceEndpoint{
			Name: "nse-1",
		},
	})
	require.NoError(t, err)

	nseResp, err := stream.Recv()
	require.NoError(t, err)

	require.Equal(t, "nse-1", nseResp.NetworkServiceEndpoint.Name)
}

func Test_K8sNSERegistry_FindWatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var myClientset = fake.NewSimpleClientset()
	s := etcd.NewNetworkServiceEndpointRegistryServer(ctx, "", myClientset)

	// Start watching
	c := adapters.NetworkServiceEndpointServerToClient(s)
	stream, err := c.Find(ctx, &registry.NetworkServiceEndpointQuery{
		NetworkServiceEndpoint: &registry.NetworkServiceEndpoint{
			Name: "nse-1",
		},
		Watch: true,
	})
	require.NoError(t, err)

	// Register
	nse := registry.NetworkServiceEndpoint{Name: "nse-1"}
	_, err = s.Register(ctx, &nse)
	require.NoError(t, err)

	nseResp, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "nse-1", nseResp.NetworkServiceEndpoint.Name)

	// NSE reregisteration. We shouldn't get any updates
	_, err = s.Register(ctx, nse.Clone())
	require.NoError(t, err)

	// Update NSE again - add labels
	updatedNSE := nse.Clone()
	updatedNSE.NetworkServiceLabels = map[string]*registry.NetworkServiceLabels{"label": {}}
	_, err = myClientset.NetworkservicemeshV1().NetworkServiceEndpoints("").Update(ctx, &v1.NetworkServiceEndpoint{
		Spec: v1.NetworkServiceEndpointSpec(*updatedNSE.Clone()),
		ObjectMeta: metav1.ObjectMeta{
			Name:            updatedNSE.Name,
			ResourceVersion: "2",
		},
	}, metav1.UpdateOptions{})
	require.NoError(t, err)

	// We should receive only the last update
	nseResp, err = stream.Recv()
	require.NoError(t, err)
	require.Equal(t, 1, len(nseResp.GetNetworkServiceEndpoint().NetworkServiceLabels))
}

func Test_NSEHightloadWatch_ShouldNotFail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	const clinetCount = 20
	const updateCount int32 = 200

	var actual atomic.Int32
	var myClientset = fake.NewSimpleClientset()

	var s = etcd.NewNetworkServiceEndpointRegistryServer(ctx, "ns-1", myClientset)
	var wg sync.WaitGroup
	wg.Add(clinetCount)

	for i := 0; i < clinetCount; i++ {
		go func() {
			defer wg.Done()
			clientCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			c := adapters.NetworkServiceEndpointServerToClient(s)
			stream, err := c.Find(clientCtx, &registry.NetworkServiceEndpointQuery{
				NetworkServiceEndpoint: &registry.NetworkServiceEndpoint{},
				Watch:                  true,
			})
			require.NoError(t, err)

			for range registry.ReadNetworkServiceEndpointChannel(stream) {
				actual.Add(1)
			}
		}()
	}

	go func() {
		for i := int32(0); i < updateCount; i++ {
			_, _ = myClientset.NetworkservicemeshV1().NetworkServiceEndpoints("ns-1").Create(ctx, &v1.NetworkServiceEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
				},
				Spec: v1.NetworkServiceEndpointSpec{
					ExpirationTime: timestamppb.New(time.Now().Add(-time.Hour)),
				},
			}, metav1.CreateOptions{})
			time.Sleep(time.Millisecond * 10)
		}
	}()
	wg.Wait()

	require.InDelta(t, updateCount, actual.Load()/clinetCount, 5)
}

func Test_K8sNSERegistry_Find_ExpiredNSE(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var myClientset = fake.NewSimpleClientset()
	_, err := myClientset.NetworkservicemeshV1().NetworkServiceEndpoints("ns-1").Create(ctx, &v1.NetworkServiceEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nse-1",
		},
		Spec: v1.NetworkServiceEndpointSpec{
			ExpirationTime: timestamppb.New(time.Now().Add(-time.Hour)),
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	c := adapters.NetworkServiceEndpointServerToClient(etcd.NewNetworkServiceEndpointRegistryServer(ctx, "ns-1", myClientset))
	stream, err := c.Find(ctx, &registry.NetworkServiceEndpointQuery{
		NetworkServiceEndpoint: &registry.NetworkServiceEndpoint{
			Name: "nse-1",
		},
	})
	require.NoError(t, err)

	_, err = stream.Recv()
	require.True(t, errors.Is(err, io.EOF))
	require.Eventually(t, func() bool {
		resp, err := myClientset.NetworkservicemeshV1().NetworkServiceEndpoints("ns-1").List(ctx, metav1.ListOptions{})
		return err == nil && len(resp.Items) == 0
	}, time.Second/2, time.Millisecond*100)
}

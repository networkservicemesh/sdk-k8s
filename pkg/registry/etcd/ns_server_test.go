// Copyright (c) 2021 Doc.ai and/or its affiliates.
//
// Copyright (c) 2023-2024 Cisco and/or its affiliates.
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
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/networkservicemesh/api/pkg/api/registry"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/networkservicemesh/sdk/pkg/registry/core/adapters"

	"github.com/networkservicemesh/sdk-k8s/pkg/registry/etcd"
	v1 "github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/apis/networkservicemesh.io/v1"
	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned/fake"
)

func Test_NSReRegister(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s := etcd.NewNetworkServiceRegistryServer(ctx, "", fake.NewSimpleClientset())
	_, err := s.Register(ctx, &registry.NetworkService{Name: "netsvc-1"})
	require.NoError(t, err)
	_, err = s.Register(ctx, &registry.NetworkService{Name: "netsvc-1"})
	require.NoError(t, err)
}

func Test_NSServer_UpdateShouldWorkСonsistently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s := etcd.NewNetworkServiceRegistryServer(ctx, "", fake.NewSimpleClientset())

	var expected []*registry.NetworkService
	// Register
	for i := 0; i < 10; i++ {
		expected = append(expected, &registry.NetworkService{Name: "netsvc-" + fmt.Sprint(i)})
		_, err := s.Register(ctx, expected[len(expected)-1].Clone())
		require.NoError(t, err)
	}

	// Update only first nse
	expected[0].Payload = "ip"
	_, err := s.Register(ctx, expected[0].Clone())
	require.NoError(t, err)

	// Update only last nse
	expected[len(expected)-1].Payload = "ethernet"
	_, err = s.Register(ctx, expected[len(expected)-1].Clone())
	require.NoError(t, err)

	// Get all nses
	stream, err := adapters.NetworkServiceServerToClient(s).Find(ctx, &registry.NetworkServiceQuery{NetworkService: &registry.NetworkService{}})
	require.NoError(t, err)

	nsList := registry.ReadNetworkServiceList(stream)

	require.Len(t, nsList, 10)

	for i := range nsList {
		require.True(t, proto.Equal(expected[i], nsList[i]))
	}
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

	time.Sleep(time.Millisecond * 10)
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

	// NS reregisteration.
	_, err = s.Register(ctx, ns.Clone())
	require.NoError(t, err)

	nsResp, err = stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "ns-1", nsResp.NetworkService.Name)

	const expectedPayload = "IPPayload"

	// Update NS again - add payload
	updatedNS := ns.Clone()
	updatedNS.Payload = expectedPayload
	_, err = myClientset.NetworkservicemeshV1().NetworkServices("").Update(ctx, &v1.NetworkService{
		Spec: v1.NetworkServiceSpec(*updatedNS.Clone()),
		ObjectMeta: metav1.ObjectMeta{
			Name:            updatedNS.Name,
			ResourceVersion: "2",
		},
	}, metav1.UpdateOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		nsResp, err = stream.Recv()
		return err == nil && nsResp.NetworkService.Payload == expectedPayload
	}, time.Second, time.Millisecond*100)
}

func Test_NSHighloadWatch_ShouldNotFail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	const clinetCount = 20
	const updateCount int32 = 200

	watch.DefaultChanSize = updateCount

	var actual atomic.Int32
	var myClientset = fake.NewSimpleClientset()

	var s = etcd.NewNetworkServiceRegistryServer(ctx, "ns-1", myClientset)
	var doneWg, startWg sync.WaitGroup
	doneWg.Add(clinetCount)
	startWg.Add(clinetCount)

	for i := 0; i < clinetCount; i++ {
		go func() {
			defer doneWg.Done()
			clientCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			c := adapters.NetworkServiceServerToClient(s)
			stream, _ := c.Find(clientCtx, &registry.NetworkServiceQuery{
				NetworkService: &registry.NetworkService{},
				Watch:          true,
			})
			ch := registry.ReadNetworkServiceChannel(stream)
			startWg.Done()
			for range ch {
				actual.Add(1)
			}
		}()
	}
	startWg.Wait()
	go func() {
		for i := int32(0); i < updateCount; i++ {
			_, _ = myClientset.NetworkservicemeshV1().NetworkServices("ns-1").Create(ctx, &v1.NetworkService{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
				},
			}, metav1.CreateOptions{})
		}
	}()
	doneWg.Wait()
	require.InDelta(t, updateCount, actual.Load()/clinetCount, 20)
}

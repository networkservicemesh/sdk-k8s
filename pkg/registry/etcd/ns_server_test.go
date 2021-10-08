// Copyright (c) 2021 Doc.ai and/or its affiliates.
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

func Test_K8sNERegistry_ShouldMatchMetadataToName(t *testing.T) {
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

	nse, err := stream.Recv()
	require.NoError(t, err)

	require.Equal(t, "ns-1", nse.Name)
}

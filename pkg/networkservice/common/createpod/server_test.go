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

package createpod_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/clientinfo"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/adapters"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/next"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/networkservicemesh/sdk-k8s/pkg/networkservice/common/createpod"
)

func TestCreatePod(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	clientSet := fake.NewSimpleClientset()

	podTemplate := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "my-container-1",
					Image: "my-image-1",
				},
				{
					Name:  "my-container-2",
					Image: "my-image-2",
				},
			},
		},
	}

	namespace := "pod-ns-name"

	server := next.NewNetworkServiceServer(
		adapters.NewClientToServer(clientinfo.NewClient()),
		createpod.NewServer(clientSet, podTemplate, namespace),
	)

	nodeName := "node1"
	err := os.Setenv("NODE_NAME", nodeName)
	require.NoError(t, err)

	_, err = server.Request(ctx, &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{},
	})
	require.Error(t, err)

	podList, err := clientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Equal(t, 1, len(podList.Items))
	pod := podList.Items[0]

	want := corev1.Pod{
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{
					Name:  "my-container-1",
					Image: "my-image-1",
					Env:   []corev1.EnvVar{{Name: "NSM_LABELS", Value: "nodeName: " + nodeName}},
				},
				{
					Name:  "my-container-2",
					Image: "my-image-2",
					Env:   []corev1.EnvVar{{Name: "NSM_LABELS", Value: "nodeName: " + nodeName}},
				},
			},
		},
	}
	require.Equal(t, pod.Spec, want.Spec)
}

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
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/clientinfo"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/adapters"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/next"

	"github.com/networkservicemesh/sdk-k8s/pkg/networkservice/common/createpod"
)

const (
	testNamespace = "pod-ns-name"
	nodeName1     = "node1"
	nodeName2     = "node2"
)

func TestCreatePod_RepeatedRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	clientSet := fake.NewSimpleClientset()

	podTemplate := defaultPodTemplate()

	counter := atomic.Int32{}

	server := next.NewNetworkServiceServer(
		adapters.NewClientToServer(clientinfo.NewClient()),
		createpod.NewServer(ctx, clientSet, podTemplate,
			createpod.WithNamespace(testNamespace),
			createpod.WithNameGenerator(func(templateName, nodeName string) string {
				return templateName + strconv.Itoa(int(counter.Add(1)))
			}),
		),
	)

	err := os.Setenv("NODE_NAME", nodeName1)
	require.NoError(t, err)

	// first request: should succeed
	_, err = server.Request(ctx, &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{},
	})
	require.Error(t, err)
	require.Equal(t, "cannot provide required networkservice: local endpoint created as PodName1", err.Error())

	// second request: should fail
	_, err = server.Request(ctx, &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{},
	})
	require.Error(t, err)
	require.Equal(t, "cannot provide required networkservice: local endpoint already exists", err.Error())

	podList, err := clientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Equal(t, 1, len(podList.Items))
	pod := podList.Items[0].DeepCopy()

	want := podTemplate.DeepCopy()
	want.Spec.NodeName = nodeName1
	want.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "NSM_LABELS", Value: "nodeName: " + nodeName1}}
	want.Spec.Containers[1].Env = []corev1.EnvVar{{Name: "NSM_LABELS", Value: "nodeName: " + nodeName1}}
	require.Equal(t, pod.Spec, want.Spec)

	pod.Status.Phase = "Succeeded"
	_, err = clientSet.CoreV1().Pods(testNamespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		// third request: should succeed as soon as the server gets pod deletion event
		_, err = server.Request(ctx, &networkservice.NetworkServiceRequest{
			Connection: &networkservice.Connection{},
		})
		require.Error(t, err)
		return err.Error() == "cannot provide required networkservice: local endpoint created as PodName2"
	}, time.Millisecond*100, time.Millisecond*10)

	podList, err = clientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(podList.Items) == 1
	}, time.Millisecond*100, time.Millisecond*5)
	pod2 := podList.Items[0].DeepCopy()
	require.NotEqual(t, pod.Name, pod2.Name, "new pod wasn't created")
}

func TestCreatePod_TwoNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	clientSet := fake.NewSimpleClientset()

	podTemplate := defaultPodTemplate()

	counter := atomic.Int32{}

	server := next.NewNetworkServiceServer(
		adapters.NewClientToServer(clientinfo.NewClient()),
		createpod.NewServer(ctx, clientSet, podTemplate,
			createpod.WithNamespace(testNamespace),
			createpod.WithNameGenerator(func(templateName, nodeName string) string {
				return templateName + strconv.Itoa(int(counter.Add(1)))
			}),
		),
	)

	err := os.Setenv("NODE_NAME", nodeName1)
	require.NoError(t, err)

	_, err = server.Request(ctx, &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{},
	})
	require.Error(t, err)
	require.Equal(t, "cannot provide required networkservice: local endpoint created as PodName1", err.Error())

	err = os.Setenv("NODE_NAME", nodeName2)
	require.NoError(t, err)

	_, err = server.Request(ctx, &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{},
	})
	require.Error(t, err)
	require.Equal(t, "cannot provide required networkservice: local endpoint created as PodName2", err.Error())

	podList, err := clientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Equal(t, 2, len(podList.Items))
	require.Equal(t, nodeName1, podList.Items[0].Spec.NodeName)
	require.Equal(t, nodeName2, podList.Items[1].Spec.NodeName)
}

func defaultPodTemplate() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "PodName",
		},
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
}

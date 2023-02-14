// Copyright (c) 2021-2022 Doc.ai and/or its affiliates.
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

package createpod_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/clientinfo"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/adapters"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/next"

	"github.com/networkservicemesh/sdk-k8s/pkg/networkservice/common/createpod"
)

var ignoreKLogDaemon = goleak.IgnoreTopFunction("k8s.io/klog/v2.(*loggingT).flushDaemon")

const (
	testNamespace = "pod-ns-name"
	nodeName1     = "node1"
	nodeName2     = "node2"
)

func TestCreatePod_RepeatedRequest(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t, ignoreKLogDaemon) })

	const podTemplate = `---
apiVersion: apps/v1
kind: Pod
metadata:
    name: nse-1
    labels: {{ range $key, $value := .Labels }}
        "{{ $key }}": "{{ $value }}"{{ end }}
        uniq: {{ uuid }}
objectmeta:
    containers:
        - name: my-container-1
          image: my-image-1
`

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	clientSet := fake.NewSimpleClientset()

	err := os.Setenv("HOSTNAME", "supplier-1")
	require.NoError(t, err)

	server := next.NewNetworkServiceServer(
		adapters.NewClientToServer(clientinfo.NewClient()),
		createpod.NewServer(ctx, clientSet, podTemplate,
			createpod.WithNamespace(testNamespace),
		),
	)

	err = os.Setenv("NODE_NAME", nodeName1)
	require.NoError(t, err)

	// first request: should succeed
	_, err = server.Request(ctx, &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot provide required networkservice: endpoint created as ")

	// second request: should fail
	_, err = server.Request(ctx, &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{},
	})
	require.Error(t, err)
	require.Equal(t, "failed to create a pod nse-1 in a namespace pod-ns-name: pods \"nse-1\" already exists", err.Error())

	podList, err := clientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Equal(t, 1, len(podList.Items))
	pod := podList.Items[0].DeepCopy()

	pod.Status.Phase = "Failed"
	_, err = clientSet.CoreV1().Pods(testNamespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		// third request: should succeed as soon as the server gets pod deletion event
		_, err = server.Request(ctx, &networkservice.NetworkServiceRequest{
			Connection: &networkservice.Connection{},
		})
		require.Error(t, err)

		podList, err = clientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
		require.NoError(t, err)
		return len(podList.Items) == 1 && podList.Items[0].GetObjectMeta().GetLabels()["uniq"] != pod.GetObjectMeta().GetLabels()["uniq"]
	}, time.Millisecond*100, time.Millisecond*10, "new pod wasn't created")

	podList, err = clientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(podList.Items) == 1
	}, time.Millisecond*100, time.Millisecond*5)
	pod2 := podList.Items[0].DeepCopy()
	require.NotEqual(t, pod.String(), pod2.String())
}

func TestCreatePod_TwoNodes(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t, ignoreKLogDaemon) })

	const podTemplate = `---
apiVersion: apps/v1
kind: Pod
metadata:
    name: nse-{{ uuid }}
    labels: {{ range $key, $value := .Labels }}
        "{{ $key }}": "{{ $value }}"{{ end }}
objectmeta:
    containers:
        - name: my-container-1
          image: my-image-1
`

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	clientSet := fake.NewSimpleClientset()

	err := os.Setenv("HOSTNAME", "supplier-1")
	require.NoError(t, err)

	server := next.NewNetworkServiceServer(
		adapters.NewClientToServer(clientinfo.NewClient()),
		createpod.NewServer(ctx, clientSet, podTemplate,
			createpod.WithNamespace(testNamespace),
		),
	)

	err = os.Setenv("NODE_NAME", nodeName1)
	require.NoError(t, err)

	_, err = clientSet.CoreV1().Nodes().Create(ctx, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName1}}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = clientSet.CoreV1().Nodes().Create(ctx, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName2}}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = server.Request(ctx, &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{
			Labels: map[string]string{
				"a": "b",
				"c": "e",
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot provide required networkservice: endpoint created as ")

	err = os.Setenv("NODE_NAME", nodeName2)
	require.NoError(t, err)

	_, err = server.Request(ctx, &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{
			Labels: map[string]string{
				"a": "b",
				"c": "e",
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot provide required networkservice: endpoint created as ")

	podList, err := clientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	var nodesSet = map[string]struct{}{
		nodeName1: {},
		nodeName2: {},
	}
	require.Contains(t, podList.Items[0].GetLabels(), "a")
	require.Contains(t, podList.Items[0].GetLabels(), "c")
	require.Equal(t, 2, len(podList.Items))
	require.Contains(t, nodesSet, podList.Items[0].Spec.NodeName)
	delete(nodesSet, podList.Items[0].Spec.NodeName)
	require.Contains(t, nodesSet, podList.Items[1].Spec.NodeName)
}

func TestCreatePod_UnknownNode(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t, ignoreKLogDaemon) })

	const podTemplate = `---
apiVersion: apps/v1
kind: Pod
metadata:
    name: nse-{{ uuid }}
objectmeta:
    containers:
        - name: my-container-1
          image: my-image-1
`

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	clientSet := fake.NewSimpleClientset()

	err := os.Setenv("HOSTNAME", "supplier-1")
	require.NoError(t, err)

	server := next.NewNetworkServiceServer(
		adapters.NewClientToServer(clientinfo.NewClient()),
		createpod.NewServer(ctx, clientSet, podTemplate,
			createpod.WithNamespace(testNamespace),
		),
	)

	err = os.Setenv("NODE_NAME", "unknown")
	require.NoError(t, err)

	_, err = clientSet.CoreV1().Nodes().Create(ctx, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName1}}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = server.Request(ctx, &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot provide required networkservice: endpoint created as ")

	podList, err := clientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)

	require.Empty(t, podList.Items[0].Spec.NodeName)
}

func TestCreatePod_DeleteForgotPods(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t, ignoreKLogDaemon) })

	const podTemplate = `---
apiVersion: apps/v1
kind: Pod
metadata:
    name: nse-{{ uuid }}
objectmeta:
    containers:
        - name: my-container-1
          image: my-image-1
`

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	clientSet := fake.NewSimpleClientset()

	err := os.Setenv("HOSTNAME", "supplier-1")
	require.NoError(t, err)

	server := next.NewNetworkServiceServer(
		adapters.NewClientToServer(clientinfo.NewClient()),
		createpod.NewServer(ctx, clientSet, podTemplate,
			createpod.WithNamespace(testNamespace),
		),
	)

	err = os.Setenv("NODE_NAME", "unknown")
	require.NoError(t, err)

	_, err = clientSet.CoreV1().Nodes().Create(ctx, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName1}}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = server.Request(ctx, &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot provide required networkservice: endpoint created as ")

	podList, err := clientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)

	require.Len(t, podList.Items, 1)

	cancel()

	for end := time.Now().Add(time.Millisecond * 150); time.Until(end) > 0; time.Sleep(time.Millisecond * 15) {
		podList, err = clientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
		require.NoError(t, err)
		require.Len(t, podList.Items, 1)
	}

	podList.Items[0].Status.Phase = "Succeeded"
	_, err = clientSet.CoreV1().Pods(testNamespace).UpdateStatus(ctx, &podList.Items[0], metav1.UpdateOptions{})
	require.NoError(t, err)

	for end := time.Now().Add(time.Millisecond * 100); time.Until(end) > 0; time.Sleep(time.Millisecond * 10) {
		podList, err = clientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
		require.NoError(t, err)
		require.Len(t, podList.Items, 1)
	}

	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_ = next.NewNetworkServiceServer(
		adapters.NewClientToServer(clientinfo.NewClient()),
		createpod.NewServer(ctx, clientSet, podTemplate,
			createpod.WithNamespace(testNamespace),
		),
	)

	require.Eventually(t, func() bool {
		podList, err := clientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
		return err == nil && len(podList.Items) == 0
	}, time.Millisecond*100, time.Millisecond*10)
}

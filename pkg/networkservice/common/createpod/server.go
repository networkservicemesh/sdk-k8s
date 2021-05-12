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

// Package createpod provides server chain element that creates pods with specified parameters on demand
package createpod

import (
	"context"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/next"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	nodeNameKey = "NodeNameKey"
)

type createPodServer struct {
	client      kubernetes.Interface
	podTemplate *corev1.Pod
	namespace   string
}

// NewServer - returns a new server chain element that creates new pods using provided template.
//
// Pods are created on the node with a name specified by key "NodeNameKey" in request labels
// (this label is expected to be filled by clientinfo client).
func NewServer(client kubernetes.Interface, podTemplate *corev1.Pod, namespace string) networkservice.NetworkServiceServer {
	s := &createPodServer{
		podTemplate: podTemplate.DeepCopy(),
		client:      client,
		namespace:   namespace,
	}
	return s
}

func (s *createPodServer) Request(ctx context.Context, request *networkservice.NetworkServiceRequest) (*networkservice.Connection, error) {
	nodeName := request.GetConnection().GetLabels()[nodeNameKey]
	if nodeName == "" {
		return nil, errors.New("NodeNameKey not set")
	}

	podTemplate := s.podTemplate.DeepCopy()
	podTemplate.ObjectMeta.Name = podTemplate.ObjectMeta.Name + "-nodeName=" + nodeName
	podTemplate.Spec.NodeName = nodeName
	for i := range podTemplate.Spec.Containers {
		podTemplate.Spec.Containers[i].Env = append(podTemplate.Spec.Containers[i].Env, corev1.EnvVar{
			Name:  "NSM_LABELS",
			Value: "nodeName: " + nodeName,
		})
	}

	_, err := s.client.CoreV1().Pods(s.namespace).Create(ctx, podTemplate, metav1.CreateOptions{})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return nil, errors.New("cannot provide required networkservice")
}

func (s *createPodServer) Close(ctx context.Context, conn *networkservice.Connection) (*empty.Empty, error) {
	return next.Server(ctx).Close(ctx, conn)
}

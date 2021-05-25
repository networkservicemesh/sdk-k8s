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
	"sync"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/next"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
)

const (
	nodeNameKey = "NodeNameKey"
)

type createPodServer struct {
	ctx           context.Context
	client        kubernetes.Interface
	podTemplate   *corev1.Pod
	namespace     string
	labelsKey     string
	nodeMap       nodeInfoMap
	nameGenerator func(templateName, nodeName string) string
}

type nodeInfo struct {
	mut  sync.Mutex
	name string
}

// NewServer - returns a new server chain element that creates new pods using provided template.
//
// Pods are created on the node with a name specified by key "NodeNameKey" in request labels
// (this label is expected to be filled by clientinfo client).
func NewServer(ctx context.Context, client kubernetes.Interface, podTemplate *corev1.Pod, options ...Option) networkservice.NetworkServiceServer {
	s := &createPodServer{
		ctx:           ctx,
		podTemplate:   podTemplate.DeepCopy(),
		client:        client,
		namespace:     "default",
		labelsKey:     "NSM_LABELS",
		nameGenerator: func(templateName, nodeName string) string { return templateName + uuid.New().String() },
	}

	for _, opt := range options {
		opt(s)
	}

	w, err := s.client.CoreV1().Pods(s.namespace).Watch(ctx, metav1.ListOptions{
		LabelSelector: "",
		FieldSelector: "",
	})
	if err == nil {
		go s.monitorCompletedPods(w)
	} else {
		log.FromContext(s.ctx).Error("createpod: can't start watching pod events: ", err)
	}

	return s
}

func (s *createPodServer) monitorCompletedPods(w watch.Interface) {
	for event := range w.ResultChan() {
		p, ok := event.Object.(*corev1.Pod)
		if !ok {
			continue
		}
		if p.Status.Phase == "Succeeded" || p.Status.Phase == "Failed" {
			if ni, loaded := s.nodeMap.Load(p.Spec.NodeName); loaded {
				ni.mut.Lock()
				if p.ObjectMeta.Name == ni.name {
					ni.name = ""
				}
				ni.mut.Unlock()

				err := s.client.CoreV1().Pods(s.namespace).Delete(context.Background(), p.Name, metav1.DeleteOptions{})
				if err != nil {
					log.FromContext(s.ctx).Warn("createpod: can't delete finished pod: ", err)
				}
			}
		}
	}
}

func (s *createPodServer) Request(ctx context.Context, request *networkservice.NetworkServiceRequest) (*networkservice.Connection, error) {
	nodeName := request.GetConnection().GetLabels()[nodeNameKey]
	if nodeName == "" {
		return nil, errors.New("NodeNameKey not set")
	}

	ni, _ := s.nodeMap.LoadOrStore(nodeName, &nodeInfo{})
	ni.mut.Lock()
	defer ni.mut.Unlock()

	if ni.name != "" {
		return nil, errors.New("cannot provide required networkservice: local endpoint already exists")
	}

	ni.name = s.nameGenerator(s.podTemplate.ObjectMeta.Name, nodeName)
	err := s.createPod(ctx, nodeName, ni.name)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return nil, errors.Errorf("cannot provide required networkservice: local endpoint created as %v", ni.name)
}

func (s *createPodServer) Close(ctx context.Context, conn *networkservice.Connection) (*empty.Empty, error) {
	return next.Server(ctx).Close(ctx, conn)
}

func (s *createPodServer) createPod(ctx context.Context, nodeName, podName string) error {
	podTemplate := s.podTemplate.DeepCopy()
	podTemplate.ObjectMeta.Name = podName
	podTemplate.Spec.NodeName = nodeName
	for i := range podTemplate.Spec.Containers {
		podTemplate.Spec.Containers[i].Env = append(podTemplate.Spec.Containers[i].Env, corev1.EnvVar{
			Name:  s.labelsKey,
			Value: "nodeName: " + nodeName,
		})
	}

	_, err := s.client.CoreV1().Pods(s.namespace).Create(ctx, podTemplate, metav1.CreateOptions{})
	return err
}

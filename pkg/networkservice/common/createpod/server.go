// Copyright (c) 2021-2022 Doc.ai and/or its affiliates.
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
	"bytes"
	"context"
	"fmt"
	"sync"
	"text/template"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/next"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
)

const (
	nodeNameKey = "nodeName"
)

type createPodServer struct {
	ctx          context.Context
	client       kubernetes.Interface
	podTemplate  string
	namespace    string
	nodeMap      nodeInfoMap
	deserializer runtime.Decoder
}

type nodeInfo struct {
	mut  sync.Mutex
	name string
}

// NewServer - returns a new server chain element that creates new pods using provided template.
//
// Pods are created on the node with a name specified by key "NodeNameKey" in request labels
// (this label is expected to be filled by clientinfo client).
func NewServer(ctx context.Context, client kubernetes.Interface, podTemplate string, options ...Option) networkservice.NetworkServiceServer {

	scheme := runtime.NewScheme()
	codecFactory := serializer.NewCodecFactory(scheme)
	deserializer := codecFactory.UniversalDeserializer()

	s := &createPodServer{
		ctx:          ctx,
		podTemplate:  podTemplate,
		client:       client,
		namespace:    "default",
		deserializer: deserializer,
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
		return nil, errors.New("nodeName label is not set")
	}

	ni, _ := s.nodeMap.LoadOrStore(nodeName, &nodeInfo{})
	ni.mut.Lock()
	defer ni.mut.Unlock()

	if ni.name != "" {
		return nil, errors.New("cannot provide required networkservice: local endpoint already exists")
	}

	name, err := s.createPod(ctx, nodeName, request.GetConnection())
	if err != nil {
		return nil, errors.WithStack(err)
	}
	ni.name = name
	return nil, errors.Errorf("cannot provide required networkservice: local endpoint created as %v", ni.name)
}

func (s *createPodServer) Close(ctx context.Context, conn *networkservice.Connection) (*empty.Empty, error) {
	return next.Server(ctx).Close(ctx, conn)
}

func (s *createPodServer) createPod(ctx context.Context, nodeName string, conn *networkservice.Connection) (string, error) {
	var t, err = template.New("createPod").Funcs(template.FuncMap{
		"uuid": func() string {
			return uuid.New().String()
		},
	}).Parse(s.podTemplate)

	if err != nil {
		return "", err
	}
	var buffer bytes.Buffer
	if err = t.Execute(&buffer, conn); err != nil {
		return "", err
	}
	var pod corev1.Pod

	fmt.Println(string(buffer.Bytes()))
	_, _, err = s.deserializer.Decode(buffer.Bytes(), nil, &pod)
	if err != nil {
		return "", err
	}
	if pod.Spec.NodeName == "" {
		pod.Spec.NodeName = nodeName
	}

	resp, err := s.client.CoreV1().Pods(s.namespace).Create(ctx, &pod, metav1.CreateOptions{})

	if err != nil {
		return "", err
	}

	return resp.GetObjectMeta().GetName(), nil
}

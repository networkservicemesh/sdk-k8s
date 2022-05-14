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
	"os"
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
	createdBy   = "createdBy"
)

type createPodServer struct {
	ctx          context.Context
	client       kubernetes.Interface
	deserializer runtime.Decoder
	podTemplate  string
	myNamespace  string
	myNode       string
	myName       string
}

// NewServer - returns a new server chain element that creates new pods using provided template.
//
// Pods are created on the node with a name specified by key "NodeNameKey" in request labels
// (this label is expected to be filled by clientinfo client).
func NewServer(ctx context.Context, client kubernetes.Interface, podTemplate string, options ...Option) networkservice.NetworkServiceServer {
	var scheme = runtime.NewScheme()
	var codecFactory = serializer.NewCodecFactory(scheme)
	var deserializer = codecFactory.UniversalDeserializer()

	var s = &createPodServer{
		ctx:          ctx,
		podTemplate:  podTemplate,
		client:       client,
		myNamespace:  "default",
		deserializer: deserializer,
		myName:       os.Getenv("HOSTNAME"),
	}

	for _, opt := range options {
		opt(s)
	}

	var w, err = s.client.CoreV1().Pods(s.myNamespace).Watch(ctx, metav1.ListOptions{
		LabelSelector: "",
		FieldSelector: "",
	})
	if err == nil {
		go s.monitorCompletedPods(w)
	} else {
		log.FromContext(s.ctx).Warn("createpod: can't start watching pod events: ", err)
	}

	return s
}

func (s *createPodServer) monitorCompletedPods(w watch.Interface) {
	var list, err = s.client.CoreV1().Pods(s.myNamespace).List(s.ctx, metav1.ListOptions{})

	if err == nil {
		for i := 0; i < len(list.Items); i++ {
			var p = &list.Items[i]
			s.deleteSpawnedCompletedPod(p)
		}
	} else {
		log.FromContext(s.ctx).Warn("createpod: can't list current pods: ", err)
	}

	go func() {
		<-s.ctx.Done()
		w.Stop()
	}()

	for event := range w.ResultChan() {
		p, ok := event.Object.(*corev1.Pod)
		if !ok {
			continue
		}
		s.deleteSpawnedCompletedPod(p)
	}
}

func (s *createPodServer) Request(ctx context.Context, request *networkservice.NetworkServiceRequest) (*networkservice.Connection, error) {
	nodeName := request.GetConnection().GetLabels()[nodeNameKey]
	_, err := s.client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		nodeName = ""
	}

	name, err := s.createPod(ctx, nodeName, request.GetConnection())
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return nil, errors.Errorf("cannot provide required networkservice: endpoint created as %v", name)
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
		return "", errors.WithStack(err)
	}
	var buffer bytes.Buffer
	if err = t.Execute(&buffer, conn); err != nil {
		return "", errors.WithStack(err)
	}
	var pod corev1.Pod

	_, _, err = s.deserializer.Decode(buffer.Bytes(), nil, &pod)
	if err != nil {
		return "", errors.WithStack(err)
	}

	if pod.Spec.NodeName == "" {
		pod.Spec.NodeName = nodeName
	}

	if pod.GetObjectMeta().GetLabels() == nil {
		pod.GetObjectMeta().SetLabels(make(map[string]string))
	}

	pod.GetObjectMeta().GetLabels()[createdBy] = s.myName

	resp, err := s.client.CoreV1().Pods(s.myNamespace).Create(ctx, &pod, metav1.CreateOptions{})
	if err != nil {
		return "", errors.WithStack(err)
	}

	return resp.GetObjectMeta().GetName(), nil
}

func (s *createPodServer) deleteSpawnedCompletedPod(p *corev1.Pod) {
	if p.Labels[createdBy] != s.myName {
		return
	}
	if p.Status.Phase != "Succeeded" && p.Status.Phase != "Failed" {
		return
	}
	err := s.client.CoreV1().Pods(s.myNamespace).Delete(s.ctx, p.Name, metav1.DeleteOptions{})
	if err != nil {
		log.FromContext(s.ctx).Warn("createpod: can't delete finished pod: ", err)
	}
}

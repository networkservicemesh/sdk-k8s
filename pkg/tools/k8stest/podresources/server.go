// Copyright (c) 2020 Doc.ai and/or its affiliates.
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

// Package podresources provides pod resources GRPC API stubs for testing
package podresources

import (
	"context"

	"google.golang.org/grpc"
	podresources "k8s.io/kubelet/pkg/apis/podresources/v1alpha1"
)

type podResourcesListerServer struct{}

// StartPodResourcesListerServer creates a new podResourcesListServer and registers it on given GRPC server
func StartPodResourcesListerServer(server *grpc.Server) {
	podresources.RegisterPodResourcesListerServer(server, &podResourcesListerServer{})
}

func (prls *podResourcesListerServer) List(_ context.Context, _ *podresources.ListPodResourcesRequest) (*podresources.ListPodResourcesResponse, error) {
	return &podresources.ListPodResourcesResponse{}, nil
}

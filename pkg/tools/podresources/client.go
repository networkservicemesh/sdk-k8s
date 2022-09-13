// Copyright (c) 2020-2022 Doc.ai and/or its affiliates.
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

//go:build !windows
// +build !windows

// Package podresources provides a client for k8s podresources API
package podresources

import (
	"context"
	"path"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	podresources "k8s.io/kubelet/pkg/apis/podresources/v1alpha1"

	"github.com/networkservicemesh/sdk/pkg/tools/grpcutils"
	"github.com/networkservicemesh/sdk/pkg/tools/log"

	"github.com/networkservicemesh/sdk-k8s/pkg/tools/socketpath"
)

const (
	kubeletSocket = "kubelet.sock"
)

// Client is a k8s podresources API helper class
type Client struct {
	podResourcesSocket string
}

// NewClient creates a new deviceplugin client
func NewClient(podResourcesPath string) *Client {
	return &Client{
		podResourcesSocket: path.Join(podResourcesPath, kubeletSocket),
	}
}

// GetPodResourcesListerClient returns a new PodResourcesListerClient
func (km *Client) GetPodResourcesListerClient(ctx context.Context) (podresources.PodResourcesListerClient, error) {
	logger := log.FromContext(ctx).WithField("podresources.Client", "GetPodResourcesListerClient")

	socketURL := grpcutils.AddressToURL(socketpath.SocketPath(km.podResourcesSocket))
	conn, err := grpc.DialContext(ctx, socketURL.String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, errors.Wrap(err, "cannot connect to pod resources kubelet service")
	}

	logger.Info("start pod resources lister client")
	go func() {
		<-ctx.Done()
		logger.Info("close pod resources lister client")
		_ = conn.Close()
	}()

	return podresources.NewPodResourcesListerClient(conn), nil
}

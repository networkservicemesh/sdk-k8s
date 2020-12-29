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

//+build !windows

// Package deviceplugin provides a client for k8s deviceplugin API
package deviceplugin

import (
	"context"
	"path"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/networkservicemesh/sdk/pkg/tools/grpcutils"
	"github.com/networkservicemesh/sdk/pkg/tools/log"

	"github.com/networkservicemesh/sdk-k8s/pkg/tools/socketpath"
)

const (
	dialTimeoutDefault = 15 * time.Second
	kubeletSocket      = "kubelet.sock"
)

// Client is a k8s deviceplugin API helper class
type Client struct {
	devicePluginPath   string
	devicePluginSocket string
}

// NewClient creates a new deviceplugin client
func NewClient(devicePluginPath string) *Client {
	return &Client{
		devicePluginPath:   devicePluginPath,
		devicePluginSocket: path.Join(devicePluginPath, kubeletSocket),
	}
}

// StartDeviceServer starts device plugin server and returns the name of the corresponding unix socket
func (c *Client) StartDeviceServer(ctx context.Context, deviceServer pluginapi.DevicePluginServer) (string, error) {
	logEntry := log.Entry(ctx).WithField("Client", "StartDeviceServer")

	socket := uuid.New().String()
	socketPath := socketpath.SocketPath(path.Join(c.devicePluginPath, socket))
	logEntry.Infof("socket = %v", socket)
	if err := socketpath.SocketCleanup(socketPath); err != nil {
		return "", err
	}

	grpcServer := grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(grpcServer, deviceServer)

	socketURL := grpcutils.AddressToURL(socketPath)
	errCh := grpcutils.ListenAndServe(ctx, socketURL, grpcServer)
	select {
	case err := <-errCh:
		return "", errors.Wrap(err, "failed to start device plugin server")
	default:
	}
	go func() {
		if err := <-errCh; err != nil {
			logEntry.Fatalf("error in device plugin server at %s: %s", socket, err.Error())
		}
	}()

	dialCtx, cancel := context.WithTimeout(ctx, dialTimeoutDefault)
	defer cancel()

	logEntry.Info("check device server operational")
	conn, err := grpc.DialContext(dialCtx, socketURL.String(), grpc.WithBlock(), grpc.WithInsecure())
	if err != nil {
		logEntry.Errorf("failed to dial kubelet api: %s", err.Error())
		return "", err
	}
	_ = conn.Close()

	logEntry.Info("device server is operational")

	return socket, nil
}

// RegisterDeviceServer registers device plugin server using the given request
func (c *Client) RegisterDeviceServer(ctx context.Context, request *pluginapi.RegisterRequest) error {
	logEntry := log.Entry(ctx).WithField("Client", "RegisterDeviceServer")

	socketURL := grpcutils.AddressToURL(socketpath.SocketPath(c.devicePluginSocket))
	conn, err := grpc.DialContext(ctx, socketURL.String(), grpc.WithInsecure())
	if err != nil {
		return errors.Wrap(err, "cannot connect to device plugin kubelet service")
	}
	defer func() { _ = conn.Close() }()

	client := pluginapi.NewRegistrationClient(conn)
	logEntry.Info("trying to register to device plugin kubelet service")
	if _, err = client.Register(context.Background(), request); err != nil {
		return errors.Wrap(err, "cannot register to device plugin kubelet service")
	}
	logEntry.Info("register done")

	return nil
}

// MonitorKubeletRestart monitors if kubelet restarts so we need to re register device plugin server
func (c *Client) MonitorKubeletRestart(ctx context.Context) (chan struct{}, error) {
	logEntry := log.Entry(ctx).WithField("Client", "MonitorKubeletRestart")

	watcher, err := watchOn(c.devicePluginPath)
	if err != nil {
		logEntry.Errorf("failed to watch on %v", c.devicePluginPath)
		return nil, err
	}

	monitorCh := make(chan struct{}, 1)
	go func() {
		defer func() { _ = watcher.Close() }()
		defer close(monitorCh)
		for {
			select {
			case <-ctx.Done():
				logEntry.Info("end monitoring")
				return
			case event, ok := <-watcher.Events:
				if !ok {
					logEntry.Info("watcher has been closed")
					return
				}
				if event.Name == c.devicePluginSocket && event.Op&fsnotify.Create == fsnotify.Create {
					logEntry.Warn("kubelet restarts")
					monitorCh <- struct{}{}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					logEntry.Info("watcher has been closed")
					return
				}
				logEntry.Warn(err)
			}
		}
	}()
	return monitorCh, nil
}

func watchOn(paths ...string) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	for _, path := range paths {
		if err := watcher.Add(path); err != nil {
			_ = watcher.Close()
			return nil, err
		}
	}

	return watcher, nil
}

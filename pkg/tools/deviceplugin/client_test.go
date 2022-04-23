// Copyright (c) 2022 Cisco and/or its affiliates.
//
// Copyright (c) 2020-2021 Doc.ai and/or its affiliates.
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

package deviceplugin_test

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/networkservicemesh/sdk-k8s/pkg/tools/deviceplugin"
)

const (
	kubeletSocket = "kubelet.sock"
	timeout       = time.Second
)

func TestDevicePluginManager_MonitorKubeletRestart(t *testing.T) {
	devicePluginPath := path.Join(os.TempDir(), t.Name())
	devicePluginSocket := path.Join(devicePluginPath, kubeletSocket)

	c := deviceplugin.NewClient(devicePluginPath)

	_ = os.RemoveAll(devicePluginPath)
	err := os.MkdirAll(devicePluginPath, os.ModeDir|os.ModePerm)
	require.NoError(t, err)

	monitorCh, err := c.MonitorKubeletRestart(context.Background())
	require.NoError(t, err)

	_, err = os.Create(filepath.Clean(devicePluginSocket))
	require.NoError(t, err)

	select {
	case <-monitorCh:
	case <-time.After(timeout):
		require.Fail(t, "no update event before timeout")
	}
}

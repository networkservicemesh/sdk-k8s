// Copyright (c) 2020 Doc.ai and/or its affiliates.
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

// Package k8s contains different functions for kubernetes interfaces and structures
package k8s

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClientSetConfig creates ClientSetConfig from config file.
// If config file path not provided via env variable KUBECONFIG, default path "HOME/.kube/config" will be used
func NewClientSetConfig() (*rest.Config, error) {
	configPath := os.Getenv("KUBECONFIG")
	if configPath == "" {
		configPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		logrus.Infof("Unable to get in cluster config, attempting to fall back to kubeconfig: %v", errors.WithStack(err))
		config, err = clientcmd.BuildConfigFromFlags("", configPath)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to build a config from a %s", configPath)
		}
	}

	return config, nil
}

// NewVersionedClient creates a new networkservicemesh.io ClietSet for the default kubernetes config.
func NewVersionedClient() (versioned.Interface, *rest.Config, error) {
	config, err := NewClientSetConfig()
	if err != nil {
		return nil, nil, err
	}
	nsmClientSet, err := versioned.NewForConfig(config)
	return nsmClientSet, config, errors.Wrapf(err, "failed to create a new Clientset for the given config %s", config.String())
}

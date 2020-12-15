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

// Package client provides helper function to build versioned networkservicemesh.io client.
package client

import (
	"flag"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned"
)

// NewClientSet creates a new Clientset for the default kubernetes config.
func NewClientSet() (versioned.Interface, *rest.Config, error) {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// check if CRD is installed
	config, err := rest.InClusterConfig()
	if err != nil {
		logrus.Println("Unable to get in cluster config, attempting to fall back to kubeconfig", err)
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			return nil, nil, errors.Wrap(err, "unable to build config")
		}
	}

	nsmClientSet, err := versioned.NewForConfig(config)
	return nsmClientSet, config, err
}

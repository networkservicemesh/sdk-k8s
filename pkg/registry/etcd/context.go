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

package etcd

import (
	"context"
	"runtime/trace"

	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client"
	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned"
	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned/fake"
)

type contextKey string

const (
	clientKey    contextKey = "client"
	namesapceKey contextKey = "namespace"
)

// WithClientSet stores in ctx  versioned.Interface to use it in servers
func WithClientSet(parent context.Context, c versioned.Interface) context.Context {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	return context.WithValue(parent, clientKey, c)
}

// WithNamespace stores in ctx namespace to use it in servers
func WithNamespace(parent context.Context, ns string) context.Context {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	return context.WithValue(parent, namesapceKey, ns)
}

// Namespace returns namespace where registry is deployed
func Namespace(parent context.Context) string {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	v := parent.Value(namesapceKey)
	if v == nil {
		return "default"
	}
	return v.(string)
}

// ClientSet returns passed into context versioned.Interface.
// If versioned.Interface is not provided in ctx than creates new versioned.Interface.
func ClientSet(parent context.Context) versioned.Interface {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	val := parent.Value(clientKey)
	if c, ok := val.(versioned.Interface); ok {
		return c
	}
	return defaultClientSet(parent)
}

func defaultClientSet(ctx context.Context) versioned.Interface {
	result, _, _ := client.NewClientSet()

	if result == nil {
		result = fake.NewSimpleClientset()
		trace.Log(ctx, "registry-k8s", "cannot initialize clientset")
	}

	return result
}

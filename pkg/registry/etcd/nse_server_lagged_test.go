// Copyright (c) 2025-2026 OpenInfra Foundation Europe. All rights reserved.
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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/networkservicemesh/api/pkg/api/registry"

	"github.com/networkservicemesh/sdk-k8s/pkg/tools/k8s/client/clientset/versioned/fake"
)

// stuckFindServer is a NetworkServiceEndpoint_FindServer that parks in Send
// until released, emulating a client that stopped reading. Because watch()
// calls Send synchronously, parking in Send stops watch() from draining its
// subscriber channel, so a burst deterministically fills the buffer and marks
// the subscriber lagged (independent of scheduler timing).
type stuckFindServer struct {
	grpc.ServerStream
	ctx     context.Context
	release chan struct{}
}

func (s *stuckFindServer) Send(*registry.NetworkServiceEndpointResponse) error {
	select {
	case <-s.release:
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
	return nil
}

func (s *stuckFindServer) Context() context.Context { return s.ctx }

// TestFindWatchLaggedSubscriberReturnsResourceExhausted verifies the server-side
// contract of the memory-leak fix: a watcher that stops reading while the server
// keeps broadcasting has its Find(Watch=true) call terminated with
// codes.ResourceExhausted, instead of blocking the server or leaking memory.
//
// This test lives in package etcd (not etcd_test) so it can deterministically
// observe the private subscriber list and lagged flag. It avoids the adapter
// path (which discards Find's error and surfaces EOF; the status code only
// propagates over a real gRPC transport) by invoking Find directly.
func TestFindWatchLaggedSubscriberReturnsResourceExhausted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Empty clientset: the initial List in Find sends nothing, so the stuck
	// server's first Send is a watch event (not a List result), and watch() is
	// reached and subscribes.
	myClientset := fake.NewSimpleClientset()
	s := NewNetworkServiceEndpointRegistryServer(ctx, "", myClientset)
	reg := s.(*etcdNSERegistryServer)

	srv := &stuckFindServer{
		ctx:     ctx,
		release: make(chan struct{}),
	}

	findErrCh := make(chan error, 1)
	go func() {
		findErrCh <- s.Find(&registry.NetworkServiceEndpointQuery{
			NetworkServiceEndpoint: &registry.NetworkServiceEndpoint{},
			Watch:                  true,
		}, srv)
	}()

	// Wait until watch() has subscribed (its node is in the list). Read behind
	// the subscribersExecutor barrier so there is no concurrent mutation.
	require.Eventually(t, func() bool {
		var n int
		<-reg.subscribersExecutor.AsyncExec(func() { n = reg.subscribers.Len() })
		return n == 1
	}, 10*time.Second, 2*time.Millisecond, "watch never subscribed")

	// Drive the broadcast deterministically by calling the production sendEvent
	// directly (on the subscribersExecutor, exactly as watchRemoteStorage does).
	// This avoids depending on fake-watcher/goroutine scheduling and avoids
	// RaceFreeFakeWatcher's panic-on-full behavior. The stuck server parks in
	// its first Send, so watch() stops draining the subscriber channel and the
	// burst (> nseUpdateChannelSize) deterministically fills it, marking the
	// subscriber lagged.
	resp := &registry.NetworkServiceEndpointResponse{
		NetworkServiceEndpoint: &registry.NetworkServiceEndpoint{Name: "burst"},
	}
	for i := 0; i < nseUpdateChannelSize+16; i++ {
		<-reg.subscribersExecutor.AsyncExec(func() { reg.sendEvent(resp) })
	}

	// Confirm the subscriber is marked lagged (behind the executor barrier).
	require.Eventually(t, func() bool {
		lagged := false
		<-reg.subscribersExecutor.AsyncExec(func() {
			for curr := reg.subscribers.Front(); curr != nil; curr = curr.Next() {
				if curr.Value.(*nseSubscriberNode).lagged.Load() {
					lagged = true
					return
				}
			}
		})
		return lagged
	}, 10*time.Second, 2*time.Millisecond, "subscriber was never marked lagged")

	// Release the stuck client so watch() advances and observes the lagged flag.
	close(srv.release)

	select {
	case ferr := <-findErrCh:
		require.Equal(t, codes.ResourceExhausted, status.Code(ferr),
			"lagged watcher must terminate with ResourceExhausted, got: %v", ferr)
	case <-ctx.Done():
		t.Fatal("Find did not return after subscriber lagged")
	}
}

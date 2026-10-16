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
	"container/list"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/networkservicemesh/api/pkg/api/registry"
)

// TestSendEventCloseRace stresses the ordering between a watcher's context
// cancellation (which closes its channel) and concurrent sendEvent broadcasts.
// If close(ret) can run before the queued Remove executes, a sendEvent job with
// an earlier ticket could send on a closed channel and panic.
//
// The test is bounded and synchronized: it enqueues a finite number of
// sendEvent jobs (no unbounded producer that could outrun the single-worker
// serialize.Executor and leave a huge backlog), interleaves watcher churn to
// create the race, and then waits — via executor barriers bounded by the
// context deadline — for all in-flight jobs and the per-watcher cleanup
// (Remove+close) to drain, rather than sleeping.
func TestSendEventCloseRace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	n := &etcdNSERegistryServer{
		chainContext:      ctx,
		subscribers:       list.New(),
		updateChannelSize: 1,
	}

	const iterations = 2000
	resp := &registry.NetworkServiceEndpointResponse{NetworkServiceEndpoint: &registry.NetworkServiceEndpoint{Name: "x"}}

	var lastSend <-chan struct{}
	for i := 0; i < iterations; i++ {
		// Subscribe a watcher then immediately cancel it, so its cleanup
		// goroutine (Remove+close) races the interleaved sendEvent broadcast.
		wCtx, wCancel := context.WithCancel(ctx)
		_, _ = n.subscribeOnEvents(wCtx)
		wCancel()

		lastSend = n.subscribersExecutor.AsyncExec(func() { n.sendEvent(resp) })
	}

	// Wait for the last enqueued broadcast to run (bounded by the deadline).
	if lastSend != nil {
		select {
		case <-lastSend:
		case <-ctx.Done():
			t.Fatal("timed out waiting for broadcasts to drain")
		}
	}

	// The per-watcher cleanup goroutines enqueue Remove+close asynchronously
	// after observing ctx.Done(); their enqueue is not directly observable from
	// the test. Poll the subscriber count behind an executor barrier until all
	// watchers are removed (or the deadline fires). The barrier guarantees we
	// read subscribers.Len() with no other executor job running concurrently.
	require.Eventually(t, func() bool {
		var n0 int
		<-n.subscribersExecutor.AsyncExec(func() { n0 = n.subscribers.Len() })
		return n0 == 0
	}, 25*time.Second, 5*time.Millisecond, "all watchers should be removed")
}

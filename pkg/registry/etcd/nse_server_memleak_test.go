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
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/networkservicemesh/api/pkg/api/registry"
)

// TestMemoryLeakWithStuckSubscribers verifies that stuck subscribers do not
// cause memory accumulation. A stuck subscriber is one that stopped reading
// from its channel, causing the buffer to fill up.
//
// The test:
// 1. Creates multiple subscribers with small buffers
// 2. Simulates stuck subscribers (not reading)
// 3. Sends many events through sendEvent()
// 4. Verifies:
//   - sendEvent() completes without blocking
//   - lagged flag is set on stuck subscribers
//   - No new goroutines accumulate
//   - Memory usage stays bounded
func TestMemoryLeakWithStuckSubscribers(t *testing.T) {
	runMemoryLeakTestScenario(t, 100, 1000, 10)
}

func runMemoryLeakTestScenario(t *testing.T, numSubscribers, numEvents, bufferSize int) {
	initialGoroutines := runtime.NumGoroutine()
	var initialMem runtime.MemStats
	runtime.ReadMemStats(&initialMem)

	reg, lagggedCount, cancelFuncs, stuckCount := setupMemoryLeakTest(numSubscribers, bufferSize)
	sendDuration, afterEventsGoroutines, afterEventsAlloc := runMemoryLeakSendPhase(reg, lagggedCount, numEvents)

	for _, cancel := range cancelFuncs {
		cancel()
	}
	time.Sleep(100 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()
	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)

	logMemoryLeakResults(t, numSubscribers, numEvents, sendDuration, initialGoroutines,
		afterEventsGoroutines, finalGoroutines, initialMem.Alloc, afterEventsAlloc, finalMem.Alloc,
		stuckCount.Load(), lagggedCount.Load())

	verifyMemoryLeakResults(t, sendDuration, numEvents, numSubscribers, lagggedCount.Load(),
		initialGoroutines, afterEventsGoroutines, finalGoroutines, initialMem.Alloc,
		afterEventsAlloc)
}

func setupMemoryLeakTest(numSubscribers, bufferSize int) (reg *etcdNSERegistryServer, laggedCount *atomic.Int32, cancelFuncs []context.CancelFunc, stuckCount *atomic.Int32) {
	reg = &etcdNSERegistryServer{
		subscribers: list.New(),
	}
	laggedCount = &atomic.Int32{}
	stuckCount = &atomic.Int32{}
	cancelFuncs = make([]context.CancelFunc, 0, numSubscribers)

	for i := 0; i < numSubscribers; i++ {
		ch := make(chan *registry.NetworkServiceEndpointResponse, bufferSize)
		node := &nseSubscriberNode{
			channel: ch,
		}
		reg.subscribers.PushBack(node)

		if i%2 == 0 {
			ctx, cancel := context.WithCancel(context.Background())
			cancelFuncs = append(cancelFuncs, cancel)
			stuckCount.Add(1)

			go func(subscriber *nseSubscriberNode) {
				ticker := time.NewTicker(5 * time.Second)
				defer ticker.Stop()
				select {
				case <-ticker.C:
					select {
					case <-subscriber.channel:
					default:
					}
				case <-ctx.Done():
					return
				}
			}(node)
		} else {
			go func(subscriber *nseSubscriberNode) {
				for range subscriber.channel {
				}
			}(node)
		}
	}
	return reg, laggedCount, cancelFuncs, stuckCount
}

func runMemoryLeakSendPhase(reg *etcdNSERegistryServer, lagggedCount *atomic.Int32, numEvents int) (duration time.Duration, goroutines int, alloc uint64) {
	startTime := time.Now()
	for eventNum := 0; eventNum < numEvents; eventNum++ {
		resp := &registry.NetworkServiceEndpointResponse{}
		reg.sendEvent(resp)
	}
	sendDuration := time.Since(startTime)

	// Derive the lagged count from the real node state set by sendEvent(),
	// rather than tracking it in a re-implemented loop.
	var lagged int32
	for curr := reg.subscribers.Front(); curr != nil; curr = curr.Next() {
		if curr.Value.(*nseSubscriberNode).lagged.Load() {
			lagged++
		}
	}
	lagggedCount.Store(lagged)

	var afterEventsMem runtime.MemStats
	runtime.ReadMemStats(&afterEventsMem)
	afterEventsGoroutines := runtime.NumGoroutine()

	duration = sendDuration
	goroutines = afterEventsGoroutines
	alloc = afterEventsMem.Alloc
	return
}

func logMemoryLeakResults(t *testing.T, numSubscribers int, numEvents int, sendDuration time.Duration,
	initialGoroutines int, afterEventsGoroutines int, finalGoroutines int,
	initialAlloc uint64, afterEventsAlloc uint64, finalAlloc uint64, stuckCount int32, lagggedCount int32) {
	t.Logf("=== Memory Leak Test Results ===")
	t.Logf("Subscribers: %d (stuck: %d, lagged: %d)", numSubscribers, stuckCount, lagggedCount)
	t.Logf("Events sent: %d", numEvents)
	t.Logf("Send duration: %v", sendDuration)
	t.Logf("Avg time per batch: %v", sendDuration/time.Duration(numEvents))
	t.Logf("Goroutines: initial=%d after=%d final=%d", initialGoroutines, afterEventsGoroutines, finalGoroutines)
	t.Logf("Memory (KB): initial=%d after=%d final=%d", initialAlloc/1024, afterEventsAlloc/1024, finalAlloc/1024)
}

func verifyMemoryLeakResults(t *testing.T, sendDuration time.Duration, numEvents int, numSubscribers int,
	lagggedCount int32, initialGoroutines int, afterEventsGoroutines int, finalGoroutines int,
	initialAlloc uint64, afterEventsAlloc uint64) {
	// 1. sendEvent() must never block - all iterations should be fast
	avgTimePerBatch := sendDuration / time.Duration(numEvents)
	require.Less(t, avgTimePerBatch, 50*time.Millisecond,
		"sendEvent taking too long (indicates blocking or goroutine creation)")

	// 2. Stuck subscribers must be marked as lagged
	require.Greater(t, lagggedCount, int32(40),
		"Expected most stuck subscribers to be marked as lagged")

	// 3. No goroutine explosion
	require.Less(t, afterEventsGoroutines, initialGoroutines+150,
		fmt.Sprintf("Goroutine leak detected: started with %d, now have %d",
			initialGoroutines, afterEventsGoroutines))

	// 4. After cleanup, goroutines return to baseline
	require.Less(t, finalGoroutines-initialGoroutines, 100,
		fmt.Sprintf("Too many goroutines not cleaned: initial %d, final %d, excess %d",
			initialGoroutines, finalGoroutines, finalGoroutines-initialGoroutines))

	// 5. Memory growth bounded.
	// Alloc is a uint64 snapshot of live heap; GC between the two samples can
	// make afterEventsAlloc < initialAlloc. Guard the subtraction so a GC-induced
	// shrink reports 0 growth instead of underflowing uint64 to a huge value
	// (which previously made this assertion flaky under -race). Keep the whole
	// computation in uint64 to avoid an unsafe uint64->int64 conversion.
	var memGrowthMB uint64
	if afterEventsAlloc > initialAlloc {
		memGrowthMB = (afterEventsAlloc - initialAlloc) / 1024 / 1024
	}
	require.Less(t, memGrowthMB, uint64(100),
		fmt.Sprintf("Memory growth too large: %d MB (expected < 100 MB)", memGrowthMB))

	// 6. sendEvent iteration count vs subscribers validates no per-event blocking
	totalIterations := numEvents * numSubscribers
	expectedTime := time.Duration(totalIterations) * time.Microsecond
	require.Less(t, sendDuration, expectedTime*100, // Very conservative upper bound
		fmt.Sprintf("Total send time %v >> expected %v (possible blocking detected)",
			sendDuration, expectedTime*100))
}

// TestLaggedFlagPreventsMemoryAccumulation verifies that the lagged flag
// mechanism prevents the original memory leak where stuck subscribers would
// accumulate indefinitely.
func TestLaggedFlagPreventsMemoryAccumulation(t *testing.T) {
	const numSubscribers = 50
	const numEventBatches = 100
	const bufferSize = 5

	// Baseline
	var initialMem runtime.MemStats
	runtime.ReadMemStats(&initialMem)

	reg := &etcdNSERegistryServer{
		subscribers: list.New(),
	}

	// Create stuck subscribers (they will never read)
	for i := 0; i < numSubscribers; i++ {
		ch := make(chan *registry.NetworkServiceEndpointResponse, bufferSize)
		node := &nseSubscriberNode{
			channel: ch,
		}
		reg.subscribers.PushBack(node)
	}

	// Send many event batches through the real sendEvent().
	for batch := 0; batch < numEventBatches; batch++ {
		resp := &registry.NetworkServiceEndpointResponse{}

		reg.sendEvent(resp)

		// Check memory periodically
		if batch%25 == 0 {
			var memNow runtime.MemStats
			runtime.ReadMemStats(&memNow)

			var memGrowthMB uint64
			if memNow.Alloc > initialMem.Alloc {
				memGrowthMB = (memNow.Alloc - initialMem.Alloc) / 1024 / 1024
			}
			t.Logf("Batch %d: Memory growth = %d MB", batch, memGrowthMB)

			// Memory should not grow linearly with batches
			// With proper bounded behavior, should be roughly constant
			if batch > 0 {
				require.Less(t, memGrowthMB, uint64(50),
					fmt.Sprintf("Batch %d: Memory growing too fast (%d MB)",
						batch, memGrowthMB))
			}
		}
	}

	// Final check
	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)

	var totalMemGrowth uint64
	if finalMem.Alloc > initialMem.Alloc {
		totalMemGrowth = (finalMem.Alloc - initialMem.Alloc) / 1024 / 1024
	}
	t.Logf("Total memory growth: %d MB", totalMemGrowth)

	// Memory should be bounded (roughly proportional to buffer size * subscribers)
	// Not proportional to number of events
	expectedMaxGrowth := uint64(50) // Conservative estimate for buffers
	require.Less(t, totalMemGrowth, expectedMaxGrowth,
		fmt.Sprintf("Memory leak detected: %d MB growth after %d event batches",
			totalMemGrowth, numEventBatches))
}

// TestSendEventNeverBlocksUnderLoad verifies that sendEvent() completes
// instantly even when many subscribers have full buffers.
func TestSendEventNeverBlocksUnderLoad(t *testing.T) {
	const numSubscribers = 500
	const numEvents = 100
	const bufferSize = 1

	reg := &etcdNSERegistryServer{
		subscribers: list.New(),
	}

	// Fill buffers - create subscribers with buffers already full
	for i := 0; i < numSubscribers; i++ {
		ch := make(chan *registry.NetworkServiceEndpointResponse, bufferSize)
		node := &nseSubscriberNode{
			channel: ch,
		}

		// Fill the buffer
		select {
		case ch <- &registry.NetworkServiceEndpointResponse{}:
		default:
		}

		reg.subscribers.PushBack(node)
	}

	// Now try to send events - should be instant
	var slowCount int32
	var maxTime time.Duration
	var mu sync.Mutex

	for eventNum := 0; eventNum < numEvents; eventNum++ {
		resp := &registry.NetworkServiceEndpointResponse{}
		startEvent := time.Now()

		// Call the real sendEvent() with all buffers full.
		reg.sendEvent(resp)

		eventTime := time.Since(startEvent)

		mu.Lock()
		if eventTime > maxTime {
			maxTime = eventTime
		}
		mu.Unlock()

		// Should complete in < 50ms
		if eventTime > 50*time.Millisecond {
			atomic.AddInt32(&slowCount, 1)
		}

		// Hard requirement: never > 500ms
		require.Less(t, eventTime, 500*time.Millisecond,
			fmt.Sprintf("Event %d took %v (should be instant with non-blocking sends)",
				eventNum, eventTime))
	}

	t.Logf("Slow events (>50ms): %d / %d", slowCount, numEvents)
	t.Logf("Max time per event: %v", maxTime)

	// With non-blocking sends, even under heavy load:
	// - Max time should be < 100ms
	// - Very few events should take >50ms
	require.Less(t, maxTime, 100*time.Millisecond,
		"Max event time indicates possible blocking")

	require.Less(t, slowCount, int32(5),
		fmt.Sprintf("Too many slow events (%d), indicates blocking", slowCount))
}

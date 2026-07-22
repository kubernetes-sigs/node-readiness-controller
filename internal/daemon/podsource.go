/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package daemon

import (
	"context"
	"sync"

	corev1 "k8s.io/api/core/v1"
)

// EventType mirrors the Kubelet PodInfoAPI WatchPods event kinds. We define our
// own enum rather than depend on the alpha proto so the rest of the daemon is
// buildable and testable before the v1.36 proto is pinned.
type EventType int

const (
	// EventAdded is a pod appearing in the kubelet cache.
	EventAdded EventType = iota
	// EventModified is a pod state change.
	EventModified
	// EventDeleted is a pod leaving the kubelet cache.
	EventDeleted
	// EventInitialSyncComplete signals the initial set has been fully delivered.
	// The daemon must not assert any condition before observing this.
	EventInitialSyncComplete
)

func (e EventType) String() string {
	switch e {
	case EventAdded:
		return "ADDED"
	case EventModified:
		return "MODIFIED"
	case EventDeleted:
		return "DELETED"
	case EventInitialSyncComplete:
		return "INITIAL_SYNC_COMPLETE"
	default:
		return "UNKNOWN"
	}
}

// PodEvent is a single node-local pod update. Pod is nil for
// EventInitialSyncComplete.
type PodEvent struct {
	Type EventType
	Pod  *corev1.Pod
}

// PodSource streams node-local pod events. The real implementation wraps the
// Kubelet PodInfoAPI WatchPods gRPC stream (with reconnect + periodic ListPods
// reconciliation); FakePodSource replays scripted events for tests and demos.
//
// Watch returns a channel that is closed when the stream ends (ctx cancelled or
// terminal error). Implementations should re-emit EventInitialSyncComplete after
// any internal reconnect+resync so the daemon can re-gate assertions.
type PodSource interface {
	Watch(ctx context.Context) (<-chan PodEvent, error)
}

// FakePodSource replays a fixed script of events, then (by default) blocks
// until ctx is done. It is TEST/DEMO ONLY: each Watch call that leaves the
// channel open parks a goroutine until ctx cancels, which is fine for a test
// or a --source=fake demo run but is not a production-quality source.
type FakePodSource struct {
	// Script is the ordered sequence of events to emit on Watch.
	Script []PodEvent
	// CloseAfterScript closes the channel immediately after the script is
	// emitted, simulating a stream that ends (Daemon.Run then resets the cache,
	// backs off, and reconnects).
	CloseAfterScript bool

	mu         sync.Mutex
	watchCalls int
}

// WatchCalls returns how many times Watch has been invoked (safe for
// concurrent use; Run-level tests use it to assert reconnect backoff).
func (f *FakePodSource) WatchCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.watchCalls
}

// Watch emits the script on a buffered channel, then either closes it
// (CloseAfterScript) or leaves it open until ctx ends.
func (f *FakePodSource) Watch(ctx context.Context) (<-chan PodEvent, error) {
	f.mu.Lock()
	f.watchCalls++
	f.mu.Unlock()

	// Buffer exactly fits the script: every send below happens before any
	// receive, so the channel must hold the whole script.
	ch := make(chan PodEvent, len(f.Script))
	for _, ev := range f.Script {
		ch <- ev
	}
	if f.CloseAfterScript {
		close(ch)
		return ch, nil
	}
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

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
	"testing"
	"time"

	"go.uber.org/goleak"
	corev1 "k8s.io/api/core/v1"
	clocktesting "k8s.io/utils/clock/testing"
)

// verifyNoLeaks wraps goleak, ignoring klog's background flush daemon.
func verifyNoLeaks(t *testing.T) {
	t.Helper()
	goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("k8s.io/klog/v2.(*flushDaemon).run.func1"))
}

// waitFor polls cond (real time) until it holds or the test times out. Used to
// wait for the Run goroutine to reach a quiescent state on the fake clock.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// startRun launches d.Run and returns a cancel-and-wait cleanup.
func startRun(t *testing.T, d *Daemon) (cancel func()) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = d.Run(ctx)
	}()
	return func() {
		stop()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	}
}

// A source whose stream ends immediately must be reconnected WITH backoff:
// while the fake clock is frozen the daemon must park in the backoff sleep, not
// hot-loop Watch (GB1 — grpc.NewClient dials lazily, so a missing kubelet
// socket presents exactly as an immediately-closed channel).
func TestRunReconnectsWithBackoff(t *testing.T) {
	defer verifyNoLeaks(t)

	clk := clocktesting.NewFakeClock(time.Now())
	src := &FakePodSource{CloseAfterScript: true}
	w := NewFakeConditionWriter()
	d := New(Options{
		Source:            src,
		Store:             NewConfigStore(t.TempDir()),
		Writer:            w,
		Clock:             clk,
		ReconcileInterval: time.Hour, // keep tickers out of the way
		ResyncInterval:    time.Hour,
	})
	cancel := startRun(t, d)
	defer cancel()

	// First Watch, stream closes, daemon parks in the backoff sleep:
	// waiters = reconcile ticker + resync ticker + sleep timer.
	waitFor(t, "first Watch and backoff sleep", func() bool {
		return src.WatchCalls() == 1 && clk.Waiters() == 3
	})
	// Clock frozen => no reconnect may happen (bounded Watch count, no hot loop).
	time.Sleep(50 * time.Millisecond)
	if got := src.WatchCalls(); got != 1 {
		t.Fatalf("WatchCalls = %d while clock frozen; want 1 (hot reconnect loop)", got)
	}

	// First backoff step is 500ms; stepping 1s releases exactly one reconnect.
	clk.Step(time.Second)
	waitFor(t, "second Watch and backoff sleep", func() bool {
		return src.WatchCalls() == 2 && clk.Waiters() == 3
	})
	time.Sleep(50 * time.Millisecond)
	if got := src.WatchCalls(); got != 2 {
		t.Fatalf("WatchCalls = %d after one backoff step; want 2 (reconnect without backoff)", got)
	}
}

// N pod events in a burst must coalesce into one reconcile (one writer Sync)
// when the coalescing window elapses, not one Sync per event.
func TestRunCoalescesEventBurst(t *testing.T) {
	defer verifyNoLeaks(t)

	dir := t.TempDir()
	writeFile(t, dir, "cni.yaml",
		"conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\ndebounce:\n  healthyThreshold: 30s\n")

	clk := clocktesting.NewFakeClock(time.Now())
	p := pod("kube-system", "cilium", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true)
	src := &FakePodSource{Script: []PodEvent{
		{Type: EventAdded, Pod: p},
		{Type: EventInitialSyncComplete},
		{Type: EventModified, Pod: p},
		{Type: EventModified, Pod: p},
		{Type: EventModified, Pod: p},
	}}
	w := NewFakeConditionWriter()
	d := New(Options{
		Source:            src,
		Store:             NewConfigStore(dir),
		Writer:            w,
		Clock:             clk,
		ReconcileInterval: time.Hour,
		ResyncInterval:    time.Hour,
	})
	cancel := startRun(t, d)
	defer cancel()

	// After the whole script drains: sync-complete reconciled once (Calls=1),
	// the 3-event burst armed the coalescing timer, and the pending debounce
	// (healthy 30s) armed the wake timer: 2 tickers + coalesce + wake = 4.
	waitFor(t, "burst absorbed with one reconcile", func() bool {
		return w.CallCount() == 1 && clk.Waiters() == 4
	})
	time.Sleep(50 * time.Millisecond)
	if got := w.CallCount(); got != 1 {
		t.Fatalf("writer Sync called %d times during burst; want 1 (per-event reconciles, no coalescing)", got)
	}

	// Elapse the coalescing window: the whole burst collapses into ONE reconcile.
	clk.Step(coalesceDelay)
	waitFor(t, "coalesced reconcile", func() bool { return w.CallCount() == 2 })
	time.Sleep(50 * time.Millisecond)
	if got := w.CallCount(); got != 2 {
		t.Fatalf("writer Sync called %d times after window; want exactly 2", got)
	}
}

// A healthyThreshold shorter than the reconcile tick must resolve at ~threshold
// via the wake timer, not be quantized to the next tick.
func TestRunWakeTimerResolvesSubTickThreshold(t *testing.T) {
	defer verifyNoLeaks(t)

	dir := t.TempDir()
	writeFile(t, dir, "cni.yaml",
		"conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\ndebounce:\n  healthyThreshold: 3s\n")

	clk := clocktesting.NewFakeClock(time.Now())
	src := &FakePodSource{Script: []PodEvent{
		{Type: EventAdded, Pod: pod("kube-system", "cilium", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true)},
		{Type: EventInitialSyncComplete},
	}}
	w := NewFakeConditionWriter()
	d := New(Options{
		Source:            src,
		Store:             NewConfigStore(dir),
		Writer:            w,
		Clock:             clk,
		ReconcileInterval: 10 * time.Second, // the tick alone could not resolve before t+10s
		ResyncInterval:    time.Hour,
	})
	cancel := startRun(t, d)
	defer cancel()

	// Post-sync steady state: first reconcile done (pending), wake timer armed
	// at pendingSince+3s: 2 tickers + wake = 3 waiters.
	waitFor(t, "initial reconcile and wake timer", func() bool {
		return w.CallCount() >= 1 && clk.Waiters() == 3
	})
	if _, ok := w.Get("readiness.k8s.io/CNIReady"); ok {
		t.Fatalf("condition published before threshold elapsed")
	}

	// Advance only 3s (well short of the 10s tick): the wake timer must fire
	// and the condition must be published now.
	clk.Step(3 * time.Second)
	waitFor(t, "condition published at ~threshold", func() bool {
		got, ok := w.Get("readiness.k8s.io/CNIReady")
		return ok && got.Status == corev1.ConditionTrue
	})
}

// The slow resync tick must force the writer to reapply even when the input is
// unchanged (drift repair). Also exercises the zero-threshold past-due wake
// loop: healthyThreshold 0 publishes within one reconcileNow pass.
func TestRunResyncForcesReapply(t *testing.T) {
	defer verifyNoLeaks(t)

	dir := t.TempDir()
	writeFile(t, dir, "cni.yaml",
		"conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\ndebounce:\n  healthyThreshold: 0s\n  unhealthyThreshold: 0s\n")

	clk := clocktesting.NewFakeClock(time.Now())
	src := &FakePodSource{Script: []PodEvent{
		{Type: EventAdded, Pod: pod("kube-system", "cilium", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true)},
		{Type: EventInitialSyncComplete},
	}}
	w := NewFakeConditionWriter()
	d := New(Options{
		Source:            src,
		Store:             NewConfigStore(dir),
		Writer:            w,
		Clock:             clk,
		ReconcileInterval: time.Hour,
		ResyncInterval:    time.Minute,
	})
	cancel := startRun(t, d)
	defer cancel()

	// Zero threshold: the past-due wake loop resolves it immediately (two
	// reconciles inside one reconcileNow: arm pending, then commit).
	waitFor(t, "condition published with zero threshold", func() bool {
		got, ok := w.Get("readiness.k8s.io/CNIReady")
		return ok && got.Status == corev1.ConditionTrue
	})
	if w.ForceCallCount() != 0 {
		t.Fatalf("ForceNextSync called before resync tick")
	}
	before := w.CallCount()

	// Resync tick: writer memo cleared + a reconcile even though nothing changed.
	clk.Step(time.Minute)
	waitFor(t, "forced resync", func() bool {
		return w.ForceCallCount() == 1 && w.CallCount() > before
	})
}

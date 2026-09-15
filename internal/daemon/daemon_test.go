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
	"errors"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	clocktesting "k8s.io/utils/clock/testing"
)

// newTestDaemon wires a daemon with a fake writer + fake clock against a config dir.
func newTestDaemon(t *testing.T, dir string) (*Daemon, *FakeConditionWriter, *clocktesting.FakeClock) {
	t.Helper()
	clk := clocktesting.NewFakeClock(time.Now())
	w := NewFakeConditionWriter()
	d := New(Options{
		Source:            &FakePodSource{}, // unused; we drive Reconcile + cache directly
		Store:             NewConfigStore(dir),
		Writer:            w,
		Clock:             clk,
		ReconcileInterval: time.Second,
	})
	return d, w, clk
}

func TestDaemonGatesOnInitialSync(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cni.yaml",
		"conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\n")
	d, w, _ := newTestDaemon(t, dir)
	ctx := context.Background()

	// Cache not synced yet: reconcile must not write anything.
	d.cache.Apply(PodEvent{Type: EventAdded, Pod: pod("kube-system", "cilium", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true)})
	d.Reconcile(ctx)
	if w.Calls != 0 {
		t.Fatalf("writer called %d times before sync; want 0 (fail-closed gating)", w.Calls)
	}
}

func TestDaemonPublishesTrueAfterSustainedHealthy(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cni.yaml",
		"conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\ndebounce:\n  healthyThreshold: 30s\n")
	d, w, clk := newTestDaemon(t, dir)
	ctx := context.Background()

	d.cache.Apply(PodEvent{Type: EventAdded, Pod: pod("kube-system", "cilium", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true)})
	d.cache.Apply(PodEvent{Type: EventInitialSyncComplete})

	// First reconcile: healthy observed but debounce pending => no True yet (absent/Unknown).
	d.Reconcile(ctx)
	if _, ok := w.Get("readiness.k8s.io/CNIReady"); ok {
		t.Fatalf("condition published before healthyThreshold elapsed")
	}
	if !w.LastPending.Has("readiness.k8s.io/CNIReady") {
		t.Fatalf("mid-debounce condition should be passed as pending, got %v", w.LastPending)
	}

	// Advance past threshold and reconcile again => True.
	clk.Step(31 * time.Second)
	d.Reconcile(ctx)
	got, ok := w.Get("readiness.k8s.io/CNIReady")
	if !ok {
		t.Fatalf("condition not published after threshold")
	}
	if got.Status != corev1.ConditionTrue {
		t.Errorf("status = %v, want True", got.Status)
	}
}

func TestDaemonFailClosedWhenPodMissing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cni.yaml",
		"conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\ndebounce:\n  unhealthyThreshold: 10s\n")
	d, w, clk := newTestDaemon(t, dir)
	ctx := context.Background()

	// Synced, but no matching pod => unhealthy. After unhealthyThreshold => False.
	d.cache.Apply(PodEvent{Type: EventInitialSyncComplete})
	d.Reconcile(ctx)
	clk.Step(11 * time.Second)
	d.Reconcile(ctx)

	got, ok := w.Get("readiness.k8s.io/CNIReady")
	if !ok {
		t.Fatalf("expected a published condition")
	}
	if got.Status != corev1.ConditionFalse {
		t.Errorf("status = %v, want False (no critical pod => fail-closed)", got.Status)
	}
}

func TestDaemonPrunesConditionOnConfigRemoval(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "cni.yaml",
		"conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\ndebounce:\n  healthyThreshold: 0s\n")
	d, w, clk := newTestDaemon(t, dir)
	ctx := context.Background()

	d.cache.Apply(PodEvent{Type: EventAdded, Pod: pod("kube-system", "cilium", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true)})
	d.cache.Apply(PodEvent{Type: EventInitialSyncComplete})
	d.Reconcile(ctx)
	clk.Step(time.Second)
	d.Reconcile(ctx)
	if _, ok := w.Get("readiness.k8s.io/CNIReady"); !ok {
		t.Fatalf("setup: condition should be published")
	}

	// Remove the config file: the condition must be pruned from the node.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	d.Reconcile(ctx)
	if _, ok := w.Get("readiness.k8s.io/CNIReady"); ok {
		t.Errorf("condition should be pruned after config removal")
	}
	if w.LastPending.Has("readiness.k8s.io/CNIReady") {
		t.Errorf("removed config's type must not be passed as pending (that would preserve it)")
	}
}

// Regression: a daemon restart must NOT prune a condition that is already
// published and whose config is still loaded but mid-debounce. Otherwise NRC
// sees the condition vanish and re-taints the node (eviction foot-gun).
func TestDaemonRestartDoesNotPrunePublishedCondition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cni.yaml",
		"conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\ndebounce:\n  healthyThreshold: 30s\n")
	d, w, _ := newTestDaemon(t, dir)
	ctx := context.Background()

	// Simulate "already published True from a prior daemon incarnation".
	w.Last["readiness.k8s.io/CNIReady"] = DesiredCondition{Type: "readiness.k8s.io/CNIReady", Status: corev1.ConditionTrue}

	// Fresh daemon: healthy pod, sync, ONE reconcile (no clock step => debounce pending).
	d.cache.Apply(PodEvent{Type: EventAdded, Pod: pod("kube-system", "cilium", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true)})
	d.cache.Apply(PodEvent{Type: EventInitialSyncComplete})
	d.Reconcile(ctx)

	got, ok := w.Get("readiness.k8s.io/CNIReady")
	if !ok {
		t.Fatalf("published condition was pruned on restart (debounce pending); NRC would re-taint")
	}
	if got.Status != corev1.ConditionTrue {
		t.Errorf("status = %v, want True preserved across restart", got.Status)
	}
}

// A config reload that changes debounce thresholds must re-tune the existing
// debouncer (not keep the stale thresholds until restart).
func TestDaemonRetunesDebouncerOnConfigReload(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cni.yaml",
		"conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\ndebounce:\n  healthyThreshold: 30s\n")
	d, w, clk := newTestDaemon(t, dir)
	ctx := context.Background()

	d.cache.Apply(PodEvent{Type: EventAdded, Pod: pod("kube-system", "cilium", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true)})
	d.cache.Apply(PodEvent{Type: EventInitialSyncComplete})
	d.Reconcile(ctx) // arms the healthy-pending timer under the 30s threshold

	// Operator shortens the threshold to 5s; 6s of accumulated sustain must
	// commit on the next (config-reloading) reconcile.
	writeFile(t, dir, "cni.yaml",
		"conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\ndebounce:\n  healthyThreshold: 5s\n")
	clk.Step(6 * time.Second)
	d.Reconcile(ctx)

	got, ok := w.Get("readiness.k8s.io/CNIReady")
	if !ok {
		t.Fatalf("condition not published after retuned threshold elapsed (stale 30s threshold still in effect?)")
	}
	if got.Status != corev1.ConditionTrue {
		t.Errorf("status = %v, want True", got.Status)
	}
}

// A writer error must not wedge the daemon: the next reconcile retries the
// same publish and succeeds once the writer recovers.
func TestDaemonRetriesAfterWriterError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cni.yaml",
		"conditionType: readiness.k8s.io/CNIReady\nselector:\n  matchLabels: {k8s-app: cilium}\ndebounce:\n  healthyThreshold: 0s\n")
	d, w, clk := newTestDaemon(t, dir)
	ctx := context.Background()

	d.cache.Apply(PodEvent{Type: EventAdded, Pod: pod("kube-system", "cilium", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true)})
	d.cache.Apply(PodEvent{Type: EventInitialSyncComplete})
	d.Reconcile(ctx) // arms the zero-threshold pending timer

	// Control plane unreachable while the transition commits.
	w.FailWith = errors.New("apiserver unreachable")
	clk.Step(time.Second)
	d.Reconcile(ctx)
	if _, ok := w.Get("readiness.k8s.io/CNIReady"); ok {
		t.Fatalf("condition recorded despite writer error")
	}

	// Writer recovers: the next reconcile retries and publishes.
	w.FailWith = nil
	d.Reconcile(ctx)
	got, ok := w.Get("readiness.k8s.io/CNIReady")
	if !ok {
		t.Fatalf("condition not republished after writer recovered")
	}
	if got.Status != corev1.ConditionTrue {
		t.Errorf("status = %v, want True", got.Status)
	}
}

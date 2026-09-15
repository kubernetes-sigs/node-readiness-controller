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
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestCacheSyncGating(t *testing.T) {
	c := NewCache()
	if c.Synced() {
		t.Fatal("new cache must not be synced")
	}
	c.Apply(PodEvent{Type: EventAdded, Pod: pod("ns", "a", nil, corev1.PodRunning, true)})
	if c.Synced() {
		t.Fatal("adding a pod must not flip synced")
	}
	c.Apply(PodEvent{Type: EventInitialSyncComplete})
	if !c.Synced() {
		t.Fatal("INITIAL_SYNC_COMPLETE must flip synced")
	}
}

func TestCacheApplyLifecycle(t *testing.T) {
	c := NewCache()
	p := pod("ns", "a", nil, corev1.PodRunning, false)

	c.Apply(PodEvent{Type: EventAdded, Pod: p})
	if len(c.Snapshot()) != 1 {
		t.Fatalf("after ADDED, snapshot len = %d, want 1", len(c.Snapshot()))
	}

	// MODIFIED replaces by UID.
	p2 := pod("ns", "a", nil, corev1.PodRunning, true)
	c.Apply(PodEvent{Type: EventModified, Pod: p2})
	snap := c.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("after MODIFIED, len = %d, want 1", len(snap))
	}
	if !podReady(snap[0]) {
		t.Errorf("MODIFIED did not update pod state")
	}

	// DELETED removes.
	c.Apply(PodEvent{Type: EventDeleted, Pod: p2})
	if len(c.Snapshot()) != 0 {
		t.Fatalf("after DELETED, len = %d, want 0", len(c.Snapshot()))
	}
}

func TestCacheTrimsPodsButPreservesEvaluation(t *testing.T) {
	c := NewCache()
	full := pod("kube-system", "cni-x", map[string]string{"app": "cni"}, corev1.PodRunning, true)
	// Heavy fields the evaluator never reads must be dropped by the cache.
	full.Spec.Containers = []corev1.Container{{Name: "agent", Image: "cni:latest",
		Env: []corev1.EnvVar{{Name: "BIG", Value: "blob"}}}}
	full.Annotations = map[string]string{"huge": "annotation"}
	full.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name: "agent", Ready: false,
		State:   corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
		ImageID: "sha256:deadbeef",
	}}

	c.Apply(PodEvent{Type: EventAdded, Pod: full})
	got := c.Snapshot()[0]

	if len(got.Spec.Containers) != 0 || got.Annotations != nil || got.Status.ContainerStatuses[0].ImageID != "" {
		t.Error("cache must not retain spec/annotations/imageID")
	}
	if got.Namespace != full.Namespace || got.Name != full.Name || got.UID != full.UID ||
		got.Labels["app"] != "cni" {
		t.Error("trim dropped identity/label fields the evaluator needs")
	}
	if !podReady(got) {
		t.Error("trim broke podReady evaluation")
	}
	if r := crashReason(&corev1.Pod{Status: got.Status}); r == "" {
		t.Error("trim broke crashReason inputs")
	}
}

func TestCacheReset(t *testing.T) {
	c := NewCache()
	c.Apply(PodEvent{Type: EventAdded, Pod: pod("ns", "a", nil, corev1.PodRunning, true)})
	c.Apply(PodEvent{Type: EventInitialSyncComplete})

	c.Reset()
	if c.Synced() {
		t.Error("reset must clear synced (so daemon waits for fresh sync)")
	}
	if len(c.Snapshot()) != 0 {
		t.Error("reset must clear pods")
	}
}

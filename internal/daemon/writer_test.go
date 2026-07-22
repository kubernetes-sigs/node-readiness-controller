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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/fake"
	clocktesting "k8s.io/utils/clock/testing"
)

func nodeConditions(t *testing.T, client *fake.Clientset) map[string]corev1.NodeCondition {
	t.Helper()
	n, err := client.CoreV1().Nodes().Get(context.Background(), "n1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	m := map[string]corev1.NodeCondition{}
	for _, c := range n.Status.Conditions {
		m[string(c.Type)] = c
	}
	return m
}

// newFakeNode returns a fake clientset (with SSA managed-fields support) holding
// one node with the given conditions.
func newFakeNode(conds ...corev1.NodeCondition) *fake.Clientset {
	// fake.NewClientset (unlike the deprecated NewSimpleClientset) tracks managed
	// fields, so ApplyPatchType exercises real SSA merge + prune semantics.
	return fake.NewClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "n1"},
		Status:     corev1.NodeStatus{Conditions: conds},
	})
}

func TestNodeConditionWriterCoalescesAndPreservesUnrelated(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	client := newFakeNode(corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionTrue}) // unrelated, must survive
	w := NewNodeConditionWriter(client, "n1", clk)

	err := w.Sync(context.Background(), []DesiredCondition{
		{Type: "cni.example.io/NetworkReady", Status: corev1.ConditionTrue, Reason: "ok"},
		{Type: "gpu.example.io/DeviceReady", Status: corev1.ConditionFalse, Reason: "pending"},
	}, sets.New[string]())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	conds := nodeConditions(t, client)
	if _, ok := conds[string(corev1.NodeReady)]; !ok {
		t.Errorf("unrelated NodeReady condition was clobbered")
	}
	if conds["cni.example.io/NetworkReady"].Status != corev1.ConditionTrue {
		t.Errorf("NetworkReady = %v, want True", conds["cni.example.io/NetworkReady"].Status)
	}
	if conds["gpu.example.io/DeviceReady"].Status != corev1.ConditionFalse {
		t.Errorf("DeviceReady = %v, want False", conds["gpu.example.io/DeviceReady"].Status)
	}
	// LastHeartbeatTime is deliberately never stamped (it defeats no-op applies).
	if hb := conds["cni.example.io/NetworkReady"].LastHeartbeatTime; !hb.IsZero() {
		t.Errorf("LastHeartbeatTime should not be stamped, got %v", hb)
	}
}

// A condition previously applied by this field manager and later omitted from
// the apply set (config removed) must be pruned by SSA ownership.
func TestNodeConditionWriterPrunesOmittedCondition(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	client := newFakeNode()
	w := NewNodeConditionWriter(client, "n1", clk)

	// First publish both.
	if err := w.Sync(context.Background(), []DesiredCondition{
		{Type: "cni.example.io/NetworkReady", Status: corev1.ConditionTrue},
		{Type: "gpu.example.io/DeviceReady", Status: corev1.ConditionTrue},
	}, sets.New[string]()); err != nil {
		t.Fatal(err)
	}

	// GPU config removed: it is in neither desired nor pending, so the apply set
	// omits it and the server prunes it (no explicit toRemove anymore).
	if err := w.Sync(context.Background(), []DesiredCondition{
		{Type: "cni.example.io/NetworkReady", Status: corev1.ConditionTrue},
	}, sets.New[string]()); err != nil {
		t.Fatal(err)
	}

	conds := nodeConditions(t, client)
	if _, ok := conds["gpu.example.io/DeviceReady"]; ok {
		t.Errorf("DeviceReady should be pruned after being omitted from the apply set")
	}
	if _, ok := conds["cni.example.io/NetworkReady"]; !ok {
		t.Errorf("NetworkReady should remain")
	}
}

func TestNodeConditionWriterPreservesTransitionTime(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	client := newFakeNode()
	w := NewNodeConditionWriter(client, "n1", clk)

	if err := w.Sync(context.Background(), []DesiredCondition{
		{Type: "cni.example.io/NetworkReady", Status: corev1.ConditionTrue, Reason: "ok"},
	}, sets.New[string]()); err != nil {
		t.Fatal(err)
	}
	first := nodeConditions(t, client)["cni.example.io/NetworkReady"].LastTransitionTime

	// Advance time, re-sync with SAME status but a changed Reason (so the write
	// is not short-circuited): transition time must be preserved.
	clk.Step(time.Minute)
	if err := w.Sync(context.Background(), []DesiredCondition{
		{Type: "cni.example.io/NetworkReady", Status: corev1.ConditionTrue, Reason: "still-ok"},
	}, sets.New[string]()); err != nil {
		t.Fatal(err)
	}
	second := nodeConditions(t, client)["cni.example.io/NetworkReady"]
	if !second.LastTransitionTime.Equal(&first) {
		t.Errorf("transition time changed on unchanged status: %v -> %v", first, second.LastTransitionTime)
	}
	if second.Reason != "still-ok" {
		t.Errorf("reason = %q, want still-ok", second.Reason)
	}

	// Now flip status: transition time must update.
	clk.Step(time.Minute)
	if err := w.Sync(context.Background(), []DesiredCondition{
		{Type: "cni.example.io/NetworkReady", Status: corev1.ConditionFalse, Reason: "down"},
	}, sets.New[string]()); err != nil {
		t.Fatal(err)
	}
	third := nodeConditions(t, client)["cni.example.io/NetworkReady"]
	if third.LastTransitionTime.Equal(&first) {
		t.Errorf("transition time should change when status flips")
	}
}

// The key scale property: a Sync with input identical to the last successful
// apply must issue ZERO API calls (no Get, no Patch).
func TestNodeConditionWriterUnchangedInputIsZeroAPIActions(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	client := newFakeNode()
	w := NewNodeConditionWriter(client, "n1", clk)

	desired := []DesiredCondition{
		{Type: "cni.example.io/NetworkReady", Status: corev1.ConditionTrue, Reason: "ok", Message: "all good"},
	}
	pending := sets.New("dns.example.io/CoreDNSReady")
	if err := w.Sync(context.Background(), desired, pending); err != nil {
		t.Fatal(err)
	}

	client.ClearActions()
	clk.Step(time.Minute) // time alone must not force a write (no heartbeat)
	if err := w.Sync(context.Background(), desired, pending); err != nil {
		t.Fatal(err)
	}
	if got := client.Actions(); len(got) != 0 {
		t.Errorf("unchanged Sync issued %d API actions, want 0: %v", len(got), got)
	}

	// A real change must write again.
	desired[0].Status = corev1.ConditionFalse
	if err := w.Sync(context.Background(), desired, pending); err != nil {
		t.Fatal(err)
	}
	if len(client.Actions()) == 0 {
		t.Errorf("changed Sync issued no API actions")
	}
}

// A fresh writer (daemon restart) with a config whose debouncer is still
// pending must carry the already-published condition unchanged, not drop it.
func TestNodeConditionWriterPendingPreservedAcrossRestart(t *testing.T) {
	published := corev1.NodeCondition{
		Type:   "cni.example.io/NetworkReady",
		Status: corev1.ConditionTrue,
		Reason: "ok",
		// Second precision: SSA round-trips through JSON (RFC3339 seconds).
		LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second)),
	}
	clk := clocktesting.NewFakeClock(time.Now())
	client := newFakeNode(published)
	w := NewNodeConditionWriter(client, "n1", clk) // fresh writer = fresh in-memory state

	if err := w.Sync(context.Background(), nil, sets.New("cni.example.io/NetworkReady")); err != nil {
		t.Fatal(err)
	}

	got, ok := nodeConditions(t, client)["cni.example.io/NetworkReady"]
	if !ok {
		t.Fatalf("published condition was pruned while pending; NRC would re-taint the node")
	}
	if got.Status != corev1.ConditionTrue || got.Reason != "ok" {
		t.Errorf("condition changed while pending: %+v", got)
	}
	if !got.LastTransitionTime.Equal(&published.LastTransitionTime) {
		t.Errorf("transition time not carried over: %v -> %v", published.LastTransitionTime, got.LastTransitionTime)
	}
}

// A pending type that is NOT currently published stays absent (fail-closed:
// absent reads as Unknown to NRC).
func TestNodeConditionWriterPendingUnpublishedStaysAbsent(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	client := newFakeNode()
	w := NewNodeConditionWriter(client, "n1", clk)

	if err := w.Sync(context.Background(), nil, sets.New("cni.example.io/NetworkReady")); err != nil {
		t.Fatal(err)
	}
	if _, ok := nodeConditions(t, client)["cni.example.io/NetworkReady"]; ok {
		t.Errorf("pending-but-never-published condition should not be created")
	}
}

// ForceNextSync (the daemon's slow-resync hook) must clear the no-op memo so an
// unchanged input reapplies — repairing external tampering with owned conditions.
func TestNodeConditionWriterForceNextSyncReapplies(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	client := newFakeNode()
	w := NewNodeConditionWriter(client, "n1", clk)

	desired := []DesiredCondition{
		{Type: "cni.example.io/NetworkReady", Status: corev1.ConditionTrue, Reason: "ok"},
	}
	if err := w.Sync(context.Background(), desired, sets.New[string]()); err != nil {
		t.Fatal(err)
	}

	// Unchanged input: memoized no-op.
	client.ClearActions()
	if err := w.Sync(context.Background(), desired, sets.New[string]()); err != nil {
		t.Fatal(err)
	}
	if got := client.Actions(); len(got) != 0 {
		t.Fatalf("unchanged Sync issued %d API actions, want 0", len(got))
	}

	// Force: same input must now hit the API again.
	f, ok := w.(interface{ ForceNextSync() })
	if !ok {
		t.Fatalf("nodeConditionWriter does not expose ForceNextSync")
	}
	f.ForceNextSync()
	if err := w.Sync(context.Background(), desired, sets.New[string]()); err != nil {
		t.Fatal(err)
	}
	if len(client.Actions()) == 0 {
		t.Errorf("forced Sync issued no API actions; drift repair broken")
	}
}

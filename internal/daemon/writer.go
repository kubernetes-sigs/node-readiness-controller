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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/clock"
)

// fieldManager is the Server-Side Apply field manager identity for this daemon.
// SSA per-field ownership under this manager is what lets the daemon coexist
// with kubelet's built-in conditions and prune only what it applied itself.
const fieldManager = "node-readiness-daemon"

// DesiredCondition is one node condition the daemon wants published.
type DesiredCondition struct {
	Type    string
	Status  corev1.ConditionStatus
	Reason  string
	Message string
}

// ConditionWriter publishes the daemon's conditions to a Node. Sync applies
// exactly the set: desired conditions, plus — for each type in pending — the
// condition's currently-published value on the Node carried over unchanged
// (pending = config exists but the debouncer has not yet resolved to a stable
// value). Any condition this writer previously applied but that is in neither
// set is pruned. Carrying pending types preserves the invariant that a daemon
// restart mid-debounce never transiently drops a published condition (which
// would make NRC re-taint the node).
type ConditionWriter interface {
	Sync(ctx context.Context, desired []DesiredCondition, pending sets.Set[string]) error
}

// nodeConditionWriter writes via the API server using Server-Side Apply with a
// dedicated field manager, touching only the conditions it owns. Node
// status.conditions is listType=map keyed by type, so SSA merges per-entry and
// the server auto-prunes any condition this manager previously applied but now
// omits — pruning-by-ownership, with no diff bookkeeping across restarts.
//
// Sync is not safe for concurrent use; the daemon's Run loop is the sole caller.
type nodeConditionWriter struct {
	client   kubernetes.Interface
	nodeName string
	clk      clock.Clock

	// lastDesired/lastPending memoize the input of the last SUCCESSFUL apply so
	// an unchanged reconcile is a true no-op (zero API calls). Both are nil until
	// the first Sync, so the first Sync after startup always applies — that is
	// what prunes conditions whose config was removed while the daemon was down.
	lastDesired map[string]DesiredCondition
	lastPending sets.Set[string]
}

// NewNodeConditionWriter returns a writer backed by the given clientset. The
// clientset should carry system:node:<nodeName> impersonation in production so
// NodeRestriction confines writes to this node's own status.
func NewNodeConditionWriter(client kubernetes.Interface, nodeName string, clk clock.Clock) ConditionWriter {
	return &nodeConditionWriter{client: client, nodeName: nodeName, clk: clk}
}

func (w *nodeConditionWriter) Sync(ctx context.Context, desired []DesiredCondition, pending sets.Set[string]) error {
	desiredByType := make(map[string]DesiredCondition, len(desired))
	for _, d := range desired {
		desiredByType[d.Type] = d
	}

	// No-op short-circuit: identical input to the last successful apply means an
	// identical apply set (pending values on the Node are only ever written by
	// this manager), so skip the API entirely. Never taken on the first Sync.
	if w.lastDesired != nil && sameDesired(w.lastDesired, desiredByType) && w.lastPending.Equal(pending) {
		return nil
	}

	// One Get serves both: carrying pending conditions over unchanged, and
	// preserving LastTransitionTime across an unchanged status.
	node, err := w.client.CoreV1().Nodes().Get(ctx, w.nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	current := map[string]corev1.NodeCondition{}
	for _, c := range node.Status.Conditions {
		current[string(c.Type)] = c
	}
	now := metav1.NewTime(w.clk.Now())

	var conds []*corev1apply.NodeConditionApplyConfiguration
	for _, d := range desired {
		lt := now
		// Preserve transition time if status is unchanged. No heartbeat stamping:
		// LastHeartbeatTime is effectively deprecated for node conditions, and
		// stamping it would defeat SSA's unchanged-apply-is-a-no-op property.
		if prev, ok := current[d.Type]; ok && prev.Status == d.Status && !prev.LastTransitionTime.IsZero() {
			lt = prev.LastTransitionTime
		}
		conds = append(conds, corev1apply.NodeCondition().
			WithType(corev1.NodeConditionType(d.Type)).
			WithStatus(d.Status).
			WithLastTransitionTime(lt).
			WithReason(d.Reason).
			WithMessage(d.Message))
	}
	// Pending types: carry the currently-published value unchanged so it stays
	// in this manager's apply set (and is not pruned). Not currently published
	// => nothing to preserve, omit it.
	for _, t := range sets.List(pending) {
		if _, dup := desiredByType[t]; dup {
			continue
		}
		prev, ok := current[t]
		if !ok {
			continue
		}
		conds = append(conds, corev1apply.NodeCondition().
			WithType(prev.Type).
			WithStatus(prev.Status).
			WithLastTransitionTime(prev.LastTransitionTime).
			WithReason(prev.Reason).
			WithMessage(prev.Message))
	}

	apply := corev1apply.Node(w.nodeName).
		WithStatus(corev1apply.NodeStatus().WithConditions(conds...))
	// Force: this manager is the sole authority for its own conditions; a
	// conflict (e.g. an operator kubectl-edited one) must not wedge the daemon.
	if _, err := w.client.CoreV1().Nodes().ApplyStatus(ctx, apply, metav1.ApplyOptions{FieldManager: fieldManager, Force: true}); err != nil {
		return err
	}
	w.lastDesired = desiredByType
	w.lastPending = pending.Clone()
	return nil
}

// ForceNextSync clears the no-op memo so the next Sync reapplies the full
// owned set even if the input is unchanged. The daemon calls it on the slow
// resync tick: reapplying repairs external tampering with owned conditions
// (e.g. a kubectl edit) and bounds how stale a wedged apply can get.
func (w *nodeConditionWriter) ForceNextSync() {
	w.lastDesired = nil
	w.lastPending = nil
}

func sameDesired(a, b map[string]DesiredCondition) bool {
	if len(a) != len(b) {
		return false
	}
	for t, d := range a {
		if b[t] != d {
			return false
		}
	}
	return true
}

// FakeConditionWriter records the published conditions for tests/demos. Last
// models the Node's owned condition set: Sync replaces it with the apply set
// (desired + pending types carried over unchanged), mirroring the real writer's
// SSA prune-by-omission semantics.
type FakeConditionWriter struct {
	mu          sync.Mutex
	Last        map[string]DesiredCondition
	LastPending sets.Set[string]
	Calls       int
	ForceCalls  int   // number of ForceNextSync invocations
	FailWith    error // if set, Sync returns this (simulates control-plane unreachable)
}

// ForceNextSync records the call. The fake has no no-op memo (every Sync
// applies), so recording is all that is needed to test the resync path.
func (f *FakeConditionWriter) ForceNextSync() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ForceCalls++
}

// CallCount returns Calls under the lock (safe while Run drives the writer
// from another goroutine).
func (f *FakeConditionWriter) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Calls
}

// ForceCallCount returns ForceCalls under the lock.
func (f *FakeConditionWriter) ForceCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ForceCalls
}

// PendingHas reports whether the last Sync passed condType as pending (safe
// for concurrent use).
func (f *FakeConditionWriter) PendingHas(condType string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.LastPending.Has(condType)
}

// NewFakeConditionWriter returns an empty fake writer.
func NewFakeConditionWriter() *FakeConditionWriter {
	return &FakeConditionWriter{Last: map[string]DesiredCondition{}}
}

// Sync replaces the recorded set with desired plus any pending types that were
// already published (carried over unchanged); everything else is pruned.
func (f *FakeConditionWriter) Sync(_ context.Context, desired []DesiredCondition, pending sets.Set[string]) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if f.FailWith != nil {
		return f.FailWith
	}
	f.LastPending = pending.Clone()
	next := make(map[string]DesiredCondition, len(desired)+len(pending))
	for _, d := range desired {
		next[d.Type] = d
	}
	for t := range pending {
		if _, dup := next[t]; dup {
			continue
		}
		if prev, ok := f.Last[t]; ok {
			next[t] = prev
		}
	}
	f.Last = next
	return nil
}

// Get returns the last recorded desired condition of a type (test helper).
func (f *FakeConditionWriter) Get(condType string) (DesiredCondition, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.Last[condType]
	return d, ok
}

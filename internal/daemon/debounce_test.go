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
	"time"

	corev1 "k8s.io/api/core/v1"
	clocktesting "k8s.io/utils/clock/testing"
)

func TestDebounceStartsUnknown(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	d := NewDebouncer(30*time.Second, 10*time.Second, clk)
	if d.State() != corev1.ConditionUnknown {
		t.Fatalf("initial state = %v, want Unknown (fail-closed at startup)", d.State())
	}
}

func TestDebounceHealthyRequiresSustainedDuration(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	d := NewDebouncer(30*time.Second, 10*time.Second, clk)

	// First healthy observation starts the timer but does not flip yet.
	if got := d.Observe(true); got != corev1.ConditionUnknown {
		t.Fatalf("after first healthy obs = %v, want Unknown (pending)", got)
	}

	// Not enough time elapsed.
	clk.Step(29 * time.Second)
	if got := d.Observe(true); got != corev1.ConditionUnknown {
		t.Fatalf("at 29s = %v, want Unknown (threshold is 30s)", got)
	}

	// Cross the healthy threshold.
	clk.Step(2 * time.Second) // total 31s
	if got := d.Observe(true); got != corev1.ConditionTrue {
		t.Fatalf("at 31s = %v, want True", got)
	}
}

func TestDebounceUnhealthyIsFasterThanHealthy(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	d := NewDebouncer(30*time.Second, 10*time.Second, clk)

	// Drive to True first.
	d.Observe(true)
	clk.Step(31 * time.Second)
	if d.Observe(true) != corev1.ConditionTrue {
		t.Fatal("setup: expected True")
	}

	// Now go unhealthy: should flip after only unhealthyThreshold (10s), not 30s.
	if got := d.Observe(false); got != corev1.ConditionTrue {
		t.Fatalf("first unhealthy obs = %v, want still True (pending)", got)
	}
	clk.Step(9 * time.Second)
	if got := d.Observe(false); got != corev1.ConditionTrue {
		t.Fatalf("at 9s unhealthy = %v, want still True", got)
	}
	clk.Step(2 * time.Second) // total 11s >= 10s
	if got := d.Observe(false); got != corev1.ConditionFalse {
		t.Fatalf("at 11s unhealthy = %v, want False", got)
	}
}

func TestDebounceFlappingResetsPending(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	d := NewDebouncer(30*time.Second, 10*time.Second, clk)

	// Start trending healthy...
	d.Observe(true)
	clk.Step(20 * time.Second)
	// ...then flip to unhealthy before the 30s healthy threshold: pending resets.
	d.Observe(false)
	clk.Step(20 * time.Second) // 20s of unhealthy, but timer reset at the flip
	// Total healthy-pending never completed, and unhealthy only ran 20s from reset
	// (>= 10s unhealthyThreshold) so it should now be False — never reached True.
	if got := d.Observe(false); got != corev1.ConditionFalse {
		t.Fatalf("after flap = %v, want False (never reached True)", got)
	}
	if got := d.State(); got == corev1.ConditionTrue {
		t.Fatalf("must never have asserted True during flapping")
	}
}

func TestDebounceStableStateClearsPending(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	d := NewDebouncer(30*time.Second, 10*time.Second, clk)
	d.Observe(true)
	clk.Step(31 * time.Second)
	d.Observe(true) // -> True

	// A brief unhealthy blip that recovers before the threshold must NOT flip.
	d.Observe(false)
	clk.Step(5 * time.Second)
	if got := d.Observe(true); got != corev1.ConditionTrue {
		t.Fatalf("recovered blip = %v, want True (pending cleared)", got)
	}
	// Confirm the pending was cleared: an immediate unhealthy starts a fresh timer.
	d.Observe(false)
	clk.Step(9 * time.Second)
	if got := d.Observe(false); got != corev1.ConditionTrue {
		t.Fatalf("fresh unhealthy timer at 9s = %v, want still True", got)
	}
}

// SetThresholds must preserve committed state and an in-flight pending timer:
// the accumulated sustain time is measured against the NEW threshold.
func TestDebounceRetunePreservesPending(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	d := NewDebouncer(30*time.Second, 10*time.Second, clk)

	// Start a pending healthy transition under the old 30s threshold.
	if got := d.Observe(true); got != corev1.ConditionUnknown {
		t.Fatalf("first obs = %v, want Unknown (pending)", got)
	}

	// Operator shortens healthyThreshold to 5s; 6s of already-accumulated
	// sustain must now commit the transition.
	d.SetThresholds(5*time.Second, 10*time.Second)
	clk.Step(6 * time.Second)
	if got := d.Observe(true); got != corev1.ConditionTrue {
		t.Fatalf("after retune to 5s at 6s elapsed = %v, want True", got)
	}

	// Lengthening works the other way: a new pending False transition under a
	// longer unhealthyThreshold does not commit early.
	d.SetThresholds(5*time.Second, 20*time.Second)
	d.Observe(false) // arm pending
	clk.Step(11 * time.Second)
	if got := d.Observe(false); got != corev1.ConditionTrue {
		t.Fatalf("at 11s of 20s unhealthy = %v, want still True", got)
	}
	clk.Step(10 * time.Second)
	if got := d.Observe(false); got != corev1.ConditionFalse {
		t.Fatalf("at 21s of 20s unhealthy = %v, want False", got)
	}
}

func TestDebouncePendingDeadline(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	d := NewDebouncer(30*time.Second, 10*time.Second, clk)

	if _, ok := d.PendingDeadline(); ok {
		t.Fatalf("fresh debouncer reports a pending deadline")
	}

	start := clk.Now()
	d.Observe(true) // arm healthy-pending
	dl, ok := d.PendingDeadline()
	if !ok {
		t.Fatalf("no pending deadline after arming")
	}
	if want := start.Add(30 * time.Second); !dl.Equal(want) {
		t.Errorf("deadline = %v, want %v (pendingSince+healthyThreshold)", dl, want)
	}

	// Committing clears the deadline.
	clk.Step(31 * time.Second)
	d.Observe(true)
	if _, ok := d.PendingDeadline(); ok {
		t.Errorf("deadline still reported after transition committed")
	}
}

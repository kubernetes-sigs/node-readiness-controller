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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/clock"
)

// Debouncer applies hysteresis to a per-condition raw healthy/unhealthy signal.
// It starts in Unknown and only flips to True/False after the raw signal has been
// sustained for the corresponding threshold. Asymmetric by design: react quickly
// to failure (unhealthyThreshold), return slowly to healthy (healthyThreshold).
//
// Starting in Unknown means a fresh daemon (or a resync) does not assert a value
// until it has observed a stable signal — a critical startup/restart safety property.
type Debouncer struct {
	healthyThreshold   time.Duration
	unhealthyThreshold time.Duration
	clk                clock.Clock

	state        corev1.ConditionStatus // last emitted: Unknown initially
	pendingRaw   bool                   // the raw value we are waiting to confirm
	pendingSince time.Time
	havePending  bool
}

// NewDebouncer creates a debouncer in the Unknown state.
func NewDebouncer(healthy, unhealthy time.Duration, clk clock.Clock) *Debouncer {
	return &Debouncer{
		healthyThreshold:   healthy,
		unhealthyThreshold: unhealthy,
		clk:                clk,
		state:              corev1.ConditionUnknown,
	}
}

// Observe feeds a raw verdict and returns the debounced condition status. It must
// be called whenever the daemon reconciles; the threshold is measured in wall
// time via the injected clock, not by call count.
func (d *Debouncer) Observe(rawHealthy bool) corev1.ConditionStatus {
	desired := corev1.ConditionFalse
	if rawHealthy {
		desired = corev1.ConditionTrue
	}

	now := d.clk.Now()

	// Already at the desired state: clear any pending transition.
	if d.state == desired {
		d.havePending = false
		return d.state
	}

	// Raw differs from current state. Start (or restart) the pending timer if the
	// raw value just changed; otherwise check whether it has been sustained long
	// enough to flip.
	if !d.havePending || d.pendingRaw != rawHealthy {
		d.havePending = true
		d.pendingRaw = rawHealthy
		d.pendingSince = now
		return d.state
	}

	threshold := d.unhealthyThreshold
	if rawHealthy {
		threshold = d.healthyThreshold
	}
	if now.Sub(d.pendingSince) >= threshold {
		d.state = desired
		d.havePending = false
	}
	return d.state
}

// SetThresholds re-tunes the debouncer after a config reload. The committed
// state and any in-flight pending transition (pendingSince) are preserved: the
// already-accumulated sustain time still counts, and the next Observe measures
// it against the NEW threshold. So shortening a threshold can resolve an
// in-flight transition sooner, and lengthening one extends it — the semantics
// an operator editing a live config would expect.
func (d *Debouncer) SetThresholds(healthy, unhealthy time.Duration) {
	d.healthyThreshold = healthy
	d.unhealthyThreshold = unhealthy
}

// PendingDeadline returns the instant at which the current pending transition
// will commit (pendingSince + the relevant threshold), or ok=false when no
// transition is pending. The daemon uses it to arm a wake timer so sub-tick
// thresholds resolve on time instead of at the next periodic tick.
func (d *Debouncer) PendingDeadline() (time.Time, bool) {
	if !d.havePending {
		return time.Time{}, false
	}
	threshold := d.unhealthyThreshold
	if d.pendingRaw {
		threshold = d.healthyThreshold
	}
	return d.pendingSince.Add(threshold), true
}

// State returns the current debounced status without advancing the machine.
func (d *Debouncer) State() corev1.ConditionStatus {
	return d.state
}

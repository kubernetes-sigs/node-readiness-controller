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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

const (
	// DefaultReconcileInterval is the periodic reconcile safety-net tick. Single
	// source of truth: main.go uses it as the flag default and New falls back to
	// it when Options.ReconcileInterval is zero.
	DefaultReconcileInterval = 10 * time.Second
	// DefaultResyncInterval is the slow force-reapply period: on each resync the
	// writer reapplies the owned conditions even if the input is unchanged,
	// repairing external tampering and bounding staleness.
	DefaultResyncInterval = 5 * time.Minute
	// coalesceDelay is the pod-event coalescing window: pod events only mark the
	// daemon dirty, and one reconcile runs per window, so an event burst (pod
	// churn, initial replays) collapses into a single evaluate+write. Short
	// enough to be operationally invisible, long enough to absorb kubelet bursts.
	coalesceDelay = 250 * time.Millisecond
)

// Options configures a Daemon.
type Options struct {
	Source PodSource
	Store  *ConfigStore
	Writer ConditionWriter
	// Clock must support tickers (clock.WithTicker); RealClock and the test
	// FakeClock both satisfy it. WithTicker embeds clock.Clock.
	Clock             clock.WithTicker
	ReconcileInterval time.Duration
	// ResyncInterval is the slow force-reapply period (0 => DefaultResyncInterval).
	ResyncInterval time.Duration
}

// Daemon wires the pod source, cache, config store, evaluators, debouncers, and
// the condition writer. Pod events mark it dirty and are coalesced into one
// reconcile per short window; a periodic tick reconciles unconditionally as a
// safety net; a one-shot wake timer resolves pending debounce transitions at
// their exact deadline; and a slow resync force-reapplies the owned conditions.
//
// Concurrency: Reconcile is not safe for concurrent use; Run is the sole caller.
// The debouncers map, cached configs, and Debouncer state are only ever touched
// from the Run goroutine. A mutex guards Reconcile anyway as cheap insurance
// against a future second caller.
type Daemon struct {
	source            PodSource
	cache             *Cache
	store             *ConfigStore
	writer            ConditionWriter
	clk               clock.WithTicker
	reconcileInterval time.Duration
	resyncInterval    time.Duration

	reconcileMu sync.Mutex
	debouncers  map[string]*Debouncer // by conditionType, persists across reloads

	// configs is the last successfully loaded config set. Config reloads are off
	// the hot path: only the periodic tick (and the first reconcile) hits disk;
	// event-driven reconciles reuse this cache.
	configs       []Config
	configsLoaded bool
}

// syncForcer is the optional writer capability the daemon uses on resync: it
// clears the writer's no-op memo so the next Sync reapplies even when the
// input is unchanged. nodeConditionWriter implements it.
type syncForcer interface {
	ForceNextSync()
}

// New builds a Daemon from Options.
func New(opts Options) *Daemon {
	if opts.Clock == nil {
		opts.Clock = clock.RealClock{}
	}
	if opts.ReconcileInterval == 0 {
		opts.ReconcileInterval = DefaultReconcileInterval
	}
	if opts.ResyncInterval == 0 {
		opts.ResyncInterval = DefaultResyncInterval
	}
	return &Daemon{
		source:            opts.Source,
		cache:             NewCache(),
		store:             opts.Store,
		writer:            opts.Writer,
		clk:               opts.Clock,
		reconcileInterval: opts.ReconcileInterval,
		resyncInterval:    opts.ResyncInterval,
		debouncers:        map[string]*Debouncer{},
	}
}

// newReconnectBackoff is the capped exponential backoff between source
// reconnect attempts (both Watch errors and stream ends). wait.Backoff is a
// pure duration calculator — no timers of its own — so it composes with the
// injected clock. Jitter is zero for determinism; reconnects target the
// node-local kubelet socket, so there is no thundering-herd concern.
func newReconnectBackoff() wait.Backoff {
	return wait.Backoff{Duration: 500 * time.Millisecond, Factor: 2, Steps: 10, Cap: 30 * time.Second}
}

// Run streams pod events and reconciles until ctx is cancelled. It reconnects
// the source on stream end (resetting the cache so it waits for a fresh sync),
// backing off exponentially until a stream reaches initial sync again.
func (d *Daemon) Run(ctx context.Context) error {
	ticker := d.clk.NewTicker(d.reconcileInterval)
	defer ticker.Stop()
	resync := d.clk.NewTicker(d.resyncInterval)
	defer resync.Stop()

	// One-shot timers, re-created on demand. A nil channel blocks forever in
	// select, which is exactly "not armed".
	var coalesceT, wakeT clock.Timer // coalesce doubles as the dirty flag
	var coalesceC, wakeC <-chan time.Time
	stopTimer := func(t clock.Timer) {
		if t != nil {
			t.Stop()
		}
	}
	defer func() { stopTimer(coalesceT); stopTimer(wakeT) }()

	// reconcileNow runs one reconcile, clears any armed coalescing timer (its
	// pending work is covered by this reconcile), and re-arms the wake timer at
	// the earliest pending debounce deadline so sub-tick thresholds resolve at
	// ~threshold instead of being quantized to the next tick. A deadline already
	// in the past means the transition is due now: reconcile again immediately
	// (committing clears the pending state, so the loop terminates).
	reconcileNow := func(reloadConfigs bool) {
		stopTimer(coalesceT)
		coalesceT, coalesceC = nil, nil
		stopTimer(wakeT)
		wakeT, wakeC = nil, nil
		for {
			d.reconcile(ctx, reloadConfigs)
			reloadConfigs = false
			if !d.cache.Synced() {
				return // reconcile was a no-op; don't spin on stale deadlines
			}
			deadline, ok := d.nextPendingDeadline()
			if !ok {
				return
			}
			if delay := deadline.Sub(d.clk.Now()); delay > 0 {
				wakeT = d.clk.NewTimer(delay)
				wakeC = wakeT.C()
				return
			}
		}
	}

	backoff := newReconnectBackoff()

	for ctx.Err() == nil {
		ch, err := d.source.Watch(ctx)
		if err != nil {
			delay := backoff.Step()
			klog.ErrorS(err, "pod source Watch failed; will retry", "backoff", delay)
			if !d.sleep(ctx, delay) {
				return ctx.Err()
			}
			continue
		}

	stream:
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C():
				// Periodic safety net: reload configs and reconcile unconditionally.
				reconcileNow(true)
			case <-resync.C():
				// Slow resync: force the writer to reapply even if unchanged, to
				// repair external tampering with owned conditions and bound staleness.
				if f, ok := d.writer.(syncForcer); ok {
					f.ForceNextSync()
				}
				reconcileNow(true)
			case <-coalesceC:
				reconcileNow(false)
			case <-wakeC:
				reconcileNow(false)
			case ev, ok := <-ch:
				if !ok {
					// Stream ended: reset cache, back off, reconnect (wait for
					// fresh sync). Without the backoff a missing kubelet socket
					// is a hot loop, because grpc.NewClient dials lazily and the
					// channel closes immediately.
					delay := backoff.Step()
					klog.InfoS("pod stream ended; resetting cache and reconnecting", "backoff", delay)
					d.cache.Reset()
					if !d.sleep(ctx, delay) {
						return ctx.Err()
					}
					break stream
				}
				d.cache.Apply(ev)
				if ev.Type == EventInitialSyncComplete {
					// Stream reached steady state: reset the reconnect backoff and
					// reconcile promptly (no coalescing delay on the sync edge).
					backoff = newReconnectBackoff()
					klog.InfoS("initial sync complete; reconciling")
					reconcileNow(false)
					continue
				}
				// Coalesce pod-event bursts: mark dirty by arming the window
				// timer; N events within the window collapse into one reconcile.
				if coalesceC == nil {
					coalesceT = d.clk.NewTimer(coalesceDelay)
					coalesceC = coalesceT.C()
				}
			}
		}
	}
	return ctx.Err()
}

// Reconcile reloads configs, evaluates each against the pod cache, debounces,
// and writes the coalesced condition set. It is a no-op (no writes) until the
// cache is synced, so a cold or resyncing cache never asserts a stale value.
//
// Not safe for concurrent use; Run is the sole caller (guarded by a mutex as
// insurance, see the Daemon doc).
func (d *Daemon) Reconcile(ctx context.Context) {
	d.reconcile(ctx, true)
}

// reconcile is the single reconcile step. reloadConfigs controls whether the
// config directory is rescanned (periodic tick / explicit Reconcile) or the
// cached set is reused (event- and wake-driven reconciles); the first reconcile
// always loads.
func (d *Daemon) reconcile(ctx context.Context, reloadConfigs bool) {
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()

	if !d.cache.Synced() {
		klog.V(4).InfoS("cache not synced; skipping reconcile")
		return
	}

	if reloadConfigs || !d.configsLoaded {
		configs, errs := d.store.Reload()
		for _, e := range errs {
			klog.ErrorS(e, "config load issue (skipped)")
		}
		d.configs = configs
		d.configsLoaded = true
	}

	pods := d.cache.Snapshot()
	active := sets.New[string]()
	pending := sets.New[string]()
	var desired []DesiredCondition

	for _, cfg := range d.configs {
		active.Insert(cfg.ConditionType)

		v := Evaluate(cfg, pods)
		deb := d.debouncerFor(cfg)
		status := deb.Observe(v.Healthy)

		klog.V(3).InfoS("evaluated condition",
			"type", cfg.ConditionType, "healthy", v.Healthy,
			"ready", v.ReadyPods, "total", v.TotalPods, "debounced", status)

		// Don't publish Unknown: while a condition has not yet resolved to a stable
		// value, leaving it absent reads as Unknown to NRC (= not satisfied = taint
		// stays), which is the fail-closed behavior we want during startup. It is
		// passed as pending so the writer keeps any already-published value (a
		// daemon restart mid-debounce must not transiently drop a condition).
		if status == corev1.ConditionUnknown {
			pending.Insert(cfg.ConditionType)
			continue
		}
		desired = append(desired, DesiredCondition{
			Type:    cfg.ConditionType,
			Status:  status,
			Reason:  v.Reason,
			Message: v.Message,
		})
	}

	// Drop debouncers for configs that no longer exist.
	for t := range d.debouncers {
		if !active.Has(t) {
			delete(d.debouncers, t)
		}
	}

	// Pruning is by SSA ownership: anything this daemon previously applied that
	// is in neither desired nor pending (i.e. its config is gone) is dropped by
	// the server when omitted from the apply set — no cross-reconcile diffing,
	// and stale conditions from configs removed while the daemon was down are
	// pruned on the first sync after startup.
	if err := d.writer.Sync(ctx, desired, pending); err != nil {
		klog.ErrorS(err, "failed to publish node conditions; will retry on next reconcile")
	}
}

// debouncerFor returns the debouncer for cfg's conditionType, creating it on
// first sight and re-tuning its thresholds when a config reload changed them.
func (d *Daemon) debouncerFor(cfg Config) *Debouncer {
	deb, ok := d.debouncers[cfg.ConditionType]
	if !ok {
		deb = NewDebouncer(cfg.HealthyThreshold, cfg.UnhealthyThreshold, d.clk)
		d.debouncers[cfg.ConditionType] = deb
		return deb
	}
	deb.SetThresholds(cfg.HealthyThreshold, cfg.UnhealthyThreshold)
	return deb
}

// nextPendingDeadline returns the earliest instant at which some pending
// debounce transition will commit, or ok=false when nothing is pending.
func (d *Daemon) nextPendingDeadline() (time.Time, bool) {
	var earliest time.Time
	found := false
	for _, deb := range d.debouncers {
		if dl, ok := deb.PendingDeadline(); ok && (!found || dl.Before(earliest)) {
			earliest, found = dl, true
		}
	}
	return earliest, found
}

// sleep waits for dur on the injected clock or until ctx is done; returns false
// if ctx ended.
func (d *Daemon) sleep(ctx context.Context, dur time.Duration) bool {
	t := d.clk.NewTimer(dur)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C():
		return true
	}
}

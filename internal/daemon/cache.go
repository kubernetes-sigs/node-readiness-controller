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
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Cache holds the node-local pod set keyed by UID, fed by a PodSource. It tracks
// whether the initial sync has completed; the daemon gates all condition writes
// on Synced() so a cold or resyncing cache never asserts a stale value.
type Cache struct {
	mu     sync.RWMutex
	pods   map[types.UID]*corev1.Pod
	synced bool
}

// NewCache returns an empty, un-synced cache.
func NewCache() *Cache {
	return &Cache{pods: map[types.UID]*corev1.Pod{}}
}

// Apply mutates the cache for one event. EventInitialSyncComplete flips Synced.
func (c *Cache) Apply(ev PodEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch ev.Type {
	case EventInitialSyncComplete:
		c.synced = true
	case EventAdded, EventModified:
		if ev.Pod != nil {
			c.pods[ev.Pod.UID] = trimPod(ev.Pod)
		}
	case EventDeleted:
		if ev.Pod != nil {
			delete(c.pods, ev.Pod.UID)
		}
	}
}

// Reset clears the cache and marks it un-synced (used on stream reconnect, so the
// daemon waits for a fresh INITIAL_SYNC_COMPLETE before re-asserting).
func (c *Cache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pods = map[types.UID]*corev1.Pod{}
	c.synced = false
}

// trimPod projects a pod down to the fields evaluation uses (identity, labels,
// deletion mark, phase, PodReady condition, and container states for failure
// messages). The cache holds every pod on the node for the process lifetime, and
// full Pod objects (env, volumes, probes, ...) are tens of KB each — on a dense
// node that is real memory for fields nothing reads. Trimming every pod, rather
// than dropping non-matching ones, keeps a runtime config change correct: the
// stream is push-only, so a pod discarded outright could not be recovered when a
// new config starts matching it.
func trimPod(p *corev1.Pod) *corev1.Pod {
	t := &corev1.Pod{}
	t.Namespace = p.Namespace
	t.Name = p.Name
	t.UID = p.UID
	t.Labels = p.Labels
	t.DeletionTimestamp = p.DeletionTimestamp
	t.Status.Phase = p.Status.Phase
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			t.Status.Conditions = []corev1.PodCondition{{Type: c.Type, Status: c.Status}}
			break
		}
	}
	for _, cs := range p.Status.ContainerStatuses {
		t.Status.ContainerStatuses = append(t.Status.ContainerStatuses, corev1.ContainerStatus{
			Name:  cs.Name,
			Ready: cs.Ready,
			State: cs.State,
		})
	}
	return t
}

// Synced reports whether the initial sync has completed.
func (c *Cache) Synced() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.synced
}

// Snapshot returns a shallow copy of the current pod set for evaluation.
func (c *Cache) Snapshot() []*corev1.Pod {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*corev1.Pod, 0, len(c.pods))
	for _, p := range c.pods {
		out = append(out, p)
	}
	return out
}

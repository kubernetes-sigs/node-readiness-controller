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
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Verdict is the raw (pre-debounce) evaluation of one config against the cache.
type Verdict struct {
	Healthy   bool
	ReadyPods int
	TotalPods int
	Reason    string
	Message   string
}

// podReady implements the readiness definition: a pod counts
// as ready iff it is Running, its Ready condition is True, and it is not
// terminating. The PodReady condition already aggregates per-container readiness,
// so containerStatuses are used only to enrich the failure reason (see crashReason).
func podReady(p *corev1.Pod) bool {
	if p.DeletionTimestamp != nil {
		return false
	}
	if p.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// crashReason returns a short human-readable reason a non-ready pod is unhealthy,
// for condition messages only (not a health gate).
func crashReason(p *corev1.Pod) string {
	if p.DeletionTimestamp != nil {
		return "Terminating"
	}
	if p.Status.Phase != corev1.PodRunning {
		return fmt.Sprintf("Phase=%s", p.Status.Phase)
	}
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Ready {
			continue
		}
		if w := cs.State.Waiting; w != nil {
			return fmt.Sprintf("%s=%s", cs.Name, w.Reason) // e.g. foo=CrashLoopBackOff
		}
		if t := cs.State.Terminated; t != nil {
			return fmt.Sprintf("%s=Terminated(%s)", cs.Name, t.Reason)
		}
	}
	return "NotReady"
}

func matchesConfig(cfg Config, p *corev1.Pod) bool {
	if cfg.Namespace != "" && p.Namespace != cfg.Namespace {
		return false
	}
	return cfg.Selector.Matches(labels.Set(p.Labels))
}

// Evaluate computes the raw verdict for one config against a pod snapshot.
//
// Healthy iff readyPods >= MinPods. Note MinPods >= 1 (enforced at config load),
// so an empty match (no critical pod scheduled yet) is unhealthy — fail-closed.
func Evaluate(cfg Config, pods []*corev1.Pod) Verdict {
	ready, total := 0, 0
	var problems []string

	for _, p := range pods {
		if !matchesConfig(cfg, p) {
			continue
		}
		total++
		if podReady(p) {
			ready++
		} else if len(problems) < 5 { // cap message length
			problems = append(problems, fmt.Sprintf("%s/%s: %s", p.Namespace, p.Name, crashReason(p)))
		}
	}

	v := Verdict{ReadyPods: ready, TotalPods: total, Healthy: ready >= cfg.MinPods}

	switch {
	case v.Healthy:
		v.Reason = "AllCriticalPodsReady"
		v.Message = fmt.Sprintf("%d/%d ready pods meet minPods=%d", ready, total, cfg.MinPods)
	case total == 0:
		v.Reason = "NoMatchingPods"
		v.Message = fmt.Sprintf("no pods match selector (minPods=%d)", cfg.MinPods)
	default:
		v.Reason = "CriticalPodsNotReady"
		v.Message = fmt.Sprintf("%d/%d ready, need %d", ready, total, cfg.MinPods)
		if len(problems) > 0 {
			v.Message += "; " + strings.Join(problems, ", ")
		}
	}
	return v
}

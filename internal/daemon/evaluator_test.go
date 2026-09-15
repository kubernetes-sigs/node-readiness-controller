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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

func pod(ns, name string, lbls map[string]string, phase corev1.PodPhase, ready bool) *corev1.Pod {
	st := corev1.ConditionFalse
	if ready {
		st = corev1.ConditionTrue
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: lbls, UID: types.UID(ns + "/" + name)},
		Status: corev1.PodStatus{
			Phase:      phase,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: st}},
		},
	}
}

func testConfig(minPods int, ns string) Config {
	return Config{
		ConditionType: "readiness.k8s.io/X",
		Namespace:     ns,
		Selector:      labels.SelectorFromSet(labels.Set{"k8s-app": "cilium"}),
		MinPods:       minPods,
	}
}

func TestPodReady(t *testing.T) {
	now := metav1.Now()
	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{"running and ready", pod("ns", "p", nil, corev1.PodRunning, true), true},
		{"running not ready", pod("ns", "p", nil, corev1.PodRunning, false), false},
		{"pending", pod("ns", "p", nil, corev1.PodPending, true), false},
		{"succeeded", pod("ns", "p", nil, corev1.PodSucceeded, true), false},
		{"no ready condition", &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := podReady(tt.pod); got != tt.want {
				t.Errorf("podReady = %v, want %v", got, tt.want)
			}
		})
	}

	// Terminating pod (running+ready but has deletionTimestamp) is not ready.
	term := pod("ns", "p", nil, corev1.PodRunning, true)
	term.DeletionTimestamp = &now
	if podReady(term) {
		t.Errorf("terminating pod should not be ready")
	}
}

func TestEvaluateFailClosed(t *testing.T) {
	cfg := testConfig(1, "")

	// Synced + zero matching pods => unhealthy (the not-yet-scheduled case).
	v := Evaluate(cfg, nil)
	if v.Healthy {
		t.Errorf("empty match must be unhealthy (fail-closed), got healthy")
	}
	if v.Reason != "NoMatchingPods" {
		t.Errorf("reason = %q, want NoMatchingPods", v.Reason)
	}

	// A matching, ready pod that doesn't meet minPods is unhealthy.
	cfg2 := testConfig(2, "")
	pods := []*corev1.Pod{pod("ns", "a", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true)}
	v = Evaluate(cfg2, pods)
	if v.Healthy {
		t.Errorf("1 ready < minPods=2 must be unhealthy")
	}
	if v.ReadyPods != 1 || v.TotalPods != 1 {
		t.Errorf("ready/total = %d/%d, want 1/1", v.ReadyPods, v.TotalPods)
	}
}

func TestEvaluateHealthy(t *testing.T) {
	cfg := testConfig(2, "")
	pods := []*corev1.Pod{
		pod("ns", "a", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true),
		pod("ns", "b", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true),
		pod("ns", "c", map[string]string{"k8s-app": "other"}, corev1.PodRunning, true), // non-matching
	}
	v := Evaluate(cfg, pods)
	if !v.Healthy {
		t.Errorf("2 ready matching >= minPods=2 must be healthy; msg=%s", v.Message)
	}
	if v.ReadyPods != 2 || v.TotalPods != 2 {
		t.Errorf("ready/total = %d/%d, want 2/2 (non-matching excluded)", v.ReadyPods, v.TotalPods)
	}
}

func TestEvaluateNamespaceFilter(t *testing.T) {
	cfg := testConfig(1, "kube-system")
	pods := []*corev1.Pod{
		pod("default", "a", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, true), // wrong ns
	}
	v := Evaluate(cfg, pods)
	if v.Healthy || v.TotalPods != 0 {
		t.Errorf("namespace filter failed: total=%d healthy=%v", v.TotalPods, v.Healthy)
	}
}

func TestCrashReason(t *testing.T) {
	p := pod("ns", "p", map[string]string{"k8s-app": "cilium"}, corev1.PodRunning, false)
	p.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name:  "agent",
		Ready: false,
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
	}}
	cfg := testConfig(1, "")
	v := Evaluate(cfg, []*corev1.Pod{p})
	if v.Healthy {
		t.Fatalf("crashlooping pod must be unhealthy")
	}
	if want := "agent=CrashLoopBackOff"; !strings.Contains(v.Message, want) {
		t.Errorf("message %q should mention %q", v.Message, want)
	}
}

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

package metrics

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// stubLister is a test double for RuleNodeStateLister.
type stubLister struct {
	nodes    []corev1.Node
	nodesErr error

	counts map[string]RuleNodeCounts
	err    error

	blocked    map[string]RuleBlockedConditions
	blockedErr error

	mu                    sync.Mutex
	gotNodesForRuleStates []corev1.Node
	gotNodesForBlocked    []corev1.Node
}

func (s *stubLister) ListNodes(_ context.Context) ([]corev1.Node, error) {
	if s.nodesErr != nil {
		return nil, s.nodesErr
	}
	return s.nodes, nil
}

func (s *stubLister) ListRuleNodeStates(_ context.Context, nodes []corev1.Node) (map[string]RuleNodeCounts, error) {
	s.mu.Lock()
	s.gotNodesForRuleStates = nodes
	s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return s.counts, nil
}

func (s *stubLister) ListBlockedNodes(_ context.Context, nodes []corev1.Node) (map[string]RuleBlockedConditions, error) {
	s.mu.Lock()
	s.gotNodesForBlocked = nodes
	s.mu.Unlock()
	if s.blockedErr != nil {
		return nil, s.blockedErr
	}
	return s.blocked, nil
}

func TestReadinessCollector_NoRules(t *testing.T) {
	c := NewReadinessCollector(&stubLister{counts: map[string]RuleNodeCounts{}})

	expected := ``
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "node_readiness_rule_nodes"); err != nil {
		t.Fatalf("unexpected collect mismatch: %v", err)
	}
}

func TestReadinessCollector_ZeroMatches(t *testing.T) {
	c := NewReadinessCollector(&stubLister{counts: map[string]RuleNodeCounts{
		"gpu-ready": {Held: 0, Released: 0},
	}})

	expected := `
		# HELP node_readiness_rule_nodes Number of nodes currently gated or released by the rule.
		# TYPE node_readiness_rule_nodes gauge
		node_readiness_rule_nodes{rule="gpu-ready",state="held"} 0
		node_readiness_rule_nodes{rule="gpu-ready",state="released"} 0
	`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "node_readiness_rule_nodes"); err != nil {
		t.Fatalf("unexpected collect mismatch: %v", err)
	}
}

func TestReadinessCollector_MixedHeldReleased(t *testing.T) {
	c := NewReadinessCollector(&stubLister{counts: map[string]RuleNodeCounts{
		"gpu-ready": {Held: 3, Released: 7},
		"cni-ready": {Held: 0, Released: 10},
	}})

	expected := `
		# HELP node_readiness_rule_nodes Number of nodes currently gated or released by the rule.
		# TYPE node_readiness_rule_nodes gauge
		node_readiness_rule_nodes{rule="gpu-ready",state="held"} 3
		node_readiness_rule_nodes{rule="gpu-ready",state="released"} 7
		node_readiness_rule_nodes{rule="cni-ready",state="held"} 0
		node_readiness_rule_nodes{rule="cni-ready",state="released"} 10
	`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "node_readiness_rule_nodes"); err != nil {
		t.Fatalf("unexpected collect mismatch: %v", err)
	}
}

func TestReadinessCollector_PassesThroughLister(t *testing.T) {
	c := NewReadinessCollector(&stubLister{counts: map[string]RuleNodeCounts{
		"active-rule": {Held: 1, Released: 1},
	}})

	expected := `
		# HELP node_readiness_rule_nodes Number of nodes currently gated or released by the rule.
		# TYPE node_readiness_rule_nodes gauge
		node_readiness_rule_nodes{rule="active-rule",state="held"} 1
		node_readiness_rule_nodes{rule="active-rule",state="released"} 1
	`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "node_readiness_rule_nodes"); err != nil {
		t.Fatalf("unexpected collect mismatch: %v", err)
	}
}

func TestReadinessCollector_ListError(t *testing.T) {
	c := NewReadinessCollector(&stubLister{err: errors.New("cache not synced")})

	expected := ``
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "node_readiness_rule_nodes"); err != nil {
		t.Fatalf("unexpected collect mismatch: %v", err)
	}
}

func TestReadinessCollector_BlockedNodes_NoRules(t *testing.T) {
	c := NewReadinessCollector(&stubLister{blocked: map[string]RuleBlockedConditions{}})

	expected := ``
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "node_readiness_blocked_nodes"); err != nil {
		t.Fatalf("unexpected collect mismatch: %v", err)
	}
}

func TestReadinessCollector_BlockedNodes_MixedAndZero(t *testing.T) {
	c := NewReadinessCollector(&stubLister{blocked: map[string]RuleBlockedConditions{
		"gpu-ready": {"GPUDriverReady": 2, "CNIReady": 0},
	}})

	expected := `
		# HELP node_readiness_blocked_nodes Number of nodes blocked by each required condition.
		# TYPE node_readiness_blocked_nodes gauge
		node_readiness_blocked_nodes{condition="GPUDriverReady",rule="gpu-ready"} 2
		node_readiness_blocked_nodes{condition="CNIReady",rule="gpu-ready"} 0
	`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "node_readiness_blocked_nodes"); err != nil {
		t.Fatalf("unexpected collect mismatch: %v", err)
	}
}

func TestReadinessCollector_BlockedNodes_ListError(t *testing.T) {
	c := NewReadinessCollector(&stubLister{
		counts:     map[string]RuleNodeCounts{"gpu-ready": {Held: 1, Released: 0}},
		blockedErr: errors.New("cache not synced"),
	})

	expected := ``
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "node_readiness_blocked_nodes"); err != nil {
		t.Fatalf("unexpected collect mismatch: %v", err)
	}
}

func collectAll(t *testing.T, c *ReadinessCollector) map[string][]*dto.Metric {
	t.Helper()

	ch := make(chan prometheus.Metric, 16)
	c.Collect(ch)
	close(ch)

	out := make(map[string][]*dto.Metric)
	for m := range ch {
		name := "node_readiness_rule_nodes"
		if m.Desc() == blockedNodesDesc {
			name = "node_readiness_blocked_nodes"
		}

		pb := &dto.Metric{}
		if err := m.Write(pb); err != nil {
			t.Fatalf("unexpected error writing metric %s: %v", name, err)
		}
		out[name] = append(out[name], pb)
	}
	return out
}

func TestReadinessCollector_RuleNodesErrorDoesNotBlockBlockedNodes(t *testing.T) {
	c := NewReadinessCollector(&stubLister{
		err:     errors.New("cache not synced"),
		blocked: map[string]RuleBlockedConditions{"gpu-ready": {"GPUDriverReady": 2}},
	})

	got := collectAll(t, c)

	if len(got["node_readiness_rule_nodes"]) != 0 {
		t.Fatalf("expected no node_readiness_rule_nodes metrics when ListRuleNodeStates fails, got %v", got["node_readiness_rule_nodes"])
	}

	blockedMetrics := got["node_readiness_blocked_nodes"]
	if len(blockedMetrics) != 1 {
		t.Fatalf("expected node_readiness_blocked_nodes to still be emitted despite ListRuleNodeStates failing, got %v", blockedMetrics)
	}
	if got, want := blockedMetrics[0].GetGauge().GetValue(), 2.0; got != want {
		t.Fatalf("blocked_nodes value = %v, want %v", got, want)
	}
}

func TestReadinessCollector_BlockedNodesErrorDoesNotBlockRuleNodes(t *testing.T) {
	c := NewReadinessCollector(&stubLister{
		counts:     map[string]RuleNodeCounts{"gpu-ready": {Held: 3, Released: 1}},
		blockedErr: errors.New("cache not synced"),
	})

	got := collectAll(t, c)

	ruleNodesMetrics := got["node_readiness_rule_nodes"]
	if len(ruleNodesMetrics) != 2 {
		t.Fatalf("expected node_readiness_rule_nodes (held+released) to still be emitted despite ListBlockedNodes failing, got %v", ruleNodesMetrics)
	}

	if len(got["node_readiness_blocked_nodes"]) != 0 {
		t.Fatalf("expected no node_readiness_blocked_nodes metrics when ListBlockedNodes fails, got %v", got["node_readiness_blocked_nodes"])
	}
}

func TestReadinessCollector_NodeListErrorSkipsBothMetrics(t *testing.T) {
	stub := &stubLister{
		nodesErr: errors.New("node cache not synced"),
		counts:   map[string]RuleNodeCounts{"gpu-ready": {Held: 1, Released: 1}},
		blocked:  map[string]RuleBlockedConditions{"gpu-ready": {"GPUDriverReady": 1}},
	}
	c := NewReadinessCollector(stub)

	got := collectAll(t, c)

	if len(got["node_readiness_rule_nodes"]) != 0 {
		t.Fatalf("expected no node_readiness_rule_nodes metrics when ListNodes fails, got %v", got["node_readiness_rule_nodes"])
	}
	if len(got["node_readiness_blocked_nodes"]) != 0 {
		t.Fatalf("expected no node_readiness_blocked_nodes metrics when ListNodes fails, got %v", got["node_readiness_blocked_nodes"])
	}

	if stub.gotNodesForRuleStates != nil || stub.gotNodesForBlocked != nil {
		t.Fatalf("expected Collect to short-circuit before calling either counting method, but ListRuleNodeStates got %v, ListBlockedNodes got %v",
			stub.gotNodesForRuleStates, stub.gotNodesForBlocked)
	}
}

func TestReadinessCollector_NodesSharedBetweenBothListers(t *testing.T) {
	nodes := []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}}}
	stub := &stubLister{
		nodes:   nodes,
		counts:  map[string]RuleNodeCounts{},
		blocked: map[string]RuleBlockedConditions{},
	}
	c := NewReadinessCollector(stub)

	ch := make(chan prometheus.Metric, 4)
	c.Collect(ch)
	close(ch)
	for range ch {
	}

	if len(stub.gotNodesForRuleStates) != 1 || stub.gotNodesForRuleStates[0].Name != "node-a" {
		t.Fatalf("ListRuleNodeStates did not receive the shared node snapshot: %v", stub.gotNodesForRuleStates)
	}
	if len(stub.gotNodesForBlocked) != 1 || stub.gotNodesForBlocked[0].Name != "node-a" {
		t.Fatalf("ListBlockedNodes did not receive the shared node snapshot: %v", stub.gotNodesForBlocked)
	}
}

func TestReadinessCollector_CollectAndLint(t *testing.T) {
	c := NewReadinessCollector(&stubLister{
		nodes: []corev1.Node{{}},
		counts: map[string]RuleNodeCounts{
			"gpu-ready": {Held: 3, Released: 7},
		},
		blocked: map[string]RuleBlockedConditions{
			"gpu-ready": {"GPUDriverReady": 2, "CNIReady": 0},
		},
	})

	problems, err := testutil.CollectAndLint(c)
	if err != nil {
		t.Fatalf("CollectAndLint error: %v", err)
	}
	for _, p := range problems {
		t.Errorf("lint problem: metric=%s text=%s", p.Metric, p.Text)
	}
}

func TestReadinessCollector_ConcurrentCollect(t *testing.T) {
	c := NewReadinessCollector(&stubLister{
		counts:  map[string]RuleNodeCounts{"gpu-ready": {Held: 3, Released: 7}},
		blocked: map[string]RuleBlockedConditions{"gpu-ready": {"GPUDriverReady": 3}},
	})

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Exercise concurrent Collect calls for race detection.
			ch := make(chan prometheus.Metric, 4)
			done := make(chan struct{})
			go func() {
				for range ch {
				}
				close(done)
			}()
			c.Collect(ch)
			close(ch)
			<-done
		}()
	}
	wg.Wait()
}

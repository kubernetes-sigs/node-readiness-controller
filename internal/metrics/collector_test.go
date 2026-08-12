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
)

// stubLister is a test double for RuleNodeStateLister.
type stubLister struct {
	counts map[string]RuleNodeCounts
	err    error
}

func (s *stubLister) ListRuleNodeStates(_ context.Context) (map[string]RuleNodeCounts, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.counts, nil
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

func TestReadinessCollector_ConcurrentCollect(t *testing.T) {
	c := NewReadinessCollector(&stubLister{counts: map[string]RuleNodeCounts{
		"gpu-ready": {Held: 3, Released: 7},
	}})

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Exercise concurrent Collect calls for race detection.
			ch := make(chan prometheus.Metric, 2)
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

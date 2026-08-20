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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// collectTimeout limits how long a scrape can wait for cached data.
const collectTimeout = 5 * time.Second

// NodeLister lists Nodes for the collector.
type NodeLister interface {
	ListNodes(ctx context.Context) ([]corev1.Node, error)
}

// RuleNodeCounts holds the number of held and released nodes for a rule.
type RuleNodeCounts struct {
	Held     float64
	Released float64
}

// RuleNodeStateLister lists held and released nodes for each rule.
type RuleNodeStateLister interface {
	ListRuleNodeStates(ctx context.Context, nodes []corev1.Node) (map[string]RuleNodeCounts, error)
}

// RuleBlockedConditions holds blocked node counts by condition.
type RuleBlockedConditions map[string]float64

// BlockedNodesLister lists blocked node counts for each rule and condition.
type BlockedNodesLister interface {
	ListBlockedNodes(ctx context.Context, nodes []corev1.Node) (map[string]RuleBlockedConditions, error)
}

// ReadinessLister aggregates the scrape-time lookups the collector needs.
type ReadinessLister interface {
	NodeLister
	RuleNodeStateLister
	BlockedNodesLister
}

var ruleNodesDesc = prometheus.NewDesc(
	"node_readiness_rule_nodes",
	"Number of nodes currently gated or released by the rule.",
	[]string{"rule", "state"},
	nil,
)

var blockedNodesDesc = prometheus.NewDesc(
	"node_readiness_blocked_nodes",
	"Number of nodes blocked by each required condition.",
	[]string{"rule", "condition"},
	nil,
)

// ReadinessCollector is a prometheus.Collector that reads at scrape time.
type ReadinessCollector struct {
	lister ReadinessLister
}

func NewReadinessCollector(lister ReadinessLister) *ReadinessCollector {
	return &ReadinessCollector{lister: lister}
}

// Describe implements prometheus.Collector.
func (c *ReadinessCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- ruleNodesDesc
	ch <- blockedNodesDesc
}

// Collect implements prometheus.Collector.
func (c *ReadinessCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()

	nodes, err := c.lister.ListNodes(ctx)
	if err != nil {
		ctrl.Log.V(2).Info("Failed to list nodes", "error", err)
		return
	}

	counts, err := c.lister.ListRuleNodeStates(ctx, nodes)
	if err != nil {
		ctrl.Log.V(2).Info("Failed to list rule node states", "error", err)
	} else {
		for rule, rc := range counts {
			ch <- prometheus.MustNewConstMetric(ruleNodesDesc, prometheus.GaugeValue, rc.Held, rule, string(RuleNodeStateHeld))
			ch <- prometheus.MustNewConstMetric(ruleNodesDesc, prometheus.GaugeValue, rc.Released, rule, string(RuleNodeStateReleased))
		}
	}

	blocked, err := c.lister.ListBlockedNodes(ctx, nodes)
	if err != nil {
		ctrl.Log.V(2).Info("Failed to list blocked nodes", "error", err)
	} else {
		for rule, conditions := range blocked {
			for condition, count := range conditions {
				ch <- prometheus.MustNewConstMetric(blockedNodesDesc, prometheus.GaugeValue, count, rule, condition)
			}
		}
	}
}

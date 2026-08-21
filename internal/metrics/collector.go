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
	ctrl "sigs.k8s.io/controller-runtime"
)

// collectTimeout limits how long a scrape can wait for cached data.
const collectTimeout = 5 * time.Second

// RuleNodeCounts holds the number of held and released nodes for a rule.
type RuleNodeCounts struct {
	Held     float64
	Released float64
}

// RuleNodeStateLister lists held and released nodes for each rule.
type RuleNodeStateLister interface {
	ListRuleNodeStates(ctx context.Context) (map[string]RuleNodeCounts, error)
}

// RuleMatchedNodesLister lists the number of nodes matching each rule's NodeSelector.
type RuleMatchedNodesLister interface {
	ListRuleMatchedNodes(ctx context.Context) (map[string]float64, error)
}

// ReadinessLister aggregates the scrape-time lookups the collector needs.
type ReadinessLister interface {
	RuleNodeStateLister
	RuleMatchedNodesLister
}

var ruleNodesDesc = prometheus.NewDesc(
	"node_readiness_rule_nodes",
	"Number of nodes currently gated or released by the rule.",
	[]string{"rule", "state"},
	nil,
)

var ruleMatchedNodesDesc = prometheus.NewDesc(
	"node_readiness_rule_matched_nodes",
	"Number of nodes matched by a rule's NodeSelector.",
	[]string{"rule"},
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
	ch <- ruleMatchedNodesDesc
}

// Collect implements prometheus.Collector.
func (c *ReadinessCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()

	counts, err := c.lister.ListRuleNodeStates(ctx)
	if err != nil {
		ctrl.Log.V(2).Info("Failed to list rule node states", "error", err)
	} else {
		for rule, rc := range counts {
			ch <- prometheus.MustNewConstMetric(ruleNodesDesc, prometheus.GaugeValue, rc.Held, rule, string(RuleNodeStateHeld))
			ch <- prometheus.MustNewConstMetric(ruleNodesDesc, prometheus.GaugeValue, rc.Released, rule, string(RuleNodeStateReleased))
		}
	}

	matched, err := c.lister.ListRuleMatchedNodes(ctx)
	if err != nil {
		ctrl.Log.V(2).Info("Failed to list rule matched nodes", "error", err)
		return
	}
	for rule, count := range matched {
		ch <- prometheus.MustNewConstMetric(ruleMatchedNodesDesc, prometheus.GaugeValue, count, rule)
	}
}

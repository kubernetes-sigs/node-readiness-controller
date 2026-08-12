//go:build scale

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

package scale

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
)

type phaseStats struct {
	phase string
	title string
	start time.Time
	end   time.Time
}

var (
	queryResults []queryResult
)

var _ = Describe("Node Readiness Controller Scalability Test", func() {

	It("should successfully run the scale test phases and evaluate performance", func() {
		queryResults = make([]queryResult, 0, 2)
		ctx := context.Background()
		nodeCount := cfg.NodeCount
		var phases []phaseStats

		// Tainting Phase

		By("Applying Security Agent NodeReadinessRule resource")
		taintStart := time.Now()
		ruleManifest := strings.Replace(securityAgentRuleManifest, `enforcementMode: "continuous"`, fmt.Sprintf(`enforcementMode: "%s"`, cfg.EnforcementMode), 1)
		applyManifest(ctx, ruleManifest)

		By("Applying KWOK's Stage to simulate condition false")
		applyManifest(ctx, securityAgentStageFalseManifest)

		By("Waiting for the controller manager to reconcile and add taints to all nodes")
		waitForNodeTaints(ctx, nodeCount, cfg.TaintTimeout)

		taintEnd := time.Now()
		taintDuration := taintEnd.Sub(taintStart)

		By("Deleting KWOK's Stage for condition false to avoid conflicting stages")
		deleteStage(ctx, "security-agent-stage-false")

		phases = append(phases, phaseStats{
			phase: "add",
			title: fmt.Sprintf("%d Nodes - Tainting (Add) Phase [Duration: %s]", nodeCount, taintDuration.Round(time.Millisecond)),
			start: taintStart,
			end:   taintEnd,
		})

		By("Sleeping 10 seconds to settle metrics before starting untainting")
		time.Sleep(10 * time.Second)

		// Untainting Phase

		By("Applying KWOK's Stage to simulate condition true (agent ready)")
		untaintStart := time.Now()
		applyManifest(ctx, securityAgentStageTrueManifest)

		By("Waiting for the controller manager to reconcile and remove taints on all nodes")
		waitForNodeTaints(ctx, 0, cfg.UntaintTimeout)

		untaintEnd := time.Now()
		untaintDuration := untaintEnd.Sub(untaintStart)

		phases = append(phases, phaseStats{
			phase: "remove",
			title: fmt.Sprintf("%d Nodes - Untainting Phase [Duration: %s]", nodeCount, untaintDuration.Round(time.Millisecond)),
			start: untaintStart,
			end:   untaintEnd,
		})

		By("Sleeping 10 seconds to settle metrics before gathering final report")
		time.Sleep(10 * time.Second)

		collectAndRecordPhaseMetrics(ctx, phases)
	})
})

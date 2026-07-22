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
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/node-readiness-controller/test/utils"
)

type phaseStats struct {
	title string
	start time.Time
	end   time.Time
}

var (
	queryResults  []queryResult
	nodeCountUsed int
)

var _ = Describe("Node Readiness Controller Scalability Test", func() {

	It("should successfully run the scale test phases and evaluate performance", func() {
		queryResults = make([]queryResult, 0, 2)
		ctx := context.Background()
		nodeCount := nodeCountUsed

		var phases []phaseStats

		// Tainting Phase

		By("Applying Security Agent NodeReadinessRule resource")
		taintStart := time.Now()

		setupRuleCmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
		setupRuleCmd.Stdin = strings.NewReader(securityAgentRuleManifest)
		setupRuleCmdOutput, err := utils.Run(setupRuleCmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply Security Agent NodeReadinessRule manifest:\n%s", setupRuleCmdOutput)

		By("Applying KWOK's Stage to simulate condition false")
		applyFalseCmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
		applyFalseCmd.Stdin = strings.NewReader(securityAgentStageFalseManifest)
		falseOutput, err := utils.Run(applyFalseCmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply false stage: %s", falseOutput)

		By("Waiting for the controller manager to reconcile and add taints to all nodes")
		Eventually(func(g Gomega) int {
			count, err := countTaintedNodes(ctx, "type=kwok", "readiness.k8s.io/SecurityAgentNotReady", "pending")
			g.Expect(err).NotTo(HaveOccurred())
			By(fmt.Sprintf("Progress: %d/%d nodes successfully tainted", count, nodeCount))
			return count
		}, "15m", "1s").Should(Equal(nodeCount), "Tainted nodes count did not reach target replicas")

		taintEnd := time.Now()
		taintDuration := taintEnd.Sub(taintStart)

		By("Deleting KWOK's Stage for condition false to avoid conflicting stages")
		deleteFalseCmd := exec.CommandContext(ctx, "kubectl", "delete", "stage", "security-agent-stage-false", "--ignore-not-found") // #nosec G204
		deleteCmdOutput, err := utils.Run(deleteFalseCmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to delete the false condition Stage:\n%s", deleteCmdOutput)

		phases = append(phases, phaseStats{
			title: fmt.Sprintf("%d Nodes - Tainting (Add) Phase [Duration: %s]", nodeCount, taintDuration.Round(time.Millisecond)),
			start: taintStart,
			end:   taintEnd,
		})

		By("Sleeping 10 seconds to settle metrics before starting untainting")
		time.Sleep(10 * time.Second)

		// Untainting Phase

		By("Applying KWOK's Stage to simulate condition true (agent ready)")
		untaintStart := time.Now()

		applyTrueCmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
		applyTrueCmd.Stdin = strings.NewReader(securityAgentStageTrueManifest)
		trueOutput, err := utils.Run(applyTrueCmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply true stage: %s", trueOutput)

		By("Waiting for the controller manager to reconcile and remove taints on all nodes")
		Eventually(func(g Gomega) int {
			tainted, err := countTaintedNodes(ctx, "type=kwok", "readiness.k8s.io/SecurityAgentNotReady", "pending")
			g.Expect(err).NotTo(HaveOccurred())
			By(fmt.Sprintf("Progress: %d/%d nodes remaining tainted", tainted, nodeCount))
			return tainted
		}, "15m", "1s").Should(Equal(0), "Failed to complete untainting phase")

		untaintEnd := time.Now()
		untaintDuration := untaintEnd.Sub(untaintStart)

		phases = append(phases, phaseStats{
			title: fmt.Sprintf("%d Nodes - Untainting Phase [Duration: %s]", nodeCount, untaintDuration.Round(time.Millisecond)),
			start: untaintStart,
			end:   untaintEnd,
		})

		By("Sleeping 10 seconds to settle metrics before gathering final report")
		time.Sleep(10 * time.Second)

		for _, phase := range phases {
			metricsMap := collectMetricsForPhase(ctx, phase.start, phase.end)
			reportStruct := buildReportForPhase(phase.title, phase.start, phase.end, metricsMap)
			queryResults = append(queryResults, reportStruct)
		}
	})
})

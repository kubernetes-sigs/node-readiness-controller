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

package controller

import (
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	readinessv1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

func TestClearNodeFailure(t *testing.T) {
	g := NewWithT(t)

	c := &RuleReadinessController{}

	rule := &readinessv1alpha1.NodeReadinessRule{
		Status: readinessv1alpha1.NodeReadinessRuleStatus{
			FailedNodes: []readinessv1alpha1.NodeFailure{
				{NodeName: "node-1", Reason: "EvaluationError", LastEvaluationTime: metav1.Now()},
				{NodeName: "node-2", Reason: "EvaluationError", LastEvaluationTime: metav1.Now()},
			},
		},
	}

	c.clearNodeFailure(rule, "node-1")

	g.Expect(rule.Status.FailedNodes).To(HaveLen(1))
	g.Expect(rule.Status.FailedNodes[0].NodeName).To(Equal("node-2"))
}

func TestRecordThenClearNodeFailure(t *testing.T) {
	g := NewWithT(t)

	c := &RuleReadinessController{}

	rule := &readinessv1alpha1.NodeReadinessRule{}

	// A node fails evaluation and is recorded.
	c.recordNodeFailure(rule, "node-1", "EvaluationError", "boom")
	g.Expect(rule.Status.FailedNodes).To(HaveLen(1))

	// The node later recovers; the failure record must be cleared.
	c.clearNodeFailure(rule, "node-1")
	g.Expect(rule.Status.FailedNodes).To(BeEmpty())
}

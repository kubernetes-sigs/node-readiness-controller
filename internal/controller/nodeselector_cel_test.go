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
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	nodereadinessiov1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
)

// The empty nodeSelector constraint is enforced by CEL on the CRD rather than by
// the validating webhook, so it applies on every cluster instead of only where the
// optional webhook is deployed. These specs assert it through the API server.
var _ = Describe("NodeReadinessRule nodeSelector CEL validation", func() {
	var celCtx context.Context

	newRule := func(name string, selector metav1.LabelSelector) *nodereadinessiov1alpha1.NodeReadinessRule {
		return &nodereadinessiov1alpha1.NodeReadinessRule{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: nodereadinessiov1alpha1.NodeReadinessRuleSpec{
				Conditions: []nodereadinessiov1alpha1.ConditionRequirement{
					{Type: "CELReady", RequiredStatus: corev1.ConditionTrue},
				},
				NodeSelector:    selector,
				Taint:           corev1.Taint{Key: "readiness.k8s.io/cel-selector", Effect: corev1.TaintEffectNoSchedule},
				EnforcementMode: nodereadinessiov1alpha1.EnforcementModeContinuous,
			},
		}
	}

	BeforeEach(func() { celCtx = context.Background() })

	AfterEach(func() {
		list := &nodereadinessiov1alpha1.NodeReadinessRuleList{}
		if err := k8sClient.List(celCtx, list); err == nil {
			for i := range list.Items {
				r := &list.Items[i]
				if strings.HasPrefix(r.Name, "cel-selector-") {
					r.Finalizers = nil
					_ = k8sClient.Update(celCtx, r)
					_ = k8sClient.Delete(celCtx, r)
				}
			}
		}
	})

	// A wholly absent selector is caught by the required marker rather than by the
	// CEL rule, because the field is omitempty/omitzero and serialises away
	// entirely. Both reject the object, they just report it differently.
	It("rejects a rule with no selector at all", func() {
		err := k8sClient.Create(celCtx, newRule("cel-selector-absent", metav1.LabelSelector{}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.nodeSelector: Required value"))
	})

	It("rejects a selector whose matchLabels map is present but empty", func() {
		err := k8sClient.Create(celCtx, newRule("cel-selector-emptylabels",
			metav1.LabelSelector{MatchLabels: map[string]string{}}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("nodeSelector must not be empty"))
	})

	It("rejects a selector whose matchExpressions list is present but empty", func() {
		err := k8sClient.Create(celCtx, newRule("cel-selector-emptyexprs",
			metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{}}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("nodeSelector must not be empty"))
	})

	It("accepts a selector with matchLabels", func() {
		rule := newRule("cel-selector-labels", metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
		})
		Expect(k8sClient.Create(celCtx, rule)).To(Succeed())

		persisted := &nodereadinessiov1alpha1.NodeReadinessRule{}
		Expect(k8sClient.Get(celCtx, types.NamespacedName{Name: rule.Name}, persisted)).To(Succeed())
		Expect(persisted.Spec.NodeSelector.MatchLabels).To(HaveKey("node-role.kubernetes.io/worker"))
	})

	It("accepts a selector with only matchExpressions", func() {
		rule := newRule("cel-selector-exprs", metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "node-role.kubernetes.io/control-plane", Operator: metav1.LabelSelectorOpDoesNotExist},
			},
		})
		Expect(k8sClient.Create(celCtx, rule)).To(Succeed())
	})
})

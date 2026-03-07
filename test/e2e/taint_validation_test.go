//go:build e2e
// +build e2e

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

package e2e

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/node-readiness-controller/test/utils"
)

var _ = Describe("NodeReadinessRule Taint Key Validation", Ordered, func() {
	BeforeAll(func() {
		By("installing CRDs for validation tests")
		cmd := exec.Command("make", "install")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")
	})

	AfterAll(func() {
		By("uninstalling CRDs after validation tests")
		cmd := exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)
	})

	Context("When creating a NodeReadinessRule", func() {
		AfterEach(func() {
			// Clean up any test resources
			_ = exec.Command("kubectl", "delete", "nodereadinessrule", "--all", "--ignore-not-found=true").Run()
		})

		It("should reject taint keys with multiple slashes", func() {
			manifest := `
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: test-multiple-slashes
spec:
  conditions:
    - type: "test.condition"
      requiredStatus: "True"
  taint:
    key: "readiness.k8s.io/invalid/multiple-slashes"
    effect: "NoSchedule"
  enforcementMode: "continuous"
  nodeSelector:
    matchLabels:
      kubernetes.io/os: linux
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			output, err := cmd.CombinedOutput()

			Expect(err).To(HaveOccurred(), "Should fail to create NodeReadinessRule with multiple slashes")
			Expect(string(output)).To(ContainSubstring("exactly one '/' separator"))
		})

		It("should reject taint keys with name starting with dash", func() {
			manifest := `
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: test-dash-start
spec:
  conditions:
    - type: "test.condition"
      requiredStatus: "True"
  taint:
    key: "readiness.k8s.io/-invalid-start"
    effect: "NoSchedule"
  enforcementMode: "continuous"
  nodeSelector:
    matchLabels:
      kubernetes.io/os: linux
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			output, err := cmd.CombinedOutput()

			Expect(err).To(HaveOccurred(), "Should fail with name starting with dash")
			Expect(string(output)).To(ContainSubstring("must start and end with an alphanumeric character"))
		})

		It("should reject taint keys with name ending with dash", func() {
			manifest := `
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: test-dash-end
spec:
  conditions:
    - type: "test.condition"
      requiredStatus: "True"
  taint:
    key: "readiness.k8s.io/invalid-end-"
    effect: "NoSchedule"
  enforcementMode: "continuous"
  nodeSelector:
    matchLabels:
      kubernetes.io/os: linux
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			output, err := cmd.CombinedOutput()

			Expect(err).To(HaveOccurred(), "Should fail with name ending with dash")
			Expect(string(output)).To(ContainSubstring("must start and end with an alphanumeric character"))
		})

		It("should reject taint keys with name longer than 63 characters", func() {
			longName := strings.Repeat("a", 64)
			manifest := fmt.Sprintf(`
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: test-long-name
spec:
  conditions:
    - type: "test.condition"
      requiredStatus: "True"
  taint:
    key: "readiness.k8s.io/%s"
    effect: "NoSchedule"
  enforcementMode: "continuous"
  nodeSelector:
    matchLabels:
      kubernetes.io/os: linux
`, longName)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			output, err := cmd.CombinedOutput()

			Expect(err).To(HaveOccurred(), "Should fail with name longer than 63 characters")
			Expect(string(output)).To(ContainSubstring("1-63 characters"))
		})

		It("should accept valid taint keys", func() {
			validKeys := []string{
				"readiness.k8s.io/simple",
				"readiness.k8s.io/with-dashes",
				"readiness.k8s.io/with_underscores",
				"readiness.k8s.io/with.dots",
				"readiness.k8s.io/Mixed-Case_123.OK",
				"readiness.k8s.io/security-agent-ready",
			}

			for i, key := range validKeys {
				manifest := fmt.Sprintf(`
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: test-valid-%d
spec:
  conditions:
    - type: "test.condition"
      requiredStatus: "True"
  taint:
    key: "%s"
    effect: "NoSchedule"
    value: "pending"
  enforcementMode: "continuous"
  nodeSelector:
    matchLabels:
      kubernetes.io/os: linux
`, i, key)
				cmd := exec.Command("kubectl", "apply", "-f", "-")
				cmd.Stdin = strings.NewReader(manifest)
				output, err := utils.Run(cmd)

				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should accept valid taint key: %s. Output: %s", key, output))
			}

			// Clean up
			cmd := exec.Command("kubectl", "delete", "nodereadinessrule", "--all")
			_, _ = utils.Run(cmd)
		})

		It("should reject empty name part", func() {
			manifest := `
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: test-empty-name
spec:
  conditions:
    - type: "test.condition"
      requiredStatus: "True"
  taint:
    key: "readiness.k8s.io/"
    effect: "NoSchedule"
  enforcementMode: "continuous"
  nodeSelector:
    matchLabels:
      kubernetes.io/os: linux
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			output, err := cmd.CombinedOutput()

			Expect(err).To(HaveOccurred(), "Should fail with empty name part")
			Expect(string(output)).To(ContainSubstring("1-63 characters"))
		})

		It("should reject taint keys with special characters in name", func() {
			manifest := `
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: test-special-chars
spec:
  conditions:
    - type: "test.condition"
      requiredStatus: "True"
  taint:
    key: "readiness.k8s.io/invalid@special"
    effect: "NoSchedule"
  enforcementMode: "continuous"
  nodeSelector:
    matchLabels:
      kubernetes.io/os: linux
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			output, err := cmd.CombinedOutput()

			Expect(err).To(HaveOccurred(), "Should fail with special characters")
			Expect(string(output)).To(ContainSubstring("must consist of alphanumeric characters"))
		})
	})
})

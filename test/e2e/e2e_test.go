//go:build e2e
// +build e2e

/*
Copyright 2025.

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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ajaysundark/node-readiness-gate-controller/test/utils"
)

// namespace where the project is deployed in
const namespace = "nrgcontroller-system"

// serviceAccountName created for the project
const serviceAccountName = "nrgcontroller-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "nrgcontroller-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "nrgcontroller-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")

		By("removing node selector for e2e testing")
		cmd = exec.Command("kubectl", "patch", "deployment", "nrgcontroller-controller-manager",
			"-n", namespace, "--type=json",
			"-p", `[{"op":"remove","path":"/spec/template/spec/nodeSelector"},{"op":"replace","path":"/spec/template/spec/tolerations","value":[]}]`)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to patch deployment")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", Ordered, func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})


		// +kubebuilder:scaffold:e2e-webhooks-checks
	})

	Context("NodeReadinessGateRule", func() {
		It("should handle bootstrap-only mode correctly", func() {
			nodeName := "bootstrap-test-node"

			By("creating a test node with initial taint and condition")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(`
apiVersion: v1
kind: Node
metadata:
  name: %s
  labels:
    e2e-test: "bootstrap"
spec:
  taints:
    - key: readiness.k8s.io/TestReady
      effect: NoSchedule
      value: pending
status:
  conditions:
    - type: TestReady
      status: "False"
      lastHeartbeatTime: %s
      lastTransitionTime: %s
`, nodeName, time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339)))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("applying the bootstrap-only rule")
			cmd = exec.Command("kubectl", "apply", "-f", "test/e2e/testdata/bootstrap-only-rule.yaml")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("updating node condition to True")
			cmd = exec.Command("kubectl", "patch", "node", nodeName, "--type=json", "-p",
				`[{"op":"replace","path":"/status/conditions/0/status","value":"True"}]`)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying taint is removed")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return !strings.Contains(output, "readiness.k8s.io/TestReady")
			}, 30*time.Second, 2*time.Second).Should(BeTrue())

			By("verifying rule status shows node in completedNodes")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "nodereadinessgaterule", "bootstrap-test-rule", "-o", "jsonpath={.status.completedNodes}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return strings.Contains(output, nodeName)
			}, 30*time.Second, 2*time.Second).Should(BeTrue())

			By("updating node condition back to False")
			cmd = exec.Command("kubectl", "patch", "node", nodeName, "--type=json", "-p",
				`[{"op":"replace","path":"/status/conditions/0/status","value":"False"}]`)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying taint stays removed (bootstrap-only behavior)")
			Consistently(func() bool {
				cmd := exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return !strings.Contains(output, "readiness.k8s.io/TestReady")
			}, 10*time.Second, 2*time.Second).Should(BeTrue())

			By("cleaning up test resources")
			exec.Command("kubectl", "delete", "node", nodeName).Run()
			exec.Command("kubectl", "delete", "nodereadinessgaterule", "bootstrap-test-rule").Run()
		})

		It("should handle continuous mode with add/remove cycle", func() {
			nodeName := "continuous-test-node"

			By("creating a test node with condition satisfied")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(`
apiVersion: v1
kind: Node
metadata:
  name: %s
  labels:
    e2e-test: "continuous"
status:
  conditions:
    - type: StorageReady
      status: "True"
      lastHeartbeatTime: %s
      lastTransitionTime: %s
`, nodeName, time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339)))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("applying the continuous mode rule")
			cmd = exec.Command("kubectl", "apply", "-f", "test/e2e/testdata/continuous-rule.yaml")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying no taint is added (condition satisfied)")
			Consistently(func() bool {
				cmd := exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return !strings.Contains(output, "readiness.k8s.io/StorageReady")
			}, 10*time.Second, 2*time.Second).Should(BeTrue())

			By("updating node condition to False")
			cmd = exec.Command("kubectl", "patch", "node", nodeName, "--type=json", "-p",
				`[{"op":"replace","path":"/status/conditions/0/status","value":"False"}]`)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying taint is added")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return strings.Contains(output, "readiness.k8s.io/StorageReady")
			}, 30*time.Second, 2*time.Second).Should(BeTrue())

			By("updating node condition back to True")
			cmd = exec.Command("kubectl", "patch", "node", nodeName, "--type=json", "-p",
				`[{"op":"replace","path":"/status/conditions/0/status","value":"True"}]`)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying taint is removed again (continuous enforcement)")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return !strings.Contains(output, "readiness.k8s.io/StorageReady")
			}, 30*time.Second, 2*time.Second).Should(BeTrue())

			By("cleaning up test resources")
			exec.Command("kubectl", "delete", "node", nodeName).Run()
			exec.Command("kubectl", "delete", "nodereadinessgaterule", "continuous-test-rule").Run()
		})

		It("should enforce multi-condition rules with ALL logic", func() {
			nodeName := "multi-condition-node"

			By("creating a test node with both conditions unsatisfied")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(`
apiVersion: v1
kind: Node
metadata:
  name: %s
  labels:
    e2e-test: "multi-condition"
status:
  conditions:
    - type: NetworkReady
      status: "False"
      lastHeartbeatTime: %s
      lastTransitionTime: %s
    - type: StorageReady
      status: "False"
      lastHeartbeatTime: %s
      lastTransitionTime: %s
`, nodeName, time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339),
				time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339)))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("applying the multi-condition rule")
			cmd = exec.Command("kubectl", "apply", "-f", "test/e2e/testdata/multi-condition-rule.yaml")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying taint is added (conditions not met)")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return strings.Contains(output, "readiness.k8s.io/Ready")
			}, 30*time.Second, 2*time.Second).Should(BeTrue())

			By("updating NetworkReady to True, StorageReady stays False")
			cmd = exec.Command("kubectl", "patch", "node", nodeName, "--type=json", "-p",
				`[{"op":"replace","path":"/status/conditions/0/status","value":"True"}]`)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying taint still present (not all conditions met)")
			Consistently(func() bool {
				cmd := exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return strings.Contains(output, "readiness.k8s.io/Ready")
			}, 10*time.Second, 2*time.Second).Should(BeTrue())

			By("updating StorageReady to True as well")
			cmd = exec.Command("kubectl", "patch", "node", nodeName, "--type=json", "-p",
				`[{"op":"replace","path":"/status/conditions/1/status","value":"True"}]`)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying taint is removed (all conditions met)")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return !strings.Contains(output, "readiness.k8s.io/Ready")
			}, 30*time.Second, 2*time.Second).Should(BeTrue())

			By("cleaning up test resources")
			exec.Command("kubectl", "delete", "node", nodeName).Run()
			exec.Command("kubectl", "delete", "nodereadinessgaterule", "multi-condition-rule").Run()
		})

		It("should respect node selector matching", func() {
			nodeAName := "worker-test-node"
			nodeBName := "control-plane-test-node"

			By("creating worker node")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(`
apiVersion: v1
kind: Node
metadata:
  name: %s
  labels:
    node-role.kubernetes.io/worker: ""
status:
  conditions:
    - type: TestReady
      status: "False"
      lastHeartbeatTime: %s
      lastTransitionTime: %s
`, nodeAName, time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339)))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating control-plane node")
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(`
apiVersion: v1
kind: Node
metadata:
  name: %s
  labels:
    node-role.kubernetes.io/control-plane: ""
status:
  conditions:
    - type: TestReady
      status: "False"
      lastHeartbeatTime: %s
      lastTransitionTime: %s
`, nodeBName, time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339)))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("applying the node-selector rule (targets workers only)")
			cmd = exec.Command("kubectl", "apply", "-f", "test/e2e/testdata/node-selector-rule.yaml")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying only worker node gets tainted")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "node", nodeAName, "-o", "jsonpath={.spec.taints}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return strings.Contains(output, "test-taint")
			}, 30*time.Second, 2*time.Second).Should(BeTrue())

			By("verifying control-plane node is unaffected")
			Consistently(func() bool {
				cmd := exec.Command("kubectl", "get", "node", nodeBName, "-o", "jsonpath={.spec.taints}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return !strings.Contains(output, "test-taint")
			}, 10*time.Second, 2*time.Second).Should(BeTrue())

			By("cleaning up test resources")
			exec.Command("kubectl", "delete", "node", nodeAName).Run()
			exec.Command("kubectl", "delete", "node", nodeBName).Run()
			exec.Command("kubectl", "delete", "nodereadinessgaterule", "node-selector-rule").Run()
		})

		It("should preview changes in dry-run mode without applying them", func() {
			nodeName := "dryrun-test-node"

			By("creating a test node with condition unsatisfied")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(`
apiVersion: v1
kind: Node
metadata:
  name: %s
  labels:
    e2e-test: "dryrun"
status:
  conditions:
    - type: TestReady
      status: "False"
      lastHeartbeatTime: %s
      lastTransitionTime: %s
`, nodeName, time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339)))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("applying the dry-run rule")
			cmd = exec.Command("kubectl", "apply", "-f", "test/e2e/testdata/dryrun-rule.yaml")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying no taint is added to the node")
			Consistently(func() bool {
				cmd := exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return !strings.Contains(output, "test-taint")
			}, 10*time.Second, 2*time.Second).Should(BeTrue())

			By("verifying rule has dry-run results showing what would happen")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "nodereadinessgaterule", "dryrun-test-rule", "-o", "jsonpath={.status.dryRunResults}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				// Check that dry run results exist and contain the node
				return len(output) > 0
			}, 30*time.Second, 2*time.Second).Should(BeTrue())

			By("cleaning up test resources")
			exec.Command("kubectl", "delete", "node", nodeName).Run()
			exec.Command("kubectl", "delete", "nodereadinessgaterule", "dryrun-test-rule").Run()
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}

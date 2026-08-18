//go:build e2e_npd
// +build e2e_npd

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

package e2e_npd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/node-readiness-controller/test/utils"
)

const namespace = "nrr-system"
const npdNamespace = "kube-system"

var _ = Describe("NPD Integration", Ordered, func() {
	var controllerPodName string
	var npdPodName string
	var nodeName string

	BeforeAll(func() {
		By("getting the kind worker node name")
		cmd := exec.Command("kubectl", "get", "nodes", "-l", "node-role.kubernetes.io/worker=", "-o", "jsonpath={.items[0].metadata.name}")
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to get worker node")
		nodeName = strings.TrimSpace(output)
		Expect(nodeName).NotTo(BeEmpty())

		By("creating manager namespace")
		cmd = exec.Command("kubectl", "create", "ns", namespace)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG_PREFIX=%s", imagePrefix), fmt.Sprintf("IMG_TAG=%s", imageTag))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")

		By("creating configmaps for NPD")
		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: npd-config
  namespace: kube-system
data:
  custom-plugin.sh: |
    #!/bin/sh
    if [ -f /var/log/npd-test/exit_code ]; then
      code=$(cat /var/log/npd-test/exit_code)
      if [ "$code" = "0" ]; then
        echo "OK"
        exit 0
      elif [ "$code" = "1" ]; then
        echo "ERROR"
        exit 1
      else
        echo "UNKNOWN"
        exit 2
      fi
    fi
    echo "OK"
    exit 0

  custom-plugin-monitor.json: |
    {
      "plugin": "custom",
      "pluginConfig": {
        "invoke_interval": "5s",
        "timeout": "5s",
        "max_output_length": 80,
        "concurrency": 1
      },
      "source": "custom-plugin-monitor",
      "metricsReporting": true,
      "conditions": [
        {
          "type": "CustomPluginReady",
          "reason": "CustomPluginFailed",
          "message": "Custom plugin returned failure."
        }
      ],
      "rules": [
        {
          "type": "temporary",
          "reason": "CustomPluginFailed",
          "pattern": "ERROR",
          "condition": "CustomPluginReady"
        }
      ]
    }

  system-log-monitor.json: |
    {
      "plugin": "systemLog",
      "pluginConfig": {
        "format": "regexp",
        "source": "filelog",
        "logPath": "/var/log/npd-test/simulated.log",
        "pattern": "^(?P<timestamp>\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}\\.\\d{6}Z)\\s+(?P<message>.*)$"
      },
      "logPath": "/var/log/npd-test/simulated.log",
      "source": "system-log-monitor",
      "conditions": [
        {
          "type": "SystemLogReady",
          "reason": "SystemLogFailure",
          "message": "System log indicates failure."
        }
      ],
      "rules": [
        {
          "type": "permanent",
          "reason": "SystemLogFailure",
          "pattern": "ERROR_SYNTHETIC_MATCH",
          "condition": "SystemLogReady"
        }
      ]
    }
`)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create NPD ConfigMap")

		By("deploying NPD DaemonSet")
		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(`
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-problem-detector
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: node-problem-detector
  template:
    metadata:
      labels:
        app: node-problem-detector
    spec:
      tolerations:
      - key: "startup-taint.node-readiness-controller.k8s.io/npd-ready"
        operator: "Exists"
        effect: "NoSchedule"
      containers:
      - name: node-problem-detector
        image: registry.k8s.io/node-problem-detector/node-problem-detector:v0.8.20
        command:
        - "/node-problem-detector"
        - "--config.custom-plugin-monitor=/config/custom-plugin-monitor.json"
        - "--config.system-log-monitor=/config/system-log-monitor.json"
        volumeMounts:
        - name: log
          mountPath: /var/log/npd-test
        - name: config
          mountPath: /config
          readOnly: true
        securityContext:
          privileged: true
      volumes:
      - name: log
        hostPath:
          path: /var/log/npd-test
          type: DirectoryOrCreate
      - name: config
        configMap:
          name: npd-config
          defaultMode: 0777
`)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy NPD")
	})

	AfterAll(func() {
		// Collect logs before tearing down
		if artifactsDir := os.Getenv("ARTIFACTS"); artifactsDir != "" {
			utils.Run(exec.Command("kubectl", "logs", "-n", namespace, "deployment/nrr-controller-manager", "--all-containers", ">", filepath.Join(artifactsDir, "nrr-controller-manager.log")))
			utils.Run(exec.Command("kubectl", "logs", "-n", npdNamespace, "daemonset/node-problem-detector", "--all-containers", ">", filepath.Join(artifactsDir, "node-problem-detector.log")))
			utils.Run(exec.Command("kubectl", "get", "events", "-A", "-o", "json", ">", filepath.Join(artifactsDir, "all-events.json")))
		}

		By("cleaning up NPD")
		exec.Command("kubectl", "delete", "daemonset", "node-problem-detector", "-n", "kube-system").Run()
		exec.Command("kubectl", "delete", "configmap", "npd-config", "-n", "kube-system").Run()

		By("uninstalling CRDs")
		exec.Command("make", "uninstall").Run()

		By("undeploying the controller-manager")
		exec.Command("make", "undeploy").Run()

		By("removing manager namespace")
		exec.Command("kubectl", "delete", "ns", namespace).Run()
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	Context("NPD integration with Continuous Rule", func() {
		It("should run controller and NPD successfully", func() {
			By("validating controller pod is running")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "control-plane=controller-manager", "-n", namespace, "-o", "jsonpath={.items[0].status.phase}")
				out, err := utils.Run(cmd)
				return err == nil && out == "Running"
			}).Should(BeTrue())

			By("validating NPD pod is running")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=node-problem-detector", "-n", npdNamespace, "-o", "jsonpath={.items[0].status.phase}")
				out, err := utils.Run(cmd)
				return err == nil && out == "Running"
			}).Should(BeTrue())
		})

		It("should evaluate and manage taints correctly through NPD node conditions", func() {
			By("applying NodeReadinessRule")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(`
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: npd-ready-rule
spec:
  conditions:
    - type: CustomPluginReady
      requiredStatus: "False"
    - type: SystemLogReady
      requiredStatus: "False"
  taint:
    key: "startup-taint.node-readiness-controller.k8s.io/npd-ready"
    effect: NoSchedule
  enforcementMode: "continuous"
`)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// 1. Initial State: Conditions are missing, taint remains.
			By("verifying taint remains because conditions are missing (fail-closed)")
			Consistently(func() bool {
				out, _ := utils.Run(exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}"))
				return strings.Contains(out, "startup-taint.node-readiness-controller.k8s.io/npd-ready")
			}, 10*time.Second, 2*time.Second).Should(BeTrue())

			// Helper to run command inside NPD pod or node to write to host path
			writeExitCode := func(code string) {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=node-problem-detector", "-n", npdNamespace, "-o", "jsonpath={.items[0].metadata.name}")
				out, _ := utils.Run(cmd)
				podName := strings.TrimSpace(out)
				// Write to the volume mounted at /var/log/npd-test
				utils.Run(exec.Command("kubectl", "exec", podName, "-n", npdNamespace, "--", "sh", "-c", fmt.Sprintf("echo '%s' > /var/log/npd-test/exit_code", code)))
			}

			writeLogLine := func(line string) {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=node-problem-detector", "-n", npdNamespace, "-o", "jsonpath={.items[0].metadata.name}")
				out, _ := utils.Run(cmd)
				podName := strings.TrimSpace(out)
				ts := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
				utils.Run(exec.Command("kubectl", "exec", podName, "-n", npdNamespace, "--", "sh", "-c", fmt.Sprintf("echo '%s %s' >> /var/log/npd-test/simulated.log", ts, line)))
			}

			// 2. Recovery: exit 0 -> condition False -> taint removed.
			By("writing exit code 0 to make CustomPluginReady False")
			writeExitCode("0")
			// Create simulated log to ensure SystemLogReady becomes False (NPD creates conditions on first monitor run, but maybe we need a healthy log?)
			// SystemLogMonitor might not generate condition until a log is seen or daemon starts. Let's write a generic log.
			writeLogLine("INFO everything is fine")

			By("verifying taint is removed when both conditions become False")
			Eventually(func() bool {
				out, _ := utils.Run(exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}"))
				return !strings.Contains(out, "startup-taint.node-readiness-controller.k8s.io/npd-ready")
			}).Should(BeTrue())

			// 3. Problem: exit 1 -> condition True -> taint added.
			By("writing exit code 1 to make CustomPluginReady True")
			writeExitCode("1")
			Eventually(func() bool {
				out, _ := utils.Run(exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}"))
				return strings.Contains(out, "startup-taint.node-readiness-controller.k8s.io/npd-ready")
			}).Should(BeTrue())

			// 4. Unknown: exit 2 -> condition Unknown -> taint retained.
			By("writing exit code 2 to make CustomPluginReady Unknown")
			writeExitCode("2")
			Consistently(func() bool {
				out, _ := utils.Run(exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}"))
				return strings.Contains(out, "startup-taint.node-readiness-controller.k8s.io/npd-ready")
			}, 10*time.Second, 2*time.Second).Should(BeTrue())

			// Recovery again before system log test
			By("writing exit code 0 to recover CustomPluginReady to False")
			writeExitCode("0")
			Eventually(func() bool {
				out, _ := utils.Run(exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}"))
				return !strings.Contains(out, "startup-taint.node-readiness-controller.k8s.io/npd-ready")
			}).Should(BeTrue())

			// 5. SystemLogMatch: synthetic log -> condition True -> taint added
			By("writing synthetic error log to make SystemLogReady True")
			writeLogLine("ERROR_SYNTHETIC_MATCH")
			Eventually(func() bool {
				out, _ := utils.Run(exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.spec.taints}"))
				return strings.Contains(out, "startup-taint.node-readiness-controller.k8s.io/npd-ready")
			}).Should(BeTrue())

			// 6. Assert Node Ready condition is preserved
			By("verifying the Node Ready condition is still True")
			cmd = exec.Command("kubectl", "get", "node", nodeName, "-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
			out, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("True"))
		})
	})
})

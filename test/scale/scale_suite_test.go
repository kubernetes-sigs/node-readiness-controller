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
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"text/template"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/node-readiness-controller/test/utils"
)

func TestScale(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node Readiness Controller Scale Performance Suite")
}

const (
	kwokctlVersion   = "v0.8.0"
	defaultNodeCount = 1000
)

var (
	kwokctlBinaryPath     string
	controllerBinPath     string
	controllerCmd         *exec.Cmd
	controllerLogFile     *os.File
	artifactsDir          string
	controllerMetricsPort = "8080"
	prometheusPort        = "9090"
)

//go:embed testdata/security-agent-rule.yaml
var securityAgentRuleManifest string

//go:embed testdata/security-agent-stage-false.yaml
var securityAgentStageFalseManifest string

//go:embed testdata/security-agent-stage-true.yaml
var securityAgentStageTrueManifest string

//go:embed testdata/prometheus-nrc-job.yaml
var prometheusJobTemplate string

var _ = BeforeSuite(func() {
	if port := os.Getenv("CONTROLLER_METRICS_PORT"); port != "" {
		controllerMetricsPort = port
	}
	if port := os.Getenv("PROMETHEUS_PORT"); port != "" {
		prometheusPort = port
	}

	By("Ensuring kwokctl binary is present")
	projectRootDir, err := utils.GetProjectDir()
	Expect(err).NotTo(HaveOccurred())
	toolsBinDir := filepath.Join(projectRootDir, "hack", "tools", "bin")
	kwokctlBinaryPath = ensureKwokctl(kwokctlVersion, toolsBinDir)

	By("Cleaning up any existing simulated cluster and stale controller processes")
	_ = exec.Command(kwokctlBinaryPath, "delete", "cluster").Run()
	_ = exec.Command("pkill", "-f", "node-readiness-controller").Run()

	By("Creating the simulated KWOK cluster")
	createArgs := []string{
		"create", "cluster",
		"--runtime", "binary",
		"--prometheus-port", prometheusPort,
		"--enable-crds", "Stage",
	}
	if os.Getenv("DISABLE_QPS_LIMITS") == "true" {
		createArgs = append(createArgs, "--disable-qps-limits")
	}
	if leaseSecs := os.Getenv("NODE_LEASE_DURATION_SECONDS"); leaseSecs != "" {
		createArgs = append(createArgs, "--node-lease-duration-seconds", leaseSecs)
	}

	createCmd := exec.Command(kwokctlBinaryPath, createArgs...) // #nosec G204
	createOuput, err := utils.Run(createCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to create kwok cluster:\n%s", createOuput)

	By("Applying NodeReadinessRule CRD manifests")
	crdConfigPath := filepath.Join(projectRootDir, "config", "crd")
	crdCmd := exec.Command("kubectl", "apply", "-k", crdConfigPath) // #nosec G204
	crdOutput, err := utils.Run(crdCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to apply NodeReadinessRule CRD via Kustomize:\n%s", crdOutput)

	By("Compiling node-readiness-controller manager binary")
	controllerBinName := "node-readiness-controller"
	controllerBinPath = filepath.Join(toolsBinDir, controllerBinName)
	controllerMainPath := filepath.Join(".", "cmd", "main.go")

	buildCmd := exec.Command("go", "build", "-o", controllerBinPath, controllerMainPath) // #nosec G204
	buildOutput, err := utils.Run(buildCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to compile controller manager:\n%s", buildOutput)

	By("Configuring controller scraper job in Prometheus config")
	homeDir, err := os.UserHomeDir()
	Expect(err).NotTo(HaveOccurred(), "Failed to retrieve user home directory")
	prometheusConfigPath := filepath.Join(homeDir, ".kwok", "clusters", "kwok", "prometheus.yaml")

	prometheusConfigBytes, err := os.ReadFile(prometheusConfigPath) // #nosec G304
	Expect(err).NotTo(HaveOccurred(), "Failed to read Prometheus configuration")

	newConfig := string(prometheusConfigBytes) + fmt.Sprintf(prometheusJobTemplate, controllerMetricsPort)
	err = os.WriteFile(prometheusConfigPath, []byte(newConfig), 0600)
	Expect(err).NotTo(HaveOccurred(), "Failed to update Prometheus configuration with new job")

	By("Restarting Prometheus to load updated configuration")
	err = exec.Command("pkill", "-SIGHUP", "prometheus").Run()
	Expect(err).NotTo(HaveOccurred(), "Failed to restart Prometheus instance to load updated configuration")

	By("Waiting for Prometheus endpoint to be ready")
	// Verify Prometheus readiness explicitly before proceeding
	Eventually(func(g Gomega) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/-/ready", prometheusPort))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	}, "30s", "1s").Should(Succeed(), "Prometheus is not ready")

	By("Scaling nodes up")
	nodeCount := defaultNodeCount
	if nodeCountStr := os.Getenv("NODE_COUNT"); nodeCountStr != "" {
		var err error
		nodeCount, err = strconv.Atoi(nodeCountStr)
		Expect(err).NotTo(HaveOccurred(), "Invalid NODE_COUNT: %s", nodeCountStr)
	}
	nodeCountUsed = nodeCount

	scaleCmd := exec.Command(kwokctlBinaryPath, "scale", "node", // #nosec G204
		"--replicas", strconv.Itoa(nodeCount),
		"--name", "kwok")
	scaleOutput, err := utils.Run(scaleCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to scale nodes: %s", scaleOutput)

	Eventually(func(g Gomega) int {
		count, err := countKwokNodes(context.Background(), "type=kwok")
		g.Expect(err).NotTo(HaveOccurred())
		return count
	}, "15m", "1s").Should(Equal(nodeCount), "Nodes failed to scale")

	By("Creating subdirectory for storing test artifacts")
	artifactsDir = os.Getenv("ARTIFACTS")
	if artifactsDir == "" {
		artifactsDir = filepath.Join(projectRootDir, "test", "scale", "artifacts")
	}

	err = os.MkdirAll(artifactsDir, 0750)
	Expect(err).NotTo(HaveOccurred(), "Failed to create the artifacts subdirectory")

	By("Creating file for storing the controller logs")
	var errError error
	controllerLogFile, errError = os.Create(filepath.Join(artifactsDir, "controller.log")) // #nosec G304
	Expect(errError).NotTo(HaveOccurred(), "Failed to create controller.log")

	By("Starting the node-readiness-controller manager daemon process")
	args := []string{
		fmt.Sprintf("--metrics-bind-address=:%s", controllerMetricsPort),
		"--metrics-secure=false",
		"--leader-elect=false",
		"--enable-webhook=false",
	}
	if qps := os.Getenv("KUBE_API_QPS"); qps != "" {
		args = append(args, "--kube-api-qps="+qps)
	}
	if burst := os.Getenv("KUBE_API_BURST"); burst != "" {
		args = append(args, "--kube-api-burst="+burst)
	}
	if nodeConc := os.Getenv("NODE_CONCURRENT_RECONCILES"); nodeConc != "" {
		args = append(args, "--node-concurrent-reconciles="+nodeConc)
	}
	if ruleConc := os.Getenv("RULE_CONCURRENT_RECONCILES"); ruleConc != "" {
		args = append(args, "--rule-concurrent-reconciles="+ruleConc)
	}

	controllerCmd = exec.Command(controllerBinPath, args...) // #nosec G204
	controllerCmd.Stdout = controllerLogFile
	controllerCmd.Stderr = controllerLogFile

	err = controllerCmd.Start()
	Expect(err).NotTo(HaveOccurred(), "Failed to start controller process")

	By("Waiting for the controller metrics endpoint to be responsive")
	Eventually(func(g Gomega) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/metrics", controllerMetricsPort))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	}, "15s", "500ms").Should(Succeed(), fmt.Sprintf("Controller failed to start or bind to port %s", controllerMetricsPort))
})

var _ = AfterSuite(func() {
	projectRootDir, err := utils.GetProjectDir()
	Expect(err).NotTo(HaveOccurred())

	By("Writing Markdown scalability report")
	templatePath := filepath.Join(projectRootDir, "test", "scale", "testdata", "scalability_report.md.tmpl")
	tmpl, err := template.ParseFiles(templatePath)
	Expect(err).NotTo(HaveOccurred(), "Failed to parse scalability report template")

	reportData := struct {
		NodeCount int
		Mode      string
		Phases    []queryResult
	}{
		NodeCount: nodeCountUsed,
		Mode:      "continuous",
		Phases:    queryResults,
	}

	reportPath := filepath.Join(artifactsDir, "scalability_report.md")
	reportFile, err := os.OpenFile(reportPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600) // #nosec G304
	Expect(err).NotTo(HaveOccurred(), "Failed to open the file created for the scalability report")
	defer func() { _ = reportFile.Close() }()

	err = tmpl.Execute(reportFile, reportData)
	Expect(err).NotTo(HaveOccurred(), "Failed to template report data onto report file")

	if os.Getenv("SKIP_TEARDOWN") == "true" {
		By("Skipping teardown. Controller background process, KWOK cluster and Prometheus kept alive.")
		return
	}

	By("Terminating the background controller process")
	if controllerCmd != nil && controllerCmd.Process != nil {
		_ = controllerCmd.Process.Kill()
		_ = controllerCmd.Wait()
	}
	if controllerLogFile != nil {
		_ = controllerLogFile.Close()
	}

	By("Deleting the kwokctl cluster...")
	deleteCmd := exec.Command(kwokctlBinaryPath, "delete", "cluster", "--name", "kwok") // #nosec G204
	deleteOutput, err := utils.Run(deleteCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to delete kwok cluster:\n%s", deleteOutput)
})

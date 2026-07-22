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
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	. "github.com/onsi/gomega" //nolint:staticcheck
	"sigs.k8s.io/node-readiness-controller/test/utils"
)

func readEnvConfig() {
	if port := os.Getenv("CONTROLLER_METRICS_PORT"); port != "" {
		controllerMetricsPort = port
	}
	if port := os.Getenv("PROMETHEUS_PORT"); port != "" {
		prometheusPort = port
	}
}

func cleanupStaleResources(kwokctlPath string) {
	_ = exec.Command(kwokctlPath, "delete", "cluster").Run()
	_ = exec.Command("pkill", "-f", "node-readiness-controller").Run()
}

func createKwokCluster(kwokctlPath string, promPort string) {
	createArgs := []string{
		"create", "cluster",
		"--runtime", "binary",
		"--prometheus-port", promPort,
		"--enable-crds", "Stage",
	}
	if os.Getenv("DISABLE_QPS_LIMITS") == "true" {
		createArgs = append(createArgs, "--disable-qps-limits")
	}
	if leaseSecs := os.Getenv("NODE_LEASE_DURATION_SECONDS"); leaseSecs != "" {
		createArgs = append(createArgs, "--node-lease-duration-seconds", leaseSecs)
	}

	createCmd := exec.Command(kwokctlPath, createArgs...) // #nosec G204
	createOutput, err := utils.Run(createCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to create kwok cluster:\n%s", createOutput)
}

func applyCRDs() {
	crdConfigPath := filepath.Join(getProjectDir(), "config", "crd")
	crdCmd := exec.Command("kubectl", "apply", "-k", crdConfigPath) // #nosec G204
	crdOutput, err := utils.Run(crdCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to apply NodeReadinessRule CRD via Kustomize:\n%s", crdOutput)
}

func buildController() string {
	toolsBinDir := getToolsBinDir()
	controllerBinName := "node-readiness-controller"
	binPath := filepath.Join(toolsBinDir, controllerBinName)
	controllerMainPath := filepath.Join(".", "cmd", "main.go")

	buildCmd := exec.Command("go", "build", "-o", binPath, controllerMainPath) // #nosec G204
	buildOutput, err := utils.Run(buildCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to compile controller manager:\n%s", buildOutput)

	return binPath
}

func setupPrometheusScraper(metricsPort string, promPort string) {
	homeDir, err := os.UserHomeDir()
	Expect(err).NotTo(HaveOccurred(), "Failed to retrieve user home directory")
	prometheusConfigPath := filepath.Join(homeDir, ".kwok", "clusters", "kwok", "prometheus.yaml")

	prometheusConfigBytes, err := os.ReadFile(prometheusConfigPath) // #nosec G304
	Expect(err).NotTo(HaveOccurred(), "Failed to read Prometheus configuration")

	tmpl, err := template.New("prometheus-job").Parse(prometheusJobTemplate)
	Expect(err).NotTo(HaveOccurred(), "Failed to parse Prometheus job template")

	var jobConfig strings.Builder
	err = tmpl.Execute(&jobConfig, map[string]string{"Port": metricsPort})
	Expect(err).NotTo(HaveOccurred(), "Failed to execute Prometheus job template")

	newConfig := string(prometheusConfigBytes) + jobConfig.String()
	err = os.WriteFile(prometheusConfigPath, []byte(newConfig), 0600)
	Expect(err).NotTo(HaveOccurred(), "Failed to update Prometheus configuration with new job")

	err = exec.Command("pkill", "-HUP", "prometheus").Run()
	Expect(err).NotTo(HaveOccurred(), "Failed to restart Prometheus instance to load updated configuration")

	Eventually(func(g Gomega) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/-/ready", promPort))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	}, "30s", "1s").Should(Succeed(), "Prometheus is not ready")
}

func scaleKwokNodes(kwokctlPath string) int {
	nodeCount := defaultNodeCount
	if nodeCountStr := os.Getenv("NODE_COUNT"); nodeCountStr != "" {
		var err error
		nodeCount, err = strconv.Atoi(nodeCountStr)
		Expect(err).NotTo(HaveOccurred(), "Invalid NODE_COUNT: %s", nodeCountStr)
	}

	scaleCmd := exec.Command(kwokctlPath, "scale", "node", // #nosec G204
		"--replicas", strconv.Itoa(nodeCount),
		"--name", "kwok")
	scaleOutput, err := utils.Run(scaleCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to scale nodes: %s", scaleOutput)

	Eventually(func(g Gomega) int {
		count, err := countKwokNodes(context.Background(), "type=kwok")
		g.Expect(err).NotTo(HaveOccurred())
		return count
	}, "15m", "1s").Should(Equal(nodeCount), "Nodes failed to scale")

	return nodeCount
}

func setupArtifacts() (string, *os.File) {
	dir := os.Getenv("ARTIFACTS")
	if dir == "" {
		dir = filepath.Join(getProjectDir(), "test", "scale", "artifacts")
	}

	err := os.MkdirAll(dir, 0750)
	Expect(err).NotTo(HaveOccurred(), "Failed to create the artifacts subdirectory")

	logFile, err := os.Create(filepath.Join(dir, "controller.log")) // #nosec G304
	Expect(err).NotTo(HaveOccurred(), "Failed to create controller.log")

	return dir, logFile
}

func startControllerDaemon(binPath string, metricsPort string, logFile *os.File) *exec.Cmd {
	args := []string{
		fmt.Sprintf("--metrics-bind-address=:%s", metricsPort),
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

	cmd := exec.Command(binPath, args...) // #nosec G204
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	err := cmd.Start()
	Expect(err).NotTo(HaveOccurred(), "Failed to start controller process")

	Eventually(func(g Gomega) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/metrics", metricsPort))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	}, "15s", "500ms").Should(Succeed(), fmt.Sprintf("Controller failed to start or bind to port %s", metricsPort))

	return cmd
}

func generateScalabilityReport() {
	templatePath := filepath.Join(getProjectDir(), "test", "scale", "testdata", "scalability_report.md.tmpl")
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
}

func teardownKwokCluster(cmd *exec.Cmd, logFile *os.File, kwokctlPath string) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	if logFile != nil {
		_ = logFile.Close()
	}

	deleteCmd := exec.Command(kwokctlPath, "delete", "cluster", "--name", "kwok") // #nosec G204
	deleteOutput, err := utils.Run(deleteCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to delete kwok cluster:\n%s", deleteOutput)
}

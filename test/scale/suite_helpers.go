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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"

	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck
	. "github.com/onsi/gomega"    //nolint:staticcheck
	"sigs.k8s.io/node-readiness-controller/test/utils"
)

// getProjectDir is a convenience helper that wraps utils.GetProjectDir() and performs
// an immediate Gomega assertion on errors, eliminating repeated error-checking boilerplate
// across lifecycle helper functions.
func getProjectDir() string {
	dir, err := utils.GetProjectDir()
	Expect(err).NotTo(HaveOccurred(), "Failed to retrieve project directory")
	return dir
}

func getToolsBinDir() string {
	return filepath.Join(getProjectDir(), "hack", "tools", "bin")
}

func getArtifactsDir() string {
	return filepath.Join(getProjectDir(), "test", "scale", "artifacts")
}

func ensureKwokctl(version string) string {
	targetDir := getToolsBinDir()
	goOS := runtime.GOOS
	goArch := runtime.GOARCH

	binaryName := "kwokctl"
	if goOS == "windows" {
		binaryName += ".exe"
	}
	localBinaryPath := filepath.Join(targetDir, binaryName)

	if _, err := os.Stat(localBinaryPath); err == nil {
		return localBinaryPath
	}

	err := os.MkdirAll(targetDir, 0750)
	Expect(err).NotTo(HaveOccurred(), "Failed to create tools directory structure")
	downloadURL := fmt.Sprintf(
		"https://github.com/kubernetes-sigs/kwok/releases/download/%s/kwokctl-%s-%s",
		version, goOS, goArch,
	)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, downloadURL, nil)
	Expect(err).NotTo(HaveOccurred(), "Failed to create download request")
	resp, err := http.DefaultClient.Do(req) // #nosec G107
	Expect(err).NotTo(HaveOccurred(), "Failed to initiate kwokctl binary download")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		Fail(fmt.Sprintf("Failed to download kwokctl from URL %s: Status %s", downloadURL, resp.Status))
	}

	out, err := os.OpenFile(localBinaryPath, os.O_CREATE|os.O_WRONLY, 0700) // #nosec G304 G302
	Expect(err).NotTo(HaveOccurred(), "Failed to create local binary destination file")
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, resp.Body)
	Expect(err).NotTo(HaveOccurred(), "Failed to write binary content to disk target")

	return localBinaryPath
}

func readEnvConfig() {
	cfg = defaultScaleConfig

	if version := os.Getenv("KWOKCTL_VERSION"); version != "" {
		cfg.KwokctlVersion = version
	}

	cfg.ArtifactsDir = os.Getenv("ARTIFACTS")
	if cfg.ArtifactsDir == "" {
		cfg.ArtifactsDir = filepath.Join(getProjectDir(), "test", "scale", "artifacts")
	}

	if countStr := os.Getenv("NODE_COUNT"); countStr != "" {
		count, err := strconv.Atoi(countStr)
		Expect(err).NotTo(HaveOccurred(), "Invalid NODE_COUNT: %s", countStr)
		cfg.NodeCount = count
	}
	if port := os.Getenv("CONTROLLER_METRICS_PORT"); port != "" {
		cfg.MetricsPort = port
	}
	if port := os.Getenv("PROMETHEUS_PORT"); port != "" {
		cfg.PrometheusPort = port
	}
	if qps := os.Getenv("KUBE_API_QPS"); qps != "" {
		cfg.KubeAPIQPS = qps
	}
	if burst := os.Getenv("KUBE_API_BURST"); burst != "" {
		cfg.KubeAPIBurst = burst
	}
	if nodeConc := os.Getenv("NODE_CONCURRENT_RECONCILES"); nodeConc != "" {
		cfg.NodeConcurrentReconciles = nodeConc
	}
	if ruleConc := os.Getenv("RULE_CONCURRENT_RECONCILES"); ruleConc != "" {
		cfg.RuleConcurrentReconciles = ruleConc
	}
	if leaseSecs := os.Getenv("NODE_LEASE_DURATION_SECONDS"); leaseSecs != "" {
		cfg.NodeLeaseDurationSeconds = leaseSecs
	}
	if timeout := os.Getenv("TAINT_TIMEOUT"); timeout != "" {
		cfg.TaintTimeout = timeout
	}
	if timeout := os.Getenv("UNTAINT_TIMEOUT"); timeout != "" {
		cfg.UntaintTimeout = timeout
	}
	if mode := os.Getenv("ENFORCEMENT_MODE"); mode != "" {
		cfg.EnforcementMode = mode
	}
	if os.Getenv("DISABLE_QPS_LIMITS") == "true" {
		cfg.DisableQPSLimits = true
	}
	if os.Getenv("SKIP_TEARDOWN") == "true" {
		cfg.SkipTeardown = true
	}
}

func cleanupStaleResources(kwokctlPath string) {
	_ = exec.Command(kwokctlPath, "delete", "cluster").Run()
	_ = exec.Command("pkill", "-f", "node-readiness-controller").Run()
}

func createKwokCluster(kwokctlPath string) {
	createArgs := []string{
		"create", "cluster",
		"--runtime", "binary",
		"--prometheus-port", cfg.PrometheusPort,
		"--enable-crds", "Stage",
	}
	if cfg.DisableQPSLimits {
		createArgs = append(createArgs, "--disable-qps-limits")
	}
	if cfg.NodeLeaseDurationSeconds != "" {
		createArgs = append(createArgs, "--node-lease-duration-seconds", cfg.NodeLeaseDurationSeconds)
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

func setupPrometheusScraper() {
	homeDir, err := os.UserHomeDir()
	Expect(err).NotTo(HaveOccurred(), "Failed to retrieve user home directory")
	prometheusConfigPath := filepath.Join(homeDir, ".kwok", "clusters", "kwok", "prometheus.yaml")

	prometheusConfigBytes, err := os.ReadFile(prometheusConfigPath) // #nosec G304
	Expect(err).NotTo(HaveOccurred(), "Failed to read Prometheus configuration")

	tmpl, err := template.New("prometheus-job").Parse(prometheusJobTemplate)
	Expect(err).NotTo(HaveOccurred(), "Failed to parse Prometheus job template")

	var jobConfig strings.Builder
	err = tmpl.Execute(&jobConfig, map[string]string{"Port": cfg.MetricsPort})
	Expect(err).NotTo(HaveOccurred(), "Failed to execute Prometheus job template")

	newConfig := string(prometheusConfigBytes) + jobConfig.String()
	err = os.WriteFile(prometheusConfigPath, []byte(newConfig), 0600)
	Expect(err).NotTo(HaveOccurred(), "Failed to update Prometheus configuration with new job")

	err = exec.Command("pkill", "-HUP", "prometheus").Run()
	Expect(err).NotTo(HaveOccurred(), "Failed to restart Prometheus instance to load updated configuration")

	Eventually(func(g Gomega) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/-/ready", cfg.PrometheusPort))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	}, "30s", "1s").Should(Succeed(), "Prometheus is not ready")
}

func scaleKwokNodes(kwokctlPath string) {
	scaleCmd := exec.Command(kwokctlPath, "scale", "node", // #nosec G204
		"--replicas", strconv.Itoa(cfg.NodeCount),
		"--name", "kwok")
	scaleOutput, err := utils.Run(scaleCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to scale nodes: %s", scaleOutput)

	Eventually(func(g Gomega) int {
		count, err := countKwokNodes(context.Background(), "type=kwok")
		g.Expect(err).NotTo(HaveOccurred())
		return count
	}, "15m", "1s").Should(Equal(cfg.NodeCount), "Nodes failed to scale")
}

func setupArtifacts() *os.File {
	err := os.MkdirAll(cfg.ArtifactsDir, 0750)
	Expect(err).NotTo(HaveOccurred(), "Failed to create the artifacts subdirectory")

	logFile, err := os.Create(filepath.Join(cfg.ArtifactsDir, "controller.log")) // #nosec G304
	Expect(err).NotTo(HaveOccurred(), "Failed to create controller.log")

	return logFile
}

func startControllerDaemon(binPath string, logFile *os.File) *exec.Cmd {
	args := []string{
		fmt.Sprintf("--metrics-bind-address=:%s", cfg.MetricsPort),
		"--metrics-secure=false",
		"--leader-elect=false",
		"--enable-webhook=false",
	}
	if cfg.KubeAPIQPS != "" {
		args = append(args, "--kube-api-qps="+cfg.KubeAPIQPS)
	}
	if cfg.KubeAPIBurst != "" {
		args = append(args, "--kube-api-burst="+cfg.KubeAPIBurst)
	}
	if cfg.NodeConcurrentReconciles != "" {
		args = append(args, "--node-concurrent-reconciles="+cfg.NodeConcurrentReconciles)
	}
	if cfg.RuleConcurrentReconciles != "" {
		args = append(args, "--rule-concurrent-reconciles="+cfg.RuleConcurrentReconciles)
	}

	cmd := exec.Command(binPath, args...) // #nosec G204
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	err := cmd.Start()
	Expect(err).NotTo(HaveOccurred(), "Failed to start controller process")

	Eventually(func(g Gomega) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/metrics", cfg.MetricsPort))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	}, "15s", "500ms").Should(Succeed(), fmt.Sprintf("Controller failed to start or bind to port %s", cfg.MetricsPort))

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
		NodeCount: cfg.NodeCount,
		Mode:      cfg.EnforcementMode,
		Phases:    queryResults,
	}

	reportPath := filepath.Join(cfg.ArtifactsDir, "scalability_report.md")
	reportFile, err := os.OpenFile(reportPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600) // #nosec G304
	Expect(err).NotTo(HaveOccurred(), "Failed to open the file created for the scalability report")
	defer func() { _ = reportFile.Close() }()

	err = tmpl.Execute(reportFile, reportData)
	Expect(err).NotTo(HaveOccurred(), "Failed to template report data onto report file")

	// Generate JSON Report
	jsonPhases := make([]PhaseJSON, 0, len(queryResults))
	for _, q := range queryResults {
		jsonPhases = append(jsonPhases, buildPhaseJSON(q))
	}

	jsonReport := ScalabilityReportJSON{
		NodeCount: cfg.NodeCount,
		Mode:      cfg.EnforcementMode,
		Phases:    jsonPhases,
	}

	jsonBytes, err := json.MarshalIndent(jsonReport, "", "  ")
	Expect(err).NotTo(HaveOccurred(), "Failed to marshal scalability report to JSON")

	jsonReportPath := filepath.Join(cfg.ArtifactsDir, "scalability_report.json")
	err = os.WriteFile(jsonReportPath, jsonBytes, 0600)
	Expect(err).NotTo(HaveOccurred(), "Failed to write scalability report JSON file")
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

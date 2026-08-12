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
	_ "embed"
	"os"
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestScale(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node Readiness Controller Scale Performance Suite")
}

type scaleConfig struct {
	KwokctlVersion           string
	MetricsPort              string
	PrometheusPort           string
	NodeCount                int
	ArtifactsDir             string
	KubeAPIQPS               string
	KubeAPIBurst             string
	NodeConcurrentReconciles string
	RuleConcurrentReconciles string
	DisableQPSLimits         bool
	NodeLeaseDurationSeconds string
	SkipTeardown             bool
	TaintTimeout             string
	UntaintTimeout           string
	EnforcementMode          string
}

var defaultScaleConfig = scaleConfig{
	KwokctlVersion:  "v0.8.0",
	MetricsPort:     "8080",
	PrometheusPort:  "9090",
	NodeCount:       1000,
	TaintTimeout:    "15m",
	UntaintTimeout:  "15m",
	EnforcementMode: "continuous",
}

var (
	cfg               scaleConfig
	kwokctlBinaryPath string
	controllerBinPath string
	controllerCmd     *exec.Cmd
	controllerLogFile *os.File
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
	readEnvConfig()

	By("Ensuring kwokctl binary is present")
	kwokctlBinaryPath = ensureKwokctl(cfg.KwokctlVersion)

	By("Cleaning up any existing simulated cluster and stale controller processes")
	cleanupStaleResources(kwokctlBinaryPath)

	By("Creating the simulated KWOK cluster")
	createKwokCluster(kwokctlBinaryPath)

	By("Applying NodeReadinessRule CRD manifests")
	applyCRDs()

	By("Compiling node-readiness-controller manager binary")
	controllerBinPath = buildController()

	By("Configuring controller scraper job in Prometheus config")
	setupPrometheusScraper()

	By("Scaling nodes up")
	scaleKwokNodes(kwokctlBinaryPath)

	By("Creating subdirectory for storing test artifacts and log file")
	controllerLogFile = setupArtifacts()

	By("Starting the node-readiness-controller manager daemon process")
	controllerCmd = startControllerDaemon(controllerBinPath, controllerLogFile)
})

var _ = AfterSuite(func() {
	By("Writing Markdown scalability report")
	generateScalabilityReport()

	if cfg.SkipTeardown {
		By("Skipping teardown. Controller background process, KWOK cluster and Prometheus kept alive.")
		return
	}

	By("Terminating background process and deleting KWOK cluster")
	teardownKwokCluster(controllerCmd, controllerLogFile, kwokctlBinaryPath)
})

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
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	corev1 "k8s.io/api/core/v1"

	nodereadinessiov1alpha1 "sigs.k8s.io/node-readiness-controller/api/v1alpha1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	// Create a context with proper timeout
	ctx, cancel = context.WithCancel(context.Background())

	var err error
	err = nodereadinessiov1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = corev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		UseExistingCluster:    new(bool), // Ensure we use a test cluster
	}

	// Retrieve the first found binary directory to allow running tests from IDEs
	binaryDir := getFirstFoundEnvTestBinaryDir()
	if binaryDir != "" {
		By("Using envtest binaries from: " + binaryDir)
		testEnv.BinaryAssetsDirectory = binaryDir
	} else {
		By("Using default envtest binaries (KUBEBUILDER_ASSETS)")
	}

	// cfg is defined in this file globally.
	By("starting test environment")
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	By("test environment ready")
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")

	// Cancel context first to stop any ongoing operations
	if cancel != nil {
		cancel()
	}

	// Clean up test resources
	By("cleaning up test resources")
	if k8sClient != nil {
		cleanupCtx := context.Background()

		// Delete all NodeReadinessGateRules (remove finalizers first)
		ruleList := &nodereadinessiov1alpha1.NodeReadinessGateRuleList{}
		if err := k8sClient.List(cleanupCtx, ruleList); err == nil {
			for i := range ruleList.Items {
				rule := &ruleList.Items[i]
				// Remove finalizers to allow deletion
				rule.Finalizers = nil
				_ = k8sClient.Update(cleanupCtx, rule)
				_ = k8sClient.Delete(cleanupCtx, rule)
			}
		}

		// Delete all nodes
		nodeList := &corev1.NodeList{}
		if err := k8sClient.List(cleanupCtx, nodeList); err == nil {
			for i := range nodeList.Items {
				_ = k8sClient.Delete(cleanupCtx, &nodeList.Items[i])
			}
		}
	}

	// Stop the test environment
	By("stopping test environment")
	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}
})

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Makefile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
//
// This function helps finding the required binaries, equivalent to
// setting the 'KUBEBUILDER_ASSETS' environment variable. To ensure the binaries are
// properly set up, run 'make setup-envtest' beforehand.
func getFirstFoundEnvTestBinaryDir() string {
	// First check if KUBEBUILDER_ASSETS is already set
	if assets := os.Getenv("KUBEBUILDER_ASSETS"); assets != "" {
		if _, err := os.Stat(assets); err == nil {
			return assets
		}
	}

	// Try to find binaries in the expected location
	basePath := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		// Don't log error here - it's expected in CI environments
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			fullPath := filepath.Join(basePath, entry.Name())
			// Verify that this directory actually contains the required binaries
			if hasRequiredBinaries(fullPath) {
				return fullPath
			}
		}
	}
	return ""
}

// hasRequiredBinaries checks if the directory contains the essential envtest binaries
func hasRequiredBinaries(dir string) bool {
	requiredBinaries := []string{"kube-apiserver", "etcd", "kubectl"}
	for _, binary := range requiredBinaries {
		binaryPath := filepath.Join(dir, binary)
		if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
			return false
		}
	}
	return true
}

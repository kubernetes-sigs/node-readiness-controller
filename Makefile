# Copyright The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

.DEFAULT_GOAL:=help

#
# Directories.
#
# Full directory of where the Makefile resides
ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
BIN_DIR := bin
TEST_DIR := test
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(abspath $(TOOLS_DIR)/$(BIN_DIR))
GO_INSTALL := ./scripts/go_install.sh

export PATH := $(abspath $(TOOLS_BIN_DIR)):$(PATH)

#
# Binaries.
#
KUBECTL ?= kubectl
KIND ?= kind

KUSTOMIZE_VER := v5.7.0
KUSTOMIZE_BIN := kustomize
KUSTOMIZE := $(abspath $(TOOLS_BIN_DIR)/$(KUSTOMIZE_BIN)-$(KUSTOMIZE_VER))
KUSTOMIZE_PKG := sigs.k8s.io/kustomize/kustomize/v5

SETUP_ENVTEST_VER := release-0.22
SETUP_ENVTEST_BIN := setup-envtest
SETUP_ENVTEST := $(abspath $(TOOLS_BIN_DIR)/$(SETUP_ENVTEST_BIN)-$(SETUP_ENVTEST_VER))
SETUP_ENVTEST_PKG := sigs.k8s.io/controller-runtime/tools/setup-envtest
KUBEBUILDER_ENVTEST_KUBERNETES_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')

GOLANGCI_LINT_BIN := golangci-lint
GOLANGCI_LINT_VER ?= v2.4.0
GOLANGCI_LINT := $(abspath $(TOOLS_BIN_DIR)/$(GOLANGCI_LINT_BIN)-$(GOLANGCI_LINT_VER))
GOLANGCI_LINT_PKG := github.com/golangci/golangci-lint/v2/cmd/golangci-lint

GOLANGCI_LINT_KAL_BIN := golangci-lint-kube-api-linter
GOLANGCI_LINT_KAL_VER := $(shell cat ./hack/tools/.custom-gcl.yaml | grep version: | sed 's/version: //')
GOLANGCI_LINT_KAL := $(abspath $(TOOLS_BIN_DIR)/$(GOLANGCI_LINT_KAL_BIN))

CONTROLLER_GEN_VER := v0.19.0
CONTROLLER_GEN_BIN := controller-gen
CONTROLLER_GEN := $(abspath $(TOOLS_BIN_DIR)/$(CONTROLLER_GEN_BIN)-$(CONTROLLER_GEN_VER))
CONTROLLER_GEN_PKG := sigs.k8s.io/controller-tools/cmd/controller-gen

# Image URL to use all building/pushing image targets
IMG_PREFIX ?= controller
IMG_TAG ?= latest

# ENABLE_METRICS: If set to true, includes Prometheus Service resources.
ENABLE_METRICS ?= false
ENABLE_TLS ?= false
# ENABLE_WEBHOOK: If set to true, includes validating webhook. Requires ENABLE_TLS=true.
ENABLE_WEBHOOK ?= false

# Default value for ignore-not-found flag in undeploy target
ignore-not-found ?= true

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

## --------------------------------------
## General
## --------------------------------------
##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

## --------------------------------------
## Generate / Manifests
## --------------------------------------

##@ generate

.PHONY: manifests
manifests: $(CONTROLLER_GEN) ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: $(CONTROLLER_GEN) ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate/boilerplate.go.txt" paths="./..."

## --------------------------------------
## Lint / Verify
## --------------------------------------

##@ lint and verify:

.PHONY: lint
lint: $(GOLANGCI_LINT) $(GOLANGCI_LINT_KAL) ## Run golangci-lint linter
	$(GOLANGCI_LINT) run -v $(GOLANGCI_LINT_EXTRA_ARGS)
	$(GOLANGCI_LINT_KAL) run -v --config $(ROOT_DIR)/.golangci-kal.yml $(GOLANGCI_LINT_EXTRA_ARGS)

.PHONY: lint-fix
lint-fix: $(GOLANGCI_LINT) ## Run golangci-lint linter and perform fixes
	GOLANGCI_LINT_EXTRA_ARGS=--fix $(MAKE) lint

.PHONY: lint-api
lint-api: $(GOLANGCI_LINT_KAL)
	$(GOLANGCI_LINT_KAL) run -v --config $(ROOT_DIR)/.golangci-kal.yml $(GOLANGCI_LINT_EXTRA_ARGS)

.PHONY: lint-api-fix
lint-api-fix: $(GOLANGCI_LINT_KAL)
	GOLANGCI_LINT_EXTRA_ARGS=--fix $(MAKE) lint-api

.PHONY: lint-config
lint-config: $(GOLANGCI_LINT) ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) config verify

.PHONY: verify
verify: ## Run all verification scripts.
	 ./hack/verify-all.sh

## --------------------------------------
## Build
## --------------------------------------

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	DOCKER_BUILDKIT=1 $(CONTAINER_TOOL) build -t ${IMG_PREFIX}:${IMG_TAG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG_PREFIX}:${IMG_TAG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG_PREFIX=myregistry/mypoperator IMG_TAG=0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via ${IMG_PREFIX}:${IMG_TAG} then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	- $(CONTAINER_TOOL) buildx create --name nrrcontroller-builder
	$(CONTAINER_TOOL) buildx use nrrcontroller-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG_PREFIX}:${IMG_TAG} .
	- $(CONTAINER_TOOL) buildx rm nrrcontroller-builder

.PHONY: docker-build-reporter
docker-build-reporter: ## Build docker image with the reporter.
	DOCKER_BUILDKIT=1 $(CONTAINER_TOOL) build -f Dockerfile.reporter -t ${IMG_PREFIX}:${IMG_TAG} .

.PHONY: docker-push-reporter
docker-push-reporter: ## Push docker image with the reporter.
	$(CONTAINER_TOOL) push ${IMG_PREFIX}:${IMG_TAG}

.PHONY: docker-buildx-reporter
docker-buildx-reporter: ## Build and push docker image for the reporter for cross-platform support
	- $(CONTAINER_TOOL) buildx create --name reporter-builder
	$(CONTAINER_TOOL) buildx use reporter-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG_PREFIX}:${IMG_TAG} -f Dockerfile.reporter .
	- $(CONTAINER_TOOL) buildx rm reporter-builder

.PHONY: build-installer
build-installer: build-manifests-temp ## Generate CRDs and deployment manifests for release.
	mkdir -p dist
	# Generate CRDs only
	$(KUSTOMIZE) build config/crd > dist/crds.yaml
	@echo "Generated dist/crds.yaml"
	# Generate controller deployment without CRDs
	cp $(BUILD_DIR)/manifests.yaml dist/install.yaml
	@echo "Generated dist/install.yaml with image ${IMG_PREFIX}:${IMG_TAG}"
	# Generate controller deployment with metrics
	$(MAKE) build-manifests-temp ENABLE_METRICS=true
	cp $(BUILD_DIR)/manifests.yaml dist/install-with-metrics.yaml
	@echo "Generated dist/install-with-metrics.yaml with image ${IMG_PREFIX}:${IMG_TAG}"
	# Generate controller deployment with secure metrics
	$(MAKE) build-manifests-temp ENABLE_METRICS=true ENABLE_TLS=true
	cp $(BUILD_DIR)/manifests.yaml dist/install-with-secure-metrics.yaml
	@echo "Generated dist/install-with-secure-metrics.yaml with image ${IMG_PREFIX}:${IMG_TAG}"
	# Generate controller deployment with webhook
	$(MAKE) build-manifests-temp ENABLE_TLS=true ENABLE_WEBHOOK=true
	cp $(BUILD_DIR)/manifests.yaml dist/install-with-webhook.yaml
	@echo "Generated dist/install-with-webhook.yaml with image ${IMG_PREFIX}:${IMG_TAG}"
	@echo "NOTE: Install crds.yaml first, then install.yaml, install-with-metrics.yaml, install-with-secure-metrics.yaml, or install-with-webhook.yaml. Deployment runs on any available node by default."

## --------------------------------------
## Deployment
## --------------------------------------

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

# Temporary directory for building manifests
BUILD_DIR := $(ROOT_DIR)/bin/build

# Build manifests in a temp directory to keep source config clean.
# Features are enabled by adding Kustomize Components.
.PHONY: build-manifests-temp
build-manifests-temp: manifests $(KUSTOMIZE)
	@mkdir -p $(BUILD_DIR)
	@rm -rf $(BUILD_DIR)/config
	@cp -r config $(BUILD_DIR)/
	@cd $(BUILD_DIR)/config/manager && $(KUSTOMIZE) edit set image controller=${IMG_PREFIX}:${IMG_TAG}
	@# TLS: Add certmanager component for certificates
	@if [ "$(ENABLE_TLS)" = "true" ]; then \
		cd $(BUILD_DIR)/config/default && $(KUSTOMIZE) edit add component ../certmanager; \
	fi
	@# Webhook: Requires TLS for certificates
	@if [ "$(ENABLE_WEBHOOK)" = "true" ]; then \
		if [ "$(ENABLE_TLS)" != "true" ]; then \
			echo "ERROR: ENABLE_WEBHOOK=true requires ENABLE_TLS=true"; exit 1; \
		fi; \
		cd $(BUILD_DIR)/config/default && $(KUSTOMIZE) edit add component ../webhook; \
	fi
	@# Metrics: Add prometheus, with TLS config if enabled
	@if [ "$(ENABLE_METRICS)" = "true" ]; then \
		cd $(BUILD_DIR)/config/default && $(KUSTOMIZE) edit add component ../prometheus; \
		if [ "$(ENABLE_TLS)" = "true" ]; then \
			cd $(BUILD_DIR)/config/default && $(KUSTOMIZE) edit add component ../prometheus/tls; \
		else \
			cd $(BUILD_DIR)/config/prometheus && $(KUSTOMIZE) edit add patch --path manager_prometheus_metrics.yaml --kind Deployment --name controller-manager; \
		fi; \
	fi
	@$(KUSTOMIZE) build $(BUILD_DIR)/config/default > $(BUILD_DIR)/manifests.yaml
	@rm -rf $(BUILD_DIR)/config


.PHONY: install
install: manifests $(KUSTOMIZE) ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	@out="$$( $(KUSTOMIZE) build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | $(KUBECTL) apply -f -; else echo "No CRDs to install; skipping."; fi

.PHONY: uninstall
uninstall: manifests $(KUSTOMIZE) ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	@out="$$( $(KUSTOMIZE) build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -; else echo "No CRDs to delete; skipping."; fi

.PHONY: deploy
deploy: build-manifests-temp ## Deploy controller to the K8s cluster. Use ENABLE_METRICS=true and ENABLE_TLS=true to enable features.
	$(KUBECTL) apply -f $(BUILD_DIR)/manifests.yaml

.PHONY: undeploy
undeploy: build-manifests-temp ## Undeploy controller from the K8s cluster. Use ENABLE_METRICS=true and ENABLE_TLS=true if they were enabled during deploy.
	$(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f $(BUILD_DIR)/manifests.yaml

.PHONY: deploy-with-metrics
deploy-with-metrics: ENABLE_METRICS=true
deploy-with-metrics: deploy ## Deploy with metrics (HTTP).

.PHONY: undeploy-with-metrics
undeploy-with-metrics: ENABLE_METRICS=true
undeploy-with-metrics: undeploy ## Undeploy with metrics.

.PHONY: deploy-with-metrics-and-tls
deploy-with-metrics-and-tls: ENABLE_METRICS=true
deploy-with-metrics-and-tls: ENABLE_TLS=true
deploy-with-metrics-and-tls: deploy ## Deploy with metrics and TLS.

.PHONY: undeploy-with-metrics-and-tls
undeploy-with-metrics-and-tls: ENABLE_METRICS=true
undeploy-with-metrics-and-tls: ENABLE_TLS=true
undeploy-with-metrics-and-tls: undeploy ## Undeploy with metrics and TLS.

.PHONY: deploy-with-tls
deploy-with-tls: ENABLE_TLS=true
deploy-with-tls: deploy ## Deploy with TLS (cert-manager).

.PHONY: undeploy-with-tls
undeploy-with-tls: ENABLE_TLS=true
undeploy-with-tls: undeploy ## Undeploy with TLS.

.PHONY: deploy-with-webhook
deploy-with-webhook: ENABLE_TLS=true
deploy-with-webhook: ENABLE_WEBHOOK=true
deploy-with-webhook: deploy ## Deploy with webhook (includes TLS).

.PHONY: undeploy-with-webhook
undeploy-with-webhook: ENABLE_TLS=true
undeploy-with-webhook: ENABLE_WEBHOOK=true
undeploy-with-webhook: undeploy ## Undeploy with webhook.

# Deploy with all features: metrics, TLS, webhook.
.PHONY: deploy-full
deploy-full: ENABLE_METRICS=true
deploy-full: ENABLE_TLS=true
deploy-full: ENABLE_WEBHOOK=true
deploy-full: deploy ## Deploy with all features: metrics, TLS, webhook.

.PHONY: undeploy-full
undeploy-full: ENABLE_METRICS=true
undeploy-full: ENABLE_TLS=true
undeploy-full: ENABLE_WEBHOOK=true
undeploy-full: undeploy ## Undeploy with all features.

## --------------------------------------
## Testing
## --------------------------------------

##@ test:

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(KUBEBUILDER_ASSETS)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

# TODO(user): To use a different vendor for e2e tests, modify the setup under 'tests/e2e'.
# The default setup assumes Kind is pre-installed and builds/loads the Manager Docker image locally.
# CertManager is installed by default; skip with:
# - CERT_MANAGER_INSTALL_SKIP=true
KIND_CLUSTER ?= nrr-test-e2e

.PHONY: setup-test-e2e
setup-test-e2e: ## Set up a Kind cluster for e2e tests if it does not exist
	@command -v $(KIND) >/dev/null 2>&1 || { \
		echo "Kind is not installed. Please install Kind manually."; \
		exit 1; \
	}
	@case "$$($(KIND) get clusters)" in \
		*"$(KIND_CLUSTER)"*) \
			echo "Kind cluster '$(KIND_CLUSTER)' already exists. Skipping creation." ;; \
		*) \
			echo "Creating Kind cluster '$(KIND_CLUSTER)'..."; \
			$(KIND) create cluster --name $(KIND_CLUSTER) ;; \
	esac

.PHONY: test-e2e
test-e2e: setup-test-e2e manifests generate fmt vet ## Run the e2e tests. Expected an isolated environment using Kind.
	KIND=$(KIND) KIND_CLUSTER=$(KIND_CLUSTER) go test -tags=e2e ./test/e2e/ -v -ginkgo.v
	$(MAKE) cleanup-test-e2e

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Tear down the Kind cluster used for e2e tests
	@$(KIND) delete cluster --name $(KIND_CLUSTER)

KUBEBUILDER_ASSETS ?= $(shell $(SETUP_ENVTEST) use --use-env -p path $(KUBEBUILDER_ENVTEST_KUBERNETES_VERSION))

.PHONY: setup-envtest
setup-envtest: $(SETUP_ENVTEST) ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(KUBEBUILDER_ENVTEST_KUBERNETES_VERSION)..."
	@echo KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS)

## --------------------------------------
## Hack / Tools
## --------------------------------------

##@ hack/tools:

.PHONY: $(KUSTOMIZE_BIN)
$(KUSTOMIZE_BIN): $(KUSTOMIZE) ## Build a local copy of kustomize.

.PHONY: $(CONTROLLER_GEN_BIN)
$(CONTROLLER_GEN_BIN): $(CONTROLLER_GEN) ## Build a local copy of controller-gen.

.PHONY: $(SETUP_ENVTEST_BIN)
$(SETUP_ENVTEST_BIN): $(SETUP_ENVTEST) ## Build a local copy of setup-envtest.

.PHONY: $(GOLANGCI_LINT_BIN)
$(GOLANGCI_LINT_BIN): $(GOLANGCI_LINT) ## Build a local copy of golangci-lint.

$(KUSTOMIZE): # Build kustomize from tools folder.
	CGO_ENABLED=0 GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) $(KUSTOMIZE_PKG) $(KUSTOMIZE_BIN) $(KUSTOMIZE_VER)

$(CONTROLLER_GEN): # Build controller-gen from tools folder.
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) $(CONTROLLER_GEN_PKG) $(CONTROLLER_GEN_BIN) $(CONTROLLER_GEN_VER)

$(SETUP_ENVTEST): # Build setup-envtest from tools folder.
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) $(SETUP_ENVTEST_PKG) $(SETUP_ENVTEST_BIN) $(SETUP_ENVTEST_VER)

$(GOLANGCI_LINT): # Build golangci-lint from tools folder.
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) $(GOLANGCI_LINT_PKG) $(GOLANGCI_LINT_BIN) $(GOLANGCI_LINT_VER)

$(GOLANGCI_LINT_KAL): $(GOLANGCI_LINT) # Build golangci-lint-kal from custom configuration.
	cd $(TOOLS_DIR); $(GOLANGCI_LINT) custom


## --------------------------------------
## Documentation
## --------------------------------------

##@ docs

MDBOOK_VERSION ?= 0.5.2
GO_VERSION ?= 1.25.5
MDBOOK_SCRIPT := $(ROOT_DIR)/docs/book/install-and-build-mdbook.sh


.PHONY: docs
docs: ## Build the mdBook locally using the same script Netlify uses.
	GO_VERSION=$(GO_VERSION) MDBOOK_VERSION=$(MDBOOK_VERSION) $(MDBOOK_SCRIPT)

.PHONY: docs-serve
docs-serve: ## Serve mdBook locally.
	GO_VERSION=$(GO_VERSION) MDBOOK_VERSION=$(MDBOOK_VERSION) $(MDBOOK_SCRIPT) serve docs/book --open

# generate CRD spec doc
.PHONY: crd-ref-docs
crd-ref-docs:
	crd-ref-docs \
		--source-path=${PWD}/api/v1alpha1/ \
		--config=crd-ref-docs.yaml \
		--renderer=markdown \
		--output-path=${PWD}/docs/book/src/reference/api-spec.md
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

# Scale Testing Makefile for Node Readiness Controller
# This Makefile provides targets for setting up and running scale tests
# with monitoring using Podman and Kind.

.DEFAULT_GOAL := help

# Directories
SCALE_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
PROJECT_ROOT := $(shell cd $(SCALE_DIR)/../../.. && pwd)

# Configuration
CLUSTER_NAME ?= nrr-test
CONTAINER_TOOL ?= podman
IMG_PREFIX ?= controller
IMG_TAG ?= latest
KIND_CONFIG ?= $(SCALE_DIR)/kind-config.yaml

# Tools
KUBECTL ?= kubectl
KIND ?= kind
HELM ?= helm

# Namespace for controller
CONTROLLER_NAMESPACE ?= nrr-system

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Setup

.PHONY: setup
setup: create-cluster install-controller install-monitoring ## Complete setup: cluster + controller + monitoring
	@echo ""
	@echo "✅ Setup complete!"
	@echo ""
	@echo "Next steps:"
	@echo "  1. Run scale test: make test NODE_COUNT=1000 RULE_COUNT=3"
	@echo "  2. View Grafana: http://localhost:3000 (credentials shown above)"
	@echo "  3. Import dashboard: $(SCALE_DIR)/grafana-dashboard.json"

.PHONY: create-cluster
create-cluster: ## Create Kind cluster with scale test configuration
	@echo "==> Creating Kind cluster: $(CLUSTER_NAME)"
	@if $(KIND) get clusters | grep -q "^$(CLUSTER_NAME)$$"; then \
		echo "Cluster $(CLUSTER_NAME) already exists"; \
	else \
		$(KIND) create cluster --config $(KIND_CONFIG) --name $(CLUSTER_NAME); \
		echo "✓ Cluster created"; \
	fi

.PHONY: install-controller
install-controller: ## Build and install NRR controller with Podman or Docker
	@echo "==> Installing NRR Controller"
	@echo "Step 1: Installing CRDs..."
	@cd $(PROJECT_ROOT) && $(MAKE) install
	@echo ""
	@echo "Step 2: Building controller image with $(CONTAINER_TOOL)..."
ifeq ($(CONTAINER_TOOL),podman)
	@cd $(PROJECT_ROOT) && $(MAKE) podman-build IMG_PREFIX=$(IMG_PREFIX) IMG_TAG=$(IMG_TAG)
else
	@cd $(PROJECT_ROOT) && $(MAKE) docker-build IMG_PREFIX=$(IMG_PREFIX) IMG_TAG=$(IMG_TAG)
endif
	@echo ""
	@echo "Step 3: Loading image into Kind cluster..."
	@cd $(PROJECT_ROOT) && $(MAKE) kind-load CONTAINER_TOOL=$(CONTAINER_TOOL) IMG_PREFIX=$(IMG_PREFIX) IMG_TAG=$(IMG_TAG) KIND_CLUSTER=$(CLUSTER_NAME)
	@echo ""
	@echo "Step 4: Deploying controller with metrics enabled..."
ifeq ($(CONTAINER_TOOL),podman)
	@cd $(PROJECT_ROOT) && $(MAKE) deploy IMG_PREFIX=localhost/$(IMG_PREFIX) IMG_TAG=$(IMG_TAG) ENABLE_METRICS=true
else
	@cd $(PROJECT_ROOT) && $(MAKE) deploy IMG_PREFIX=$(IMG_PREFIX) IMG_TAG=$(IMG_TAG) ENABLE_METRICS=true
endif
	@echo ""
	@echo "Step 5: Waiting for controller to be ready..."
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
		echo "Checking pod status (attempt $$i/10)..."; \
		$(KUBECTL) get pods -n $(CONTROLLER_NAMESPACE) -o wide 2>/dev/null || true; \
		if $(KUBECTL) get pods -n $(CONTROLLER_NAMESPACE) -o jsonpath='{.items[*].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null | grep -q "True"; then \
			echo "✓ Controller is running!"; \
			break; \
		fi; \
		[ $$i -lt 10 ] && sleep 10; \
	done
	@echo "✓ Controller installed"

.PHONY: install-monitoring
install-monitoring: ## Install Prometheus and Grafana monitoring stack
	@echo "==> Installing Monitoring Stack"
	@echo "Step 1: Adding Prometheus Helm repository..."
	@$(HELM) repo add prometheus-community https://prometheus-community.github.io/helm-charts 2>/dev/null || true
	@$(HELM) repo update
	@echo ""
	@echo "Step 2: Creating monitoring namespace..."
	@$(KUBECTL) create namespace monitoring --dry-run=client -o yaml | $(KUBECTL) apply -f -
	@echo ""
	@echo "Step 3: Installing or updating kube-prometheus-stack..."
	@$(HELM) upgrade --install prom-stack prometheus-community/kube-prometheus-stack \
		--namespace monitoring \
		--set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
		--set prometheus.prometheusSpec.scrapeInterval=5s \
		--set nodeExporter.enabled=false \
		--set grafana.adminPassword=admin \
		--wait --timeout 5m
	@echo "✓ Prometheus stack installed"
	@echo ""
	@echo "Step 4: Creating ServiceMonitor for NRR metrics..."
	@$(KUBECTL) apply -f $(SCALE_DIR)/servicemonitor.yaml
	@echo "✓ ServiceMonitor created"
	@echo ""
	@echo "Step 5: Setting up port forwarding..."
	@$(MAKE) port-forward
	@echo ""
	@echo "✓ Monitoring stack installed"
	@echo ""
	@echo "📊 Access URLs:"
	@echo "  Grafana:    http://localhost:3000"
	@echo "  Prometheus: http://localhost:9090"
	@echo ""
	@echo "🔐 Grafana Credentials:"
	@echo "  Username: admin"
	@echo "  Password: admin"

.PHONY: port-forward
port-forward: ## Setup port forwarding for Grafana and Prometheus
	@echo "Setting up port forwarding..."
	@pkill -f "port-forward.*grafana" 2>/dev/null || true
	@pkill -f "port-forward.*prometheus" 2>/dev/null || true
	@$(KUBECTL) port-forward -n monitoring svc/prom-stack-grafana 3000:80 > /dev/null 2>&1 &
	@$(KUBECTL) port-forward -n monitoring svc/prom-stack-kube-prometheus-prometheus 9090:9090 > /dev/null 2>&1 &
	@sleep 2
	@echo "✓ Port forwarding active (Grafana: 3000, Prometheus: 9090)"

##@ Testing

.PHONY: test
test: ## Run scale test (usage: make test NODE_COUNT=1000 RULE_COUNT=3)
	@echo "==> Running Scale Test"
	@if [ -z "$(NODE_COUNT)" ]; then \
		echo "Usage: make test NODE_COUNT=<count> [RULE_COUNT=<count>]"; \
		echo "Example: make test NODE_COUNT=1000 RULE_COUNT=3"; \
		exit 1; \
	fi
	@$(SCALE_DIR)/scale-test.sh $(NODE_COUNT) $(if $(RULE_COUNT),$(RULE_COUNT),1)

.PHONY: test-quick
test-quick: ## Quick test with 100 nodes and 1 rule
	@$(MAKE) test NODE_COUNT=100 RULE_COUNT=1

.PHONY: test-medium
test-medium: ## Medium test with 500 nodes and 2 rules
	@$(MAKE) test NODE_COUNT=500 RULE_COUNT=2

.PHONY: test-large
test-large: ## Large test with 1000 nodes and 3 rules
	@$(MAKE) test NODE_COUNT=1000 RULE_COUNT=3

.PHONY: test-xlarge
test-xlarge: ## Extra large test with 5000 nodes and 5 rules
	@$(MAKE) test NODE_COUNT=5000 RULE_COUNT=5

##@ Monitoring

.PHONY: dashboard
dashboard: ## Open Grafana dashboard in browser
	@echo "Opening Grafana dashboard..."
	@echo "URL: http://localhost:3000"
	@echo "Username: admin"
	@echo "Password: admin"
	@echo ""
	@echo "Import dashboard from: $(SCALE_DIR)/grafana-dashboard.json"
	@open http://localhost:3000 2>/dev/null || xdg-open http://localhost:3000 2>/dev/null || echo "Please open http://localhost:3000 manually"

.PHONY: prometheus
prometheus: ## Open Prometheus UI in browser
	@echo "Opening Prometheus UI..."
	@open http://localhost:9090 2>/dev/null || xdg-open http://localhost:9090 2>/dev/null || echo "Please open http://localhost:9090 manually"

.PHONY: logs
logs: ## Show controller logs
	@$(KUBECTL) logs -n $(CONTROLLER_NAMESPACE) -l control-plane=controller-manager --tail=100 -f

.PHONY: metrics
metrics: ## Show current Prometheus-formatted metrics from the controller Service
	@echo "Fetching Prometheus metrics from controller Service..."
	@$(KUBECTL) get --raw "/api/v1/namespaces/$(CONTROLLER_NAMESPACE)/services/http:metrics-service:8080/proxy/metrics" 2>/dev/null || \
		echo "Unable to fetch /metrics via metrics-service. Ensure the controller is deployed with ENABLE_METRICS=true and the Service exists."

.PHONY: etcd-size
etcd-size: ## Monitor etcd size for NodeReadinessRules
	@$(SCALE_DIR)/etcd-size.sh

##@ Cleanup

.PHONY: clean-test
clean-test: ## Clean up test resources (nodes and rules)
	@echo "==> Cleaning up test resources"
	@$(SCALE_DIR)/cleanup-kwok-nodes-rules.sh
	@echo "✓ Test resources cleaned"

.PHONY: clean-monitoring
clean-monitoring: ## Uninstall monitoring stack
	@echo "==> Uninstalling monitoring stack"
	@pkill -f "port-forward.*grafana" 2>/dev/null || true
	@pkill -f "port-forward.*prometheus" 2>/dev/null || true
	@$(HELM) uninstall prom-stack -n monitoring 2>/dev/null || true
	@$(KUBECTL) delete namespace monitoring --ignore-not-found=true
	@echo "✓ Monitoring stack removed"

.PHONY: clean-controller
clean-controller: ## Uninstall controller
	@echo "==> Uninstalling controller"
	@cd $(PROJECT_ROOT) && $(MAKE) undeploy ENABLE_METRICS=true 2>/dev/null || true
	@cd $(PROJECT_ROOT) && $(MAKE) uninstall 2>/dev/null || true
	@echo "✓ Controller uninstalled"

.PHONY: clean-cluster
clean-cluster: ## Delete Kind cluster
	@echo "==> Deleting Kind cluster: $(CLUSTER_NAME)"
	@$(KIND) delete cluster --name $(CLUSTER_NAME)
	@echo "✓ Cluster deleted"

.PHONY: clean
clean: clean-test clean-monitoring clean-controller clean-cluster ## Complete cleanup: remove everything
	@echo ""
	@echo "✅ Complete cleanup finished"

##@ Utilities

.PHONY: status
status: ## Show status of all components
	@echo "==> Cluster Status"
	@echo "Cluster: $(CLUSTER_NAME)"
	@$(KIND) get clusters | grep "^$(CLUSTER_NAME)$$" && echo "✓ Cluster exists" || echo "✗ Cluster not found"
	@echo ""
	@echo "==> Controller Status"
	@$(KUBECTL) get pods -n $(CONTROLLER_NAMESPACE) 2>/dev/null || echo "✗ Controller not deployed"
	@echo ""
	@echo "==> Monitoring Status"
	@$(KUBECTL) get pods -n monitoring 2>/dev/null || echo "✗ Monitoring not deployed"
	@echo ""
	@echo "==> Test Resources"
	@echo "NodeReadinessRules:"
	@$(KUBECTL) get nodereadinessrules 2>/dev/null || echo "  None"
	@echo "KWOK Nodes:"
	@$(KUBECTL) get nodes -l kwok.x-k8s.io/node=fake --no-headers 2>/dev/null | wc -l | xargs echo "  Count:" || echo "  0"
	@echo ""
	@echo "==> Port Forwarding"
	@pgrep -f "port-forward.*grafana" > /dev/null && echo "✓ Grafana port-forward active (3000)" || echo "✗ Grafana port-forward not active"
	@pgrep -f "port-forward.*prometheus" > /dev/null && echo "✓ Prometheus port-forward active (9090)" || echo "✗ Prometheus port-forward not active"

.PHONY: verify
verify: ## Verify all prerequisites are installed
	@echo "==> Verifying Prerequisites"
	@command -v $(KIND) >/dev/null 2>&1 && echo "✓ kind installed" || echo "✗ kind not found"
	@command -v $(KUBECTL) >/dev/null 2>&1 && echo "✓ kubectl installed" || echo "✗ kubectl not found"
	@command -v $(HELM) >/dev/null 2>&1 && echo "✓ helm installed" || echo "✗ helm not found"
	@command -v $(CONTAINER_TOOL) >/dev/null 2>&1 && echo "✓ $(CONTAINER_TOOL) installed" || echo "✗ $(CONTAINER_TOOL) not found"
	@command -v jq >/dev/null 2>&1 && echo "✓ jq installed" || echo "✗ jq not found"
	@command -v bc >/dev/null 2>&1 && echo "✓ bc installed" || echo "✗ bc not found"

.PHONY: info
info: ## Show configuration information
	@echo "==> Configuration"
	@echo "Project Root:      $(PROJECT_ROOT)"
	@echo "Scale Directory:   $(SCALE_DIR)"
	@echo "Cluster Name:      $(CLUSTER_NAME)"
	@echo "Container Tool:    $(CONTAINER_TOOL)"
	@echo "Image:             $(IMG_PREFIX):$(IMG_TAG)"
	@echo "Kind Config:       $(KIND_CONFIG)"
	@echo "Controller NS:     $(CONTROLLER_NAMESPACE)"

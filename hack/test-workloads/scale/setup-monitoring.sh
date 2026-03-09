#!/usr/bin/env bash

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

# Complete setup script for NRR scale testing with Prometheus and Grafana
# This script:
# 1. Creates a Kind cluster
# 2. Installs NRR using Podman
# 3. Installs Prometheus stack
# 4. Creates ServiceMonitor
# 5. Sets up port forwarding
# 6. Provides instructions for Grafana dashboard import

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
CLUSTER_NAME="${CLUSTER_NAME:-nrr-test}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

echo -e "${BLUE}=========================================${NC}"
echo -e "${BLUE}NRR Scale Test Setup with Monitoring${NC}"
echo -e "${BLUE}=========================================${NC}"
echo ""

# Function to print step headers
print_step() {
    echo ""
    echo -e "${GREEN}==> $1${NC}"
}

# Function to print warnings
print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

# Function to print errors
print_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Function to print success
print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

# Check prerequisites
print_step "Step 1: Checking prerequisites..."

MISSING_TOOLS=()

if ! command -v kind &> /dev/null; then
    MISSING_TOOLS+=("kind")
fi

if ! command -v kubectl &> /dev/null; then
    MISSING_TOOLS+=("kubectl")
fi

if ! command -v helm &> /dev/null; then
    MISSING_TOOLS+=("helm")
fi

if ! command -v podman &> /dev/null; then
    MISSING_TOOLS+=("podman")
fi

if [ ${#MISSING_TOOLS[@]} -ne 0 ]; then
    print_error "Missing required tools: ${MISSING_TOOLS[*]}"
    echo ""
    echo "Please install:"
    for tool in "${MISSING_TOOLS[@]}"; do
        echo "  - $tool"
    done
    exit 1
fi

print_success "All prerequisites installed"

# Create Kind cluster and install NRR
print_step "Step 2: Creating Kind cluster and installing NRR with Podman..."
cd "$PROJECT_ROOT"

if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
    print_warning "Cluster '$CLUSTER_NAME' already exists. Deleting..."
    kind delete cluster --name "$CLUSTER_NAME"
fi

# Run the podman-kind-test target
print_success "Running: make podman-kind-test"
if ! make podman-kind-test KIND_CLUSTER="$CLUSTER_NAME"; then
    print_error "Failed to create cluster and install NRR"
    exit 1
fi

print_success "NRR installed successfully"

# Add Prometheus Helm repo
print_step "Step 3: Setting up Prometheus stack..."

print_success "Adding Prometheus Helm repository..."
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Install Prometheus stack
print_success "Installing kube-prometheus-stack..."
helm upgrade --install prom-stack prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace \
  --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
  --set prometheus.prometheusSpec.scrapeInterval="5s" \
  --set nodeExporter.enabled=false \
  --wait \
  --timeout 5m

print_success "Prometheus stack installed"

# Wait for Prometheus pods to be ready
print_success "Waiting for Prometheus pods to be ready..."
kubectl wait --for=condition=ready pod \
  -l app.kubernetes.io/name=prometheus \
  -n monitoring \
  --timeout=300s

kubectl wait --for=condition=ready pod \
  -l app.kubernetes.io/name=grafana \
  -n monitoring \
  --timeout=300s

print_success "Prometheus and Grafana are ready"

# Create ServiceMonitor
print_step "Step 4: Creating ServiceMonitor for NRR metrics..."

if [ ! -f "$SCRIPT_DIR/servicemonitor.yaml" ]; then
    print_error "ServiceMonitor file not found: $SCRIPT_DIR/servicemonitor.yaml"
    exit 1
fi

kubectl apply -f "$SCRIPT_DIR/servicemonitor.yaml"
print_success "ServiceMonitor created"

# Get Grafana admin password
print_step "Step 5: Retrieving Grafana credentials..."
GRAFANA_PASSWORD=$(kubectl get secret --namespace monitoring prom-stack-grafana -o jsonpath="{.data.admin-password}" | base64 --decode)

print_success "Grafana admin password retrieved"

# Setup port forwarding
print_step "Step 6: Setting up port forwarding..."

# Kill any existing port forwards
pkill -f "port-forward.*grafana" 2>/dev/null || true
pkill -f "port-forward.*prometheus" 2>/dev/null || true

# Start port forwarding in background
kubectl port-forward -n monitoring svc/prom-stack-grafana 3000:80 > /dev/null 2>&1 &
GRAFANA_PF_PID=$!

kubectl port-forward -n monitoring svc/prom-stack-kube-prometheus-prometheus 9090:9090 > /dev/null 2>&1 &
PROMETHEUS_PF_PID=$!

# Wait for port forwards to be ready
sleep 3

print_success "Port forwarding established"

# Print final instructions
echo ""
echo -e "${BLUE}=========================================${NC}"
echo -e "${BLUE}Setup Complete!${NC}"
echo -e "${BLUE}=========================================${NC}"
echo ""
echo -e "${GREEN}📊 Access URLs:${NC}"
echo -e "  Grafana:    ${BLUE}http://localhost:3000${NC}"
echo -e "  Prometheus: ${BLUE}http://localhost:9090${NC}"
echo ""
echo -e "${GREEN}🔐 Grafana Credentials:${NC}"
echo -e "  Username: ${BLUE}admin${NC}"
echo -e "  Password: ${BLUE}${GRAFANA_PASSWORD}${NC}"
echo ""
echo -e "${GREEN}📈 Import Dashboard:${NC}"
echo "  1. Open Grafana: http://localhost:3000"
echo "  2. Login with credentials above"
echo "  3. Go to: Dashboards → Import"
echo "  4. Click 'Upload JSON file'"
echo "  5. Select: $SCRIPT_DIR/graphana-dashboard.json"
echo "  6. Select Prometheus datasource"
echo "  7. Click 'Import'"
echo ""
echo -e "${GREEN}🚀 Run Scale Test:${NC}"
echo "  cd $PROJECT_ROOT"
echo "  ./scale/new-script.sh 1000"
echo ""
echo -e "${GREEN}🧹 Cleanup:${NC}"
echo "  ./hack/test-workloads/scale/cleanup-kwok-nodes-rules.sh"
echo "  kind delete cluster --name $CLUSTER_NAME"
echo ""
echo -e "${YELLOW}⚠️  Port forwarding is running in background${NC}"
echo -e "${YELLOW}   PIDs: Grafana=$GRAFANA_PF_PID, Prometheus=$PROMETHEUS_PF_PID${NC}"
echo -e "${YELLOW}   To stop: kill $GRAFANA_PF_PID $PROMETHEUS_PF_PID${NC}"
echo ""
echo -e "${GREEN}Press Ctrl+C to stop port forwarding and exit${NC}"

# Keep script running to maintain port forwards
trap "echo ''; echo 'Stopping port forwarding...'; kill $GRAFANA_PF_PID $PROMETHEUS_PF_PID 2>/dev/null; exit 0" INT TERM

# Wait for port forward processes
wait $GRAFANA_PF_PID $PROMETHEUS_PF_PID

# Made with Bob

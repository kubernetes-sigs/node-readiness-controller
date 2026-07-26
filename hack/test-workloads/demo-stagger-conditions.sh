#!/bin/bash

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
#
# Sets up the demo environment used to validate the Grafana dashboard.
#
# This script:
#   - Labels the demo nodes.
#   - Applies the demo NodeReadinessRules.
#   - Sets the initial node conditions.
#   - Reproduces the scenarios used by the dashboard.
#
# Demo scenarios:
#   - GPU Driver Readiness: intentionally matches no nodes.
#   - CSI Registration: nodes become ready over time, with two nodes intentionally remaining blocked.
#   - Security Agent Check-in: one node briefly becomes unhealthy before recovering.
#
# Usage:
#   ./demo-stagger-conditions.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if ! command -v kubectl &> /dev/null; then
    echo "Error: kubectl command not found. Please install it."
    exit 1
fi

CSI_NODES=(kwok-stuck-0 kwok-stuck-1 kwok-stuck-2 kwok-stuck-3 kwok-stuck-4 kwok-stuck-5 kwok-fail-test kwok-fail-test-2)
SECURITY_AGENT_NODES=(kwok-flap-0 kwok-flap-1 kwok-flap-2 kwok-flap-3 kwok-flap-4 kwok-flap-5 kwok-unmanaged-0 kwok-unmanaged-1 kwok-unmanaged-2 kwok-unmanaged-3 kwok-unmanaged-4 kwok-unmanaged-5)

echo "==> Labeling demo nodes..."
kubectl label node "${CSI_NODES[@]}" nrc-demo=csi-registration --overwrite
kubectl label node "${SECURITY_AGENT_NODES[@]}" nrc-demo=security-agent --overwrite

echo "==> Applying demo rules..."
kubectl apply -f "$SCRIPT_DIR/demo-gpu-driver-readiness-rule.yaml"
kubectl apply -f "$SCRIPT_DIR/demo-csi-registration-rule.yaml"
kubectl apply -f "$SCRIPT_DIR/demo-security-agent-checkin-rule.yaml"

echo "==> Setting initial node conditions..."
for n in "${CSI_NODES[@]}"; do
    "$SCRIPT_DIR/flip-node-condition.sh" "$n" "storage.k8s.io/CSIRegistered" false
done
for n in "${SECURITY_AGENT_NODES[@]}"; do
    "$SCRIPT_DIR/flip-node-condition.sh" "$n" "security.k8s.io/AgentHealthy" true
done

echo "==> Simulating CSI registration..."
"$SCRIPT_DIR/flip-node-condition.sh" kwok-fail-test "storage.k8s.io/CSIRegistered" true
sleep 3
"$SCRIPT_DIR/flip-node-condition.sh" kwok-stuck-0 "storage.k8s.io/CSIRegistered" true
sleep 5
"$SCRIPT_DIR/flip-node-condition.sh" kwok-fail-test-2 "storage.k8s.io/CSIRegistered" true
sleep 8
"$SCRIPT_DIR/flip-node-condition.sh" kwok-stuck-1 "storage.k8s.io/CSIRegistered" true
sleep 13
"$SCRIPT_DIR/flip-node-condition.sh" kwok-stuck-2 "storage.k8s.io/CSIRegistered" true
sleep 21
"$SCRIPT_DIR/flip-node-condition.sh" kwok-stuck-3 "storage.k8s.io/CSIRegistered" true

echo "==> Simulating a temporary security agent failure..."
"$SCRIPT_DIR/flip-node-condition.sh" kwok-flap-2 "security.k8s.io/AgentHealthy" false
sleep 5
"$SCRIPT_DIR/flip-node-condition.sh" kwok-flap-2 "security.k8s.io/AgentHealthy" true

echo ""
echo "Demo environment is ready."
echo ""
echo "Expected state:"
echo "  • CSI Registration: 6 ready, 2 blocked"
echo "  • Security Agent: 12 ready"
echo "  • GPU Driver: 0 matched nodes"
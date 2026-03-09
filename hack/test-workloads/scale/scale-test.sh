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

set -e

################################################################################
# NRR SCALE TEST UTILITY - HELP GUIDE
################################################################################
# USAGE:
#   ./scale-test.sh <NODE_COUNT> <RULE_COUNT>
#
# ARGUMENTS:
#   NODE_COUNT : Total fake KWOK nodes to create (Default: 10)
#   RULE_COUNT : Total NodeReadinessRules to evaluate per node (Default: 1)
#
# DESCRIPTION:
#   1. Cleans up previous test artifacts (nodes and rules).
#   2. Creates N rules with unique conditions and taints.
#   3. Spawns M KWOK nodes in parallel batches.
#   4. Measures 'Taint Addition' latency (Controller reacting to new nodes).
#   5. Patches all nodes to satisfy all rule conditions.
#   6. Measures 'Taint Removal' latency (Controller finalizing readiness).
#   7. Outputs Etcd footprint and performance metrics for PPT Slide 4.
################################################################################

# Input Parameters
NODE_COUNT=${1:-10}
RULE_COUNT=${2:-1} 
BATCH_SIZE=50  
BASE_RULE_NAME="kwok-network-rule"

# Validate input
if ! [[ "$NODE_COUNT" =~ ^[0-9]+$ ]] || ! [[ "$RULE_COUNT" =~ ^[0-9]+$ ]]; then
  echo "Error: Please provide valid positive numbers for node and rule counts"
  echo "Example: ./scale-test.sh 1000 3"
  exit 1
fi

echo "🚀 Starting Scale Test: $NODE_COUNT Nodes | $RULE_COUNT Rules"
echo "----------------------------------------------------------"

# Step 0: Cleanup
echo "Step 0: Cleaning up existing resources..."
kubectl delete nodereadinessrules -l scale-test=true --ignore-not-found=true
kubectl delete nodes -l kwok.x-k8s.io/node=fake --ignore-not-found=true
sleep 2

# Step 1: Create Multiple Rules
echo "Step 1: Creating $RULE_COUNT rules..."
for r in $(seq 1 $RULE_COUNT); do
  cat <<EOF | kubectl apply -f -
apiVersion: readiness.node.x-k8s.io/v1alpha1
kind: NodeReadinessRule
metadata:
  name: ${BASE_RULE_NAME}-$r
  labels: { scale-test: "true" }
spec:
  nodeSelector: { matchLabels: { kwok.x-k8s.io/node: fake } }
  conditions:
    - type: "network.kubernetes.io/CNIReady-$r"
      requiredStatus: "True"
  taint:
    key: "readiness.k8s.io/network-unready-$r"
    value: "true"
    effect: NoSchedule
  enforcementMode: "bootstrap-only"
EOF
done

# Step 2: Create Nodes
echo "Step 2: Spawning $NODE_COUNT nodes in parallel..."
TAINT_START_TIME=$(date +%s); TAINT_START_NANOS=$(date +%N)

create_node() {
  cat <<EOF | kubectl apply -f - 2>/dev/null
apiVersion: v1
kind: Node
metadata:
  name: kwok-node-$1
  labels: { kwok.x-k8s.io/node: fake }
spec:
  taints: [{key: "kwok.x-k8s.io/node", value: "fake", effect: "NoSchedule"}]
status:
  allocatable: {cpu: "32", memory: "256Gi", pods: "110"}
  capacity: {cpu: "32", memory: "256Gi", pods: "110"}
  conditions: [{type: "Ready", status: "True", reason: "KubeletReady", message: "ready", lastHeartbeatTime: "$(date -u +"%Y-%m-%dT%H:%M:%SZ")", lastTransitionTime: "$(date -u +"%Y-%m-%dT%H:%M:%SZ")"}]
EOF
}

for batch_start in $(seq 1 $BATCH_SIZE $NODE_COUNT); do
  batch_end=$((batch_start + BATCH_SIZE - 1))
  [ $batch_end -gt $NODE_COUNT ] && batch_end=$NODE_COUNT
  for i in $(seq $batch_start $batch_end); do create_node $i & done
  wait
done

# Step 3: Wait for ALL taints
echo "Step 3: Waiting for Controller to add $((NODE_COUNT * RULE_COUNT)) total taints..."
while true; do
  TOTAL_TAINTS=$(kubectl get nodes -l kwok.x-k8s.io/node=fake -o json | jq "[.items[].spec.taints // [] | .[] | select(.key | startswith(\"readiness.k8s.io/network-unready\"))] | length")
  [ "$TOTAL_TAINTS" -eq $((NODE_COUNT * RULE_COUNT)) ] && break
  echo -n "[$TOTAL_TAINTS]" && sleep 1
done
TAINT_END_TIME=$(date +%s); TAINT_END_NANOS=$(date +%N)

# Step 4: Patch Conditions
echo -e "\nStep 4: Satisfying conditions for all rules..."
UNTAINT_START_TIME=$(date +%s); UNTAINT_START_NANOS=$(date +%N)

patch_node_conditions() {
  PATCH_JSON="["
  for r in $(seq 1 $RULE_COUNT); do
    PATCH_JSON+="{\"op\":\"add\",\"path\":\"/status/conditions/-\",\"value\":{\"type\":\"network.kubernetes.io/CNIReady-$r\",\"status\":\"True\",\"lastHeartbeatTime\":\"$(date -u +"%Y-%m-%dT%H:%M:%SZ")\",\"lastTransitionTime\":\"$(date -u +"%Y-%m-%dT%H:%M:%SZ")\",\"reason\":\"CNIReady\",\"message\":\"ready\"}}"
    [ $r -lt $RULE_COUNT ] && PATCH_JSON+=","
  done
  PATCH_JSON+="]"
  kubectl patch node kwok-node-$1 --subresource=status --type=json -p="$PATCH_JSON" > /dev/null 2>&1
}

for batch_start in $(seq 1 $BATCH_SIZE $NODE_COUNT); do
  batch_end=$((batch_start + BATCH_SIZE - 1))
  [ $batch_end -gt $NODE_COUNT ] && batch_end=$NODE_COUNT
  for i in $(seq $batch_start $batch_end); do patch_node_conditions $i & done
  wait
done

# Step 5: Wait for Removal
echo "Step 5: Waiting for Taint removal..."
while true; do
  REMAINING=$(kubectl get nodes -l kwok.x-k8s.io/node=fake -o json | jq "[.items[].spec.taints // [] | .[] | select(.key | startswith(\"readiness.k8s.io/network-unready\"))] | length")
  [ "$REMAINING" -eq 0 ] && break
  echo -n "[$REMAINING]" && sleep 1
done
UNTAINT_END_TIME=$(date +%s); UNTAINT_END_NANOS=$(date +%N)

# Step 6: Final Stats
TAINT_MS=$(echo "scale=0; (($TAINT_END_TIME - $TAINT_START_TIME) * 1000) + (($TAINT_END_NANOS - $TAINT_START_NANOS) / 1000000)" | bc)
UNTAINT_MS=$(echo "scale=0; (($UNTAINT_END_TIME - $UNTAINT_START_TIME) * 1000) + (($UNTAINT_END_NANOS - $UNTAINT_START_NANOS) / 1000000)" | bc)
AVG_SIZE=$(kubectl get nodereadinessrules -l scale-test=true -o json | jq '[.items[] | tostring | length] | add / length')

echo -e "\n\n╔════════════════════════════════════════════════════════════════╗"
echo "║                MULTI-RULE PERFORMANCE SUMMARY                  ║"
echo "╠════════════════════════════════════════════════════════════════╣"
printf "║ Total Nodes:           %-40s║\n" "$NODE_COUNT"
printf "║ Active Rules:          %-40s║\n" "$RULE_COUNT"
printf "║ Taint Add Time:        %-40s║\n" "${TAINT_MS} ms"
printf "║ Taint Remove Time:     %-40s║\n" "${UNTAINT_MS} ms"
echo "║                                                                ║"
printf "║ Avg Rule Size (Etcd):  %-40s║\n" "${AVG_SIZE%.*} bytes"
echo "╚════════════════════════════════════════════════════════════════╝"

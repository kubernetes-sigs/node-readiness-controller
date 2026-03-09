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

# Cleanup script for KWOK nodes and NodeReadinessRules

set -euo pipefail

echo "=== Cleanup Script ==="
echo ""

# Delete all KWOK nodes
echo "🧹 Deleting all KWOK nodes..."
NODE_COUNT=$(kubectl get nodes -l kwok.x-k8s.io/node=fake --no-headers 2>/dev/null | wc -l | tr -d ' ')

if [ "$NODE_COUNT" -eq 0 ]; then
  echo "   No KWOK nodes found."
else
  echo "   Found $NODE_COUNT KWOK nodes. Deleting..."
  kubectl delete nodes -l kwok.x-k8s.io/node=fake --grace-period=0 --force
  echo "   ✓ All KWOK nodes deleted"
fi

echo ""

# Delete all NodeReadinessRules
echo "🧹 Deleting all NodeReadinessRules..."
NRR_COUNT=$(kubectl get nodereadinessrules --no-headers 2>/dev/null | wc -l | tr -d ' ')

if [ "$NRR_COUNT" -eq 0 ]; then
  echo "   No NodeReadinessRules found."
else
  echo "   Found $NRR_COUNT NodeReadinessRule(s). Deleting..."
  kubectl delete nodereadinessrules --all
  echo "   ✓ All NodeReadinessRules deleted"
fi

echo ""
echo "=== Cleanup Complete ==="

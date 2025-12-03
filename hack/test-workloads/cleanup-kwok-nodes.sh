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

# This script finds and deletes all nodes managed by kwok,
# identified by the 'type=kwok' label. It is useful for cleaning
# up the cluster environment between test runs.
#
set -euo pipefail

# Check for required commands
if ! command -v kubectl &> /dev/null; then
    echo "Error: kubectl command not found. Please install it."
    exit 1
fi

echo "Finding kwok nodes to delete..."
KWOK_NODES=$(kubectl get nodes -l type=kwok -o jsonpath='{.items[*].metadata.name}')

if [ -z "$KWOK_NODES" ]; then
    echo "No kwok nodes found to delete. Exiting."
    exit 0
fi

echo "The following kwok nodes will be deleted:"
echo "$KWOK_NODES" | tr ' ' '\n'
echo ""

# Ask for confirmation
read -p "Are you sure you want to delete these nodes? (y/n) " -n 1 -r
echo ""
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Cleanup cancelled."
    exit 1
fi

echo "Deleting kwok nodes..."
# The --ignore-not-found flag prevents errors if a node is deleted between the get and delete operations
kubectl delete node $KWOK_NODES --ignore-not-found

echo "Cleanup complete."

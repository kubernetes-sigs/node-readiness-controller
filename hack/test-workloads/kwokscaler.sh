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

# This script scales the number of kwok worker nodes in the cluster.
# It uses a template YAML file to create and apply node configurations.
#
set -euo pipefail

# Check for required commands
if ! command -v kubectl &> /dev/null; then
    echo "kubectl command not found, please install kubectl to use this script."
    exit 1
fi

# Check input parameters
if [ $# -lt 1 ]; then
    echo "Usage: $0 <count>"
    echo "count must be a positive integer"
    exit 1
fi

COUNT=$1
TEMPLATE_FILE="kwok-template.yaml"

# Validate count
if ! [[ "$COUNT" =~ ^[0-9]+$ ]] || [ "$COUNT" -le 0 ]; then
    echo "Count must be a positive integer"
    exit 1
fi

# Check if template file exists
if [ ! -f "$TEMPLATE_FILE" ]; then
    echo "Template file '$TEMPLATE_FILE' not found."
    exit 1
fi

# Get existing kwok nodes and determine the highest node index
highest_index=-1
existing_nodes=$(kubectl get nodes -l type=kwok --no-headers -o custom-columns=NAME:.metadata.name 2>/dev/null || true)
for node in $existing_nodes; do
    if [[ $node == "kwok-node-"* ]]; then
        index="${node#kwok-node-}"
        if [[ "$index" =~ ^[0-9]+$ ]] && [ "$index" -gt "$highest_index" ]; then
            highest_index=$index
        fi
    fi
done

# Add nodes based on the highest found index and the count specified
start_index=$(($highest_index + 1))
end_index=$(($highest_index + $COUNT))

echo "Starting to scale kwok nodes from index $start_index to $end_index..."

for i in $(seq $start_index $end_index); do
    NEW_NODE_NAME="kwok-node-$i"
    echo "Creating node $NEW_NODE_NAME..."

    # Replace the node name and hostname in the template and apply it
    sed -e "s/name: kwok-node-0/name: $NEW_NODE_NAME/" \
        -e "s/kubernetes.io\/hostname: kwok-node-0/kubernetes.io\/hostname: $NEW_NODE_NAME/" \
        "$TEMPLATE_FILE" | kubectl apply -f -
done

echo "Successfully created $COUNT new kwok node(s)."
echo "Total kwok nodes in cluster:"
kubectl get nodes -l type=kwok

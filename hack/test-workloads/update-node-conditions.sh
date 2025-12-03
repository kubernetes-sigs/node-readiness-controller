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

# This script updates the status conditions for all kwok nodes in the cluster.
# It takes a condition definition from a YAML file and applies it to each node,
# with an optional random delay to simulate real-world scenarios.
#
# Prerequisites:
# - kubectl: Must be installed and configured.
# - yq: Must be installed for parsing YAML files (https://github.com/mikefarah/yq).
#
# Usage:
#   ./update-node-conditions.sh -f <condition-file.yaml> [--random-interval <max-seconds>]
#
# Example:
#   # Update all kwok nodes with the condition defined in condition-calico-ready.yaml
#   ./update-node-conditions.sh -f condition-calico-ready.yaml
#
#   # Update nodes with a random delay of up to 5 seconds between each update
#   ./update-node-conditions.sh -f condition-calico-ready.yaml --random-interval 5
#
set -euo pipefail

# --- Configuration ---
CONDITION_FILE=""
MAX_INTERVAL=0

# --- Helper Functions ---
function usage() {
    echo "Usage: $0 -f <condition-file.yaml> [--random-interval <max-seconds>]"
    echo "  -f <file>         : Path to the YAML file containing the condition definition."
    echo "  --random-interval <seconds> : Optional. Max random delay in seconds between node updates."
    exit 1
}

function check_deps() {
    if ! command -v kubectl &> /dev/null; then
        echo "Error: kubectl command not found. Please install it."
        exit 1
    fi
    if ! command -v yq &> /dev/null; then
        echo "Error: yq command not found. Please install it to parse the condition file."
        exit 1
    fi
}

# --- Argument Parsing ---
while [[ $# -gt 0 ]]; do
    key="$1"
    case $key in
        -f)
        CONDITION_FILE="$2"
        shift; shift
        ;;
        --random-interval)
        MAX_INTERVAL="$2"
        shift; shift
        ;;
        *)
        usage
        ;;
    esac
done

if [ -z "$CONDITION_FILE" ]; then
    echo "Error: Condition file must be specified with -f."
    usage
fi

if [ ! -f "$CONDITION_FILE" ]; then
    echo "Error: Condition file not found at '$CONDITION_FILE'"
    exit 1
fi

# --- Main Logic ---
check_deps

# Read condition details from the YAML file
CONDITION_TYPE=$(yq -r '.type' "$CONDITION_FILE")
CONDITION_STATUS=$(yq -r '.status' "$CONDITION_FILE")
CONDITION_REASON=$(yq -r '.reason' "$CONDITION_FILE")
CONDITION_MESSAGE=$(yq -r '.message' "$CONDITION_FILE")

if [ "$CONDITION_TYPE" == "null" ] || [ "$CONDITION_STATUS" == "null" ]; then
    echo "Error: The condition file must contain at least 'type' and 'status' fields."
    exit 1
fi

echo "Fetching kwok nodes..."
KWOK_NODES=$(kubectl get nodes -l type=kwok -o jsonpath='{.items[*].metadata.name}')

if [ -z "$KWOK_NODES" ]; then
    echo "No kwok nodes found. Exiting."
    exit 0
fi

echo "Found kwok nodes: $KWOK_NODES"
echo "Preparing to apply condition: Type=$CONDITION_TYPE, Status=$CONDITION_STATUS"
echo "---"

for node in $KWOK_NODES; do
    if [ "$MAX_INTERVAL" -gt 0 ]; then
        sleep_duration=$((RANDOM % MAX_INTERVAL))
        echo "Sleeping for ${sleep_duration}s before updating $node..."
        sleep $sleep_duration
    fi

    echo "Updating condition for node: $node"

    # Get current timestamp in RFC3339 format
    TIMESTAMP=$(date -u +'%Y-%m-%dT%H:%M:%SZ')

    # Construct the strategic merge patch
    PATCH=$(cat <<EOF
{
    "status": {
        "conditions": [
            {
                "type": "$CONDITION_TYPE",
                "status": "$CONDITION_STATUS",
                "reason": "$CONDITION_REASON",
                "message": "$CONDITION_MESSAGE",
                "lastHeartbeatTime": "$TIMESTAMP",
                "lastTransitionTime": "$TIMESTAMP"
            }
        ]
    }
}
EOF
)

    # Apply the patch to the node's status subresource
    kubectl patch node "$node" --subresource status --type strategic --patch "$PATCH"
done

echo "---"
echo "Successfully updated conditions for all kwok nodes."

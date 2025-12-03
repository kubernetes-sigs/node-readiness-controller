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

# This script flips a specific node's readiness condition to the opposite
# of its current state (True -> False, False -> True), or sets it to a
# specific state, to test the reaction of the node-readiness-controller.
#
# Usage:
#   ./flip-node-condition.sh <node-name> [condition-type] [true|false]
#
# Arguments:
#   <node-name>:      The name of the node to update (e.g., kwok-node-0).
#   [condition-type]: Optional. The condition type to flip.
#                     Defaults to "network.k8s.io/CalicoReady".
#   [true|false]:     Optional. Explicitly set the status. If omitted,
#                     the script will automatically flip the current status.
#
# Examples:
#   # Automatically flip the CalicoReady condition for kwok-node-5
#   ./flip-node-condition.sh kwok-node-5
#
#   # Explicitly set the CalicoReady condition to True
#   ./flip-node-condition.sh kwok-node-5 network.k8s.io/CalicoReady true
#
set -euo pipefail

# --- Configuration ---
NODE_NAME=""
CONDITION_TYPE="network.k8s.io/CalicoReady"
TARGET_STATUS=""

# --- Argument Parsing ---
if [ "$#" -lt 1 ] || [ "$#" -gt 3 ]; then
    echo "Error: Invalid number of arguments."
    echo "Usage: $0 <node-name> [condition-type] [true|false]"
    exit 1
fi
NODE_NAME=$1
if [ "$#" -ge 2 ]; then
    CONDITION_TYPE=$2
fi
if [ "$#" -eq 3 ]; then
    if [[ "$3" != "true" && "$3" != "false" ]]; then
        echo "Error: Invalid status argument. Must be 'true' or 'false'."
        exit 1
    fi
    TARGET_STATUS=$(echo "$3" | awk '{print toupper(substr($0,1,1))tolower(substr($0,2))}')
fi

# --- Main Logic ---
if ! command -v kubectl &> /dev/null; then
    echo "Error: kubectl command not found. Please install it."
    exit 1
fi

echo "Checking if node '$NODE_NAME' exists..."
if ! kubectl get node "$NODE_NAME" > /dev/null; then
    echo "Error: Node '$NODE_NAME' not found."
    exit 1
fi

# If TARGET_STATUS is not set, we need to determine the current status and flip it
if [ -z "$TARGET_STATUS" ]; then
    echo "Determining current status of '$CONDITION_TYPE' on node '$NODE_NAME'..."
    CURRENT_STATUS=$(kubectl get node "$NODE_NAME" -o jsonpath="{.status.conditions[?(@.type==\"$CONDITION_TYPE\")].status}")

    if [ -z "$CURRENT_STATUS" ]; then
        echo "Warning: Condition '$CONDITION_TYPE' not found on node. Defaulting to setting it to 'True'."
        TARGET_STATUS="True"
    elif [ "$CURRENT_STATUS" == "True" ]; then
        echo "Current status is 'True'. Flipping to 'False'."
        TARGET_STATUS="False"
    else
        echo "Current status is 'False' or 'Unknown'. Flipping to 'True'."
        TARGET_STATUS="True"
    fi
else
    echo "Explicitly setting condition '$CONDITION_TYPE' for node '$NODE_NAME' to $TARGET_STATUS..."
fi


# Get current timestamp in RFC3339 format
TIMESTAMP=$(date -u +'%Y-%m-%dT%H:%M:%SZ')
REASON="ConditionFlippedForTest"
MESSAGE="Condition manually set to $TARGET_STATUS for testing purposes"

# Construct the strategic merge patch
PATCH=$(cat <<EOF
{
    "status": {
        "conditions": [
            {
                "type": "$CONDITION_TYPE",
                "status": "$TARGET_STATUS",
                "reason": "$REASON",
                "message": "$MESSAGE",
                "lastHeartbeatTime": "$TIMESTAMP",
                "lastTransitionTime": "$TIMESTAMP"
            }
        ]
    }
}
EOF
)

# Apply the patch to the node's status subresource
kubectl patch node "$NODE_NAME" --subresource status --type strategic --patch "$PATCH"

echo "---"
echo "Successfully updated condition for node '$NODE_NAME'."
echo "You can now check the node's taints and the controller's logs."

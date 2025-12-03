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

# This script is a simplified version of kindscaler, originally from:
# https://github.com/lobuhi/kindscaler/
#
# It is modified to specifically support the testing needs of the
# node-readiness-controller project by only scaling worker nodes
# and using a specific node as a template.
#
set -euxo pipefail

# Check for required commands
if ! command -v kind &> /dev/null; then
    echo "kind command not found, please install kind to use this script."
    exit 1
fi

# Check input parameters
if [ $# -lt 2 ]; then
    echo "Usage: $0 <cluster-name> <count>"
    echo "count must be a positive integer"
    exit 1
fi

CLUSTER_NAME=$1
COUNT=$2
ROLE="worker" # Hardcoded for our testing purposes

# Validate count
if ! [[ "$COUNT" =~ ^[0-9]+$ ]] || [ "$COUNT" -le 0 ]; then
    echo "Count must be a positive integer"
    exit 1
fi

# Get existing nodes and determine the highest node index for the given role
highest_index=0
existing_nodes=$(kind get nodes --name "$CLUSTER_NAME")
for node in $existing_nodes; do
    if [[ $node == "$CLUSTER_NAME-$ROLE"* ]]; then
        suffix=$(echo $node | sed -e "s/^$CLUSTER_NAME-$ROLE//")
        if [[ "$suffix" =~ ^[0-9]+$ ]] && [ "$suffix" -gt "$highest_index" ]; then
            highest_index=$suffix
        fi
    fi
done

# Add nodes based on the highest found index and the count specified
start_index=$(($highest_index + 1))
end_index=$(($highest_index + $COUNT))
for i in $(seq $start_index $end_index); do
    # Use nrg-test-worker2 as the template for all new worker nodes.
    # This ensures they get the correct labels and taints.
    TEMPLATE_NODE_NAME="$CLUSTER_NAME-worker2"
    NEW_NODE_NAME="$CLUSTER_NAME-worker$i"

    # Copy the kubeadm file from the template node
    docker cp $TEMPLATE_NODE_NAME:/kind/kubeadm.conf kubeadm-$i.conf > /dev/null 2>&1

    # Replace the container role name with specific node name in the kubeadm file
    sed -i "s/$TEMPLATE_NODE_NAME$/$NEW_NODE_NAME/g" "./kubeadm-$i.conf"

    IMAGE=$(docker ps | grep $CLUSTER_NAME | awk '{print $2}' | head -1)
	
	echo -n "Adding $NEW_NODE_NAME node to $CLUSTER_NAME cluster... "
    docker run --name $NEW_NODE_NAME --hostname $NEW_NODE_NAME \
    --label io.x-k8s.kind.role=$ROLE --privileged \
    --security-opt seccomp=unconfined --security-opt apparmor=unconfined \
    --tmpfs /tmp --tmpfs /run --volume /var \
    --volume /lib/modules:/lib/modules:ro -e KIND_EXPERIMENTAL_CONTAINERD_SNAPSHOTTER \
    --detach --tty --label io.x-k8s.kind.cluster=$CLUSTER_NAME --net kind \
    --restart=on-failure:1 --init=false $IMAGE > /dev/null 2>&1

    docker cp kubeadm-$i.conf $NEW_NODE_NAME:/kind/kubeadm.conf > /dev/null 2>&1
    docker exec --privileged $NEW_NODE_NAME kubeadm join --config /kind/kubeadm.conf --skip-phases=preflight --v=6 > /dev/null 2>&1
    rm -f kubeadm-*.conf
    echo "Done!"
done

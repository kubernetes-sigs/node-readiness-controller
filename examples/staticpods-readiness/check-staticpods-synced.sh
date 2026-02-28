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


# This script will check that all static pods are mirrored in the
# API server.

# Exit code constants
readonly OK=0
readonly NONOK=1
readonly UNKOWN=2

function usage {
    echo ""
    echo "Usage: $0 --staticPodsDir"
    echo ""
    echo "Check if all static pods are mirrored in the Api server"
    echo ""
    echo "options:"
    echo -e "\t--staticPodsDir\t Path of static pods manifests (Default: /etc/kubernetes/manifests)"
    exit $UNKOWN
}

function die {
    echo $1
    exit $2
}

STATIC_PODS_DIR="/etc/kubernetes/manifests"
NODE_NAME=$(hostname)

while [[ $# -gt 0 ]]; do
    case $1 in
        --staticPodsDir) STATIC_PODS_DIR=$2; shift ;;
        -h|--help) usage ;;
        *) die "Unknown option: $1" $UNKNOWN ;;
    esac
done



YQ_VERSION="v4.52.4"
YQ_PATH="/opt/yq"

JQ_VERSION="jq-1.8.1"
JQ_PATH="/opt/jq"


if [ ! -f "$YQ_PATH" ]; then 

    echo "yq not found at $YQ_PATH, downloading..."
    OS=$(uname -s | tr [:upper:] [:lower:])
    ARCH=$(uname -m)

    case $ARCH in 
        x86_64)
            ARCH="amd64"
            ;;

        arm|aarch64)
            ARCH="arm64"
            ;;

        # This script is for demonstration purposes , YQ supports 
        # the majority of archs , add case statements as desired.
        *) 
            echo "Unsupported Arch: $ARCH"
            exit $UNKOWN
    esac

    YQ_BINARY="yq_${OS}_${ARCH}"

    curl -sL  "https://github.com/mikefarah/yq/releases/download/$YQ_VERSION/$YQ_BINARY" -o "$YQ_PATH"
    chmod +x "$YQ_PATH"

fi



if [ ! -f "$JQ_PATH" ]; then 

    echo "jq not found at $JQ_PATH, downloading..."
    OS=$(uname -s | tr [:upper:] [:lower:])
    ARCH=$(uname -m)

    case $ARCH in 
        x86_64)
            ARCH="amd64"
            ;;

        arm|aarch64)
            ARCH="arm64"
            ;;

        # This script is for demonstration purposes , YQ supports 
        # the majority of archs , add case statements as desired.
        *) 
            echo "Unsupported Arch: $ARCH"
            exit $UNKOWN
    esac

    JQ_BINARY="jq-${OS}-${ARCH}"

    curl -sL  "https://github.com/jqlang/jq/releases/download/$JQ_VERSION/$JQ_BINARY" -o "$JQ_PATH"
    chmod +x "$JQ_PATH"

fi

parse_json_manifest() {

    # The yaml file might be a multi-document yaml , but given that kubelet
    # only reads the first document we will skip the rest.
    NAME=$($JQ_PATH -r '.metadata.name' $1) 
    NAMESPACE=$($JQ_PATH -r '.metadata.namespace // "default"' $1) 

    # if NAME is empty , we'll suppose that the file is 
    # syntaxically incorrect and exit.
    if [ "$NAME" = "" ];then 
       echo "manifest $1 is invalid"
       exit $UNKNOWN
    fi

    FNAME="$NAME-$NODE_NAME"

    RESULT=$(kubectl get pod "$FNAME" -n "$NAMESPACE" -o=jsonpath='{.metadata.name}' 2>/dev/null)
    if [ "$RESULT" = "" ];then
        echo "Static pod $FNAME is missing a mirror pod"
        exit "$NONOK"
    fi
}


parse_yaml_manifest() {

    NAME=$($YQ_PATH -r '.metadata.name' $1 | head -1) 
    NAMESPACE=$($YQ_PATH -r '.metadata.namespace // "default"' $1 | head -1)

    # if NAME is empty , we'll suppose that the file is 
    # syntaxically incorrect and exit.
    if [ "$NAME" = "" ];then 
        echo "manifest $1 is invalid"
        exit $UNKNOWN
    fi

    FNAME="$NAME-$NODE_NAME"

    RESULT=$(kubectl get pod "$FNAME" -n "$NAMESPACE" -o=jsonpath='{.metadata.name}' 2>/dev/null)
    if [ "$RESULT" = "" ];then
        echo "Static pod $FNAME is missing a mirror pod"
        exit "$NONOK"
    fi
}

for file in "$STATIC_PODS_DIR"/*
do
    # we read the first non blank line , if it starts with '{'
    # after trimming then it's json , otherwise we will consider it
    # as yaml. (It's not 100% accurate , but it's a good guess work).

    # we read the first line and strip the leading whitespaces
    fline=$(grep -m 1 . $file | awk '{$1=$1};1')

    fchar="${fline:0:1}"
    if [ $fchar = "{" ]; then
        parse_json_manifest "$file"    
    else
        parse_yaml_manifest "$file"
    fi

done
exit $OK
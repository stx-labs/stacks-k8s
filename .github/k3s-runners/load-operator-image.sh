#!/usr/bin/env bash
# Builds the network operator image and stages it where k3s imports archives on
# startup, so every node has stacks-network-operator:ci before Pods need it.
# Run before k3s-node.sh.
set -euo pipefail
images=/var/lib/rancher/k3s/agent/images
docker build -q -f operators/network/Dockerfile -t stacks-network-operator:ci .
sudo mkdir -p "$images"
docker save stacks-network-operator:ci | sudo tee "$images/stacks-network-operator.tar" >/dev/null

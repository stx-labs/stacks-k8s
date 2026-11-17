#!/usr/bin/env bash
# Builds the network operator image and stages it where k3s imports archives on
# startup, so every node has stacks-network-operator:ci before Pods need it.
# With BASE_REVERT set to a commit, also builds stacks-network-operator:ci-base from this
# checkout with only that commit reverted, using the same Dockerfile and build command,
# so the two images differ only by that change (it needs the commit's history). Prints
# "image id" for each image. Run before k3s-node.sh.
set -euo pipefail
images=/var/lib/rancher/k3s/agent/images
sudo mkdir -p "$images"
build() {
  local context=$1 image=stacks-network-operator:$2
  docker build -q -f "$context/operators/network/Dockerfile" -t "$image" "$context" >/dev/null
  docker save "$image" | sudo tee "$images/stacks-network-operator-$2.tar" >/dev/null
  echo "$image $(docker image inspect --format '{{.Id}}' "$image")"
}
build . ci
if [ -n "${BASE_REVERT:-}" ]; then
  base="$(mktemp -d)/base"
  git worktree add -q --detach "$base" HEAD
  git -C "$base" revert --no-commit "$BASE_REVERT"
  echo "base: HEAD with $(git log -1 --format='%h %s' "$BASE_REVERT") reverted"
  build "$base" ci-base
fi

#!/usr/bin/env bash
# Usage: place-operator.sh NODE [IMAGE]
# Moves the network operator (release "network") onto NODE, tolerating the control-plane
# taint, optionally switching its manager container to IMAGE (preloaded on the nodes), and
# returns once the replacement Pod runs there and holds the leader lease. The chart has no
# placement values, so this patches the installed Deployment.
# Env: KCFG, CTX.
set -euo pipefail
node=$1 image=${2:-}
k() { kubectl --kubeconfig "$KCFG" --context "$CTX" -n stacks-network-system "$@"; }
k patch deployment network --type=strategic -p "$(jq -nc --arg host "$node" --arg image "$image" '{spec: {template: {spec: ({
  nodeSelector: {"kubernetes.io/hostname": $host},
  tolerations: [{key: "node-role.kubernetes.io/control-plane", operator: "Exists", effect: "NoSchedule"}]}
  + if $image == "" then {} else {containers: [{name: "manager", image: $image}]} end)}}}')" >/dev/null
k rollout status deployment network --timeout 5m >/dev/null
pod="$(k get pods -l app.kubernetes.io/instance=network -o json |
  jq -r --arg node "$node" --arg image "$image" '.items[] | select(.metadata.deletionTimestamp == null and
    .spec.nodeName == $node and ($image == "" or .spec.containers[0].image == $image)) | .metadata.name' | head -1)"
[ -n "$pod" ] || { echo "no operator Pod on $node${image:+ running $image}" >&2; exit 1; }
for _ in $(seq 60); do
  holder="$(k get lease stacks-network-operator.network.stacks.org -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || true)"
  if [[ "$holder" == "$pod"_* ]]; then
    echo "operator $pod leads on $node ($(k get pod "$pod" -o jsonpath='{.status.containerStatuses[0].imageID}'))"
    exit 0
  fi
  sleep 3
done
echo "operator $pod on $node did not acquire the leader lease" >&2
exit 1

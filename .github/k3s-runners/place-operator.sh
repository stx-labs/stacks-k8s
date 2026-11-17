#!/usr/bin/env bash
# Usage: place-operator.sh NODE
# Moves the network operator (release "network") onto NODE, tolerating the control-plane
# taint, and returns once the replacement Pod holds the leader lease. The chart has no
# placement values, so this patches the installed Deployment; the image is unchanged.
# Env: KCFG, CTX.
set -euo pipefail
node=$1
k() { kubectl --kubeconfig "$KCFG" --context "$CTX" -n stacks-network-system "$@"; }
k patch deployment network --type=strategic -p "$(jq -nc --arg host "$node" '{spec: {template: {spec: {
  nodeSelector: {"kubernetes.io/hostname": $host},
  tolerations: [{key: "node-role.kubernetes.io/control-plane", operator: "Exists", effect: "NoSchedule"}]}}}}')" \
  >/dev/null
k rollout status deployment network --timeout 5m >/dev/null
pod="$(k get pods -l app.kubernetes.io/instance=network -o json |
  jq -r --arg node "$node" '.items[] | select(.metadata.deletionTimestamp == null and .spec.nodeName == $node) |
    .metadata.name' | head -1)"
[ -n "$pod" ] || { echo "no operator Pod on $node" >&2; exit 1; }
for _ in $(seq 60); do
  holder="$(k get lease stacks-network-operator.network.stacks.org -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || true)"
  [[ "$holder" == "$pod"_* ]] && { echo "operator $pod leads on $node"; exit 0; }
  sleep 3
done
echo "operator $pod on $node did not acquire the leader lease" >&2
exit 1

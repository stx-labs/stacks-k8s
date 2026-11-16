#!/usr/bin/env bash
# Usage: minimal-network.sh KUBECONFIG CONTEXT OUTPUT_DIR
# Runs the public minimal example on the local k3d cluster: apply paused, start,
# wait for initialization, sample native progress, then dispose in the documented order.
set -euo pipefail
KCFG=$1 CTX=$2 OUT=$3 NS=lab-minimal
k() { kubectl --kubeconfig "$KCFG" --context "$CTX" "$@"; }
kn() { k -n "$NS" "$@"; }
stamp() { date -u +%H:%M:%S; }
conditions() { kn get stacksnetwork network -o jsonpath='{range .status.conditions[*]}{.type}={.status}/{.reason} {end}'; }
mkdir -p "$OUT"

# Map the kind node names in the example onto the k3d cluster.
sed -e 's/stacks-k8s-worker2/k3d-stacks-k8s-agent-1/' \
  -e 's/hostname: stacks-k8s-worker$/hostname: k3d-stacks-k8s-agent-0/' \
  -e 's/stacks-k8s-control-plane/k3d-stacks-k8s-server-0/' \
  docs/design/public-api/examples/minimal.yaml > "$OUT/minimal-k3d.yaml"

k apply -f "$OUT/minimal-k3d.yaml" >/dev/null
kn wait stacksnetwork/network --for=jsonpath='{.status.conditions[?(@.type=="Resolved")].status}'=True --timeout=5m
echo "$(stamp) resolved: $(conditions)"

kn patch stacksnetwork network --type=merge -p '{"spec":{"operation":"Running"}}' >/dev/null
start=$(date +%s)
echo "$(stamp) running"
until conditions | grep -q 'Initialized=True'; do
  if (( $(date +%s) - start > 2400 )); then echo "$(stamp) not initialized after 40 min: $(conditions)"; exit 1; fi
  sleep 30
  echo "$(stamp) +$(( $(date +%s) - start ))s $(conditions)"
done
echo "initialized_seconds=$(( $(date +%s) - start ))" | tee "$OUT/initialized.env"

rpc=$(kn get svc -o name | grep stacksnode-node-01-rpc | cut -d/ -f2)
info() { k get --raw "/api/v1/namespaces/$NS/services/$rpc:20443/proxy/v2/info" | jq -c '{burn: .burn_block_height, stacks: .stacks_tip_height}'; }
echo "$(stamp) progress $(info)"
sleep 60
echo "$(stamp) progress $(info)"
k get --raw "/api/v1/namespaces/$NS/services/$rpc:20443/proxy/v2/pox" | jq -c '{epoch: .current_epoch, cycle: .current_cycle.id}'
kn get pods -o wide > "$OUT/pods.txt"

kn patch stacksnetwork network --type=merge -p '{"spec":{"operation":"Stopped"}}' >/dev/null
kn wait stacksnetwork/network --for=jsonpath='{.status.phase}'=Stopped --timeout=5m
kn delete stacksnetwork network --wait=true --timeout=5m
kn get stacksnetworkparticipants,persistentvolumeclaims
k delete namespace "$NS" --wait=true --timeout=5m
echo "$(stamp) disposed"

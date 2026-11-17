#!/usr/bin/env bash
# Usage: minimal-network.sh KUBECONFIG CONTEXT OUTPUT_DIR
# Runs the public minimal example: apply paused, start, wait for initialization,
# sample native progress, then dispose in the documented order. NODE_WORKER,
# NODE_WORKER2 and NODE_CONTROL_PLANE replace the example's kind node names
# (defaults: the local k3d cluster).
set -euo pipefail
KCFG=$1 CTX=$2 OUT=$3 NS=lab-minimal
NODE_WORKER=${NODE_WORKER:-k3d-stacks-k8s-agent-0}
NODE_WORKER2=${NODE_WORKER2:-k3d-stacks-k8s-agent-1}
NODE_CONTROL_PLANE=${NODE_CONTROL_PLANE:-k3d-stacks-k8s-server-0}
k() { kubectl --kubeconfig "$KCFG" --context "$CTX" "$@"; }
kn() { k -n "$NS" "$@"; }
stamp() { date -u +%H:%M:%S; }
conditions() { kn get stacksnetwork network -o jsonpath='{range .status.conditions[*]}{.type}={.status}/{.reason} {end}'; }
# Bitcoin height, highest Stacks height, and ready/total Pods per participant kind
# (e.g. StacksNode=1/1) with total restarts, from the API status and Pod labels.
progress() {
  local btc stx pods
  btc="$(kn get stacksnetwork network -o jsonpath='{.status.burnchainObservations.bitcoin.height}' 2>/dev/null)"
  stx="$(kn get stacksnetworkparticipants -o jsonpath='{range .items[*]}{.status.runtime.protocol.stacksHeight}{"\n"}{end}' 2>/dev/null | sort -n | tail -1)"
  pods="$(kn get pods -o jsonpath='{range .items[*]}{.metadata.labels.network\.stacks\.org/participant-kind}{"\t"}{.status.phase}{"\t"}{.status.containerStatuses[*].ready}{"\t"}{.status.containerStatuses[*].restartCount}{"\n"}{end}' 2>/dev/null |
    awk -F'\t' '$1 != "" && $2 != "Succeeded" {
        total[$1]++; if ($2 == "Running" && $3 !~ /false/) ready[$1]++
        n = split($4, r, " "); for (i = 1; i <= n; i++) restarts += r[i]
      }
      END { for (k in total) printf "%s=%d/%d\n", k, ready[k], total[k]; printf "~restarts=%d\n", restarts }' |
    sort | tr -d '~' | paste -sd' ' -)"
  echo "bitcoin=${btc:--} stacks=${stx:--} $pods"
}
# Evidence is lost with the namespace and the runner: save it before teardown or on failure.
collect() {
  mkdir -p "$OUT/logs"
  kn get pods -o wide > "$OUT/pods.txt" 2>&1 || true
  kn get stacksnetwork network -o yaml > "$OUT/network.yaml" 2>&1 || true
  # Every namespaced *.stacks.org resource: participants, Bitcoin executions, production, ...
  kn get "$(k api-resources --namespaced --verbs=list -o name | grep '\.stacks\.org$' | paste -sd, -)" \
    -o yaml > "$OUT/resources.yaml" 2>&1 || true
  kn get events --sort-by=.lastTimestamp > "$OUT/events.txt" 2>&1 || true
  for pod in $(kn get pods -o name 2>/dev/null); do
    kn logs "$pod" --all-containers --prefix --tail=5000 > "$OUT/logs/${pod#pod/}.log" 2>&1 || true
    kn logs "$pod" --all-containers --prefix --previous --tail=1000 > "$OUT/logs/${pod#pod/}.previous.log" 2>/dev/null ||
      rm -f "$OUT/logs/${pod#pod/}.previous.log"
  done
}
on_exit() { local status=$?; if [ "$status" -ne 0 ]; then collect; fi; }
trap on_exit EXIT
mkdir -p "$OUT"

# Map the kind node names in the example onto this cluster.
sed -e "s/hostname: stacks-k8s-worker2$/hostname: $NODE_WORKER2/" \
  -e "s/hostname: stacks-k8s-worker$/hostname: $NODE_WORKER/" \
  -e "s/hostname: stacks-k8s-control-plane$/hostname: $NODE_CONTROL_PLANE/" \
  docs/design/public-api/examples/minimal.yaml > "$OUT/minimal.yaml"

k apply -f "$OUT/minimal.yaml" >/dev/null
kn wait stacksnetwork/network --for=jsonpath='{.status.conditions[?(@.type=="Resolved")].status}'=True --timeout=5m
echo "$(stamp) resolved: $(conditions)"

kn patch stacksnetwork network --type=merge -p '{"spec":{"operation":"Running"}}' >/dev/null
start=$(date +%s)
echo "$(stamp) running"
last=""
until [[ "$last" == *Initialized=True* ]]; do
  if (( $(date +%s) - start > 2400 )); then echo "$(stamp) not initialized after 40 min"; exit 1; fi
  sleep 30
  current="$(conditions)"
  if [ "$current" != "$last" ]; then echo "$(stamp) conditions: $current"; last="$current"; fi
  echo "$(stamp) +$(( $(date +%s) - start ))s $(progress)"
done
echo "initialized_seconds=$(( $(date +%s) - start ))" | tee "$OUT/initialized.env"

rpc=$(kn get svc -o name | grep stacksnode-node-01-rpc | cut -d/ -f2)
info() { k get --raw "/api/v1/namespaces/$NS/services/$rpc:20443/proxy/v2/info" | jq -c '{burn: .burn_block_height, stacks: .stacks_tip_height}'; }
echo "$(stamp) progress $(info)"
sleep 60
echo "$(stamp) progress $(info)"
k get --raw "/api/v1/namespaces/$NS/services/$rpc:20443/proxy/v2/pox" | jq -c '{epoch: .current_epoch, cycle: .current_cycle.id}'
collect

kn patch stacksnetwork network --type=merge -p '{"spec":{"operation":"Stopped"}}' >/dev/null
kn wait stacksnetwork/network --for=jsonpath='{.status.phase}'=Stopped --timeout=5m
kn delete stacksnetwork network --wait=true --timeout=5m
kn get stacksnetworkparticipants,persistentvolumeclaims
k delete namespace "$NS" --wait=true --timeout=5m
echo "$(stamp) disposed"

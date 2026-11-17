#!/usr/bin/env bash
# Usage: minimal-network.sh KUBECONFIG CONTEXT OUTPUT_DIR
# Runs the public minimal example: apply paused, start, wait for initialization,
# sample native progress, then dispose in the documented order. NODE_WORKER,
# NODE_WORKER2 and NODE_CONTROL_PLANE replace the example's kind node names
# (defaults: the local k3d cluster). INIT_TIMEOUT bounds initialization in seconds
# (default 1200: about twice a single runner's ~600s). HOLD_SECONDS (default 0) keeps the
# network up for live inspection before teardown, also after a failure; creating
# HOLD_RELEASE ends the hold early.
set -euo pipefail
KCFG=$1 CTX=$2 OUT=$3 NS=lab-minimal
NODE_WORKER=${NODE_WORKER:-k3d-stacks-k8s-agent-0}
NODE_WORKER2=${NODE_WORKER2:-k3d-stacks-k8s-agent-1}
NODE_CONTROL_PLANE=${NODE_CONTROL_PLANE:-k3d-stacks-k8s-server-0}
INIT_TIMEOUT=${INIT_TIMEOUT:-1200}
HOLD_SECONDS=${HOLD_SECONDS:-0}
HOLD_RELEASE=${HOLD_RELEASE:-/tmp/stacks-k8s-release}
k() { kubectl --kubeconfig "$KCFG" --context "$CTX" "$@"; }
kn() { k -n "$NS" "$@"; }
stamp() { date -u +%H:%M:%S; }
conditions() { kn get stacksnetwork network -o jsonpath='{range .status.conditions[*]}{.type}={.status}/{.reason} {end}'; }
# Greatest value of status FIELD across RESOURCE.
highest() {
  kn get "$1" -o jsonpath="{range .items[*]}{.status.$2}{\"\n\"}{end}" 2>/dev/null | sort -n | tail -1
}
# Heights from operator status (the network's burnchainObservations stay empty until
# initialization completes): Bitcoin nodes' tip, the burn and Stacks heights Stacks nodes
# have processed, Bitcoin initialization phase/reason, and ready/total Pods per participant
# kind with total restarts.
progress() {
  local btc init burn stx pods
  btc="$(highest bitcoinexecutions.bitcoin.stacks.org observation.height)"
  init="$(kn get bitcoininitializations.bitcoin.stacks.org \
    -o jsonpath='{range .items[*]}{.status.phase}/{.status.reason}{","}{end}' 2>/dev/null)"
  init="${init%,}"
  burn="$(highest stacksnetworkparticipants runtime.protocol.burnHeight)"
  stx="$(highest stacksnetworkparticipants runtime.protocol.stacksHeight)"
  pods="$(kn get pods -o jsonpath='{range .items[*]}{.metadata.labels.network\.stacks\.org/participant-kind}{"\t"}{.status.phase}{"\t"}{.status.containerStatuses[*].ready}{"\t"}{.status.containerStatuses[*].restartCount}{"\n"}{end}' 2>/dev/null |
    awk -F'\t' '$1 != "" && $2 != "Succeeded" {
        total[$1]++; if ($2 == "Running" && $3 !~ /false/) ready[$1]++
        n = split($4, r, " "); for (i = 1; i <= n; i++) restarts += r[i]
      }
      END { for (k in total) printf "%s=%d/%d\n", k, ready[k], total[k]; printf "~restarts=%d\n", restarts }' |
    sort | tr -d '~' | paste -sd' ' -)"
  echo "bitcoin=${btc:--} init=${init:--} burn=${burn:--} stacks=${stx:--} $pods"
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
hold() {
  (( HOLD_SECONDS > 0 )) || return 0
  local deadline=$(( $(date +%s) + HOLD_SECONDS ))
  echo "$(stamp) holding up to ${HOLD_SECONDS}s for inspection; create $HOLD_RELEASE to continue"
  until [ -e "$HOLD_RELEASE" ] || (( $(date +%s) >= deadline )); do sleep 10; done
  echo "$(stamp) hold ended"
}
on_exit() { local status=$?; if [ "$status" -ne 0 ]; then collect; hold; fi; }
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
  if (( $(date +%s) - start > INIT_TIMEOUT )); then echo "$(stamp) not initialized after ${INIT_TIMEOUT}s"; exit 1; fi
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
hold

kn patch stacksnetwork network --type=merge -p '{"spec":{"operation":"Stopped"}}' >/dev/null
kn wait stacksnetwork/network --for=jsonpath='{.status.phase}'=Stopped --timeout=5m
kn delete stacksnetwork network --wait=true --timeout=5m
kn get stacksnetworkparticipants,persistentvolumeclaims
k delete namespace "$NS" --wait=true --timeout=5m
echo "$(stamp) disposed"

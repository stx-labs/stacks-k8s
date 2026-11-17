#!/usr/bin/env bash
# Usage: network.sh EXAMPLE KUBECONFIG CONTEXT OUTPUT_DIR
# Runs a public StacksNetwork example (e.g. docs/design/public-api/examples/minimal.yaml):
# apply paused, start, wait for initialization, sample native progress, then dispose in
# the documented order. The example's kind node pins are mapped onto this cluster:
# stacks-k8s-worker and stacks-k8s-worker2 pins rotate through the space-separated
# WORKER_NODES and WORKER2_NODES, and stacks-k8s-control-plane becomes NODE_CONTROL_PLANE
# (defaults: the local k3d cluster). RESOLVE_TIMEOUT bounds resolution (default 300s) and
# INIT_TIMEOUT initialization (default 1200s: about twice a single runner's ~600s for the
# minimal example), both in seconds. RESOLVE_ONLY=true disposes right after resolution.
# HOLD_SECONDS (default 0) keeps the network up for live inspection before teardown, also
# after a failure; creating HOLD_RELEASE ends the hold early.
set -euo pipefail
EXAMPLE=$1 KCFG=$2 CTX=$3 OUT=$4
RESOLVE_ONLY=${RESOLVE_ONLY:-false}
NS="$(awk '/^kind: StacksNetwork$/ { found = 1 } found && /^  namespace:/ { print $2; exit }' "$EXAMPLE")"
WORKER_NODES=${WORKER_NODES:-k3d-stacks-k8s-agent-0}
WORKER2_NODES=${WORKER2_NODES:-k3d-stacks-k8s-agent-1}
NODE_CONTROL_PLANE=${NODE_CONTROL_PLANE:-k3d-stacks-k8s-server-0}
RESOLVE_TIMEOUT=${RESOLVE_TIMEOUT:-300}
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
# API server request counters for every client, one series per label set.
requests() { k get --raw /metrics 2>/dev/null | grep '^apiserver_request_total{' || true; }
# Requests between two snapshots for *.stacks.org resources and Secrets, as
# "count VERB resource[/subresource] code", largest first. Includes every client.
request_delta() {
  awk 'NR == FNR { before[$1] = $2; next } { d = $2 - before[$1]; if (d > 0) print $1, d }' "$1" "$2" |
    awk '$1 ~ /group="[a-z.]*stacks\.org"|resource="secrets"/ {
        match($1, /verb="[A-Z]+"/); verb = substr($1, RSTART + 6, RLENGTH - 7)
        match($1, /resource="[a-z]+"/); res = substr($1, RSTART + 10, RLENGTH - 11)
        part = ""; if (match($1, /subresource="[a-z]+"/)) part = "/" substr($1, RSTART + 13, RLENGTH - 14)
        match($1, /code="[0-9]+"/); code = substr($1, RSTART + 6, RLENGTH - 7)
        n[verb " " res part " " code] += $2
      }
      END { for (k in n) print n[k], k }' | sort -rn
}
hold() {
  (( HOLD_SECONDS > 0 )) || return 0
  local deadline=$(( $(date +%s) + HOLD_SECONDS ))
  echo "$(stamp) holding up to ${HOLD_SECONDS}s for inspection; create $HOLD_RELEASE to continue"
  until [ -e "$HOLD_RELEASE" ] || (( $(date +%s) >= deadline )); do sleep 10; done
  echo "$(stamp) hold ended"
}
# Documented order: stop, delete the network, then its namespace.
dispose() {
  kn patch stacksnetwork network --type=merge -p '{"spec":{"operation":"Stopped"}}' >/dev/null
  kn wait stacksnetwork/network --for=jsonpath='{.status.phase}'=Stopped --timeout=5m
  kn delete stacksnetwork network --wait=true --timeout=5m
  kn get stacksnetworkparticipants,persistentvolumeclaims
  k delete namespace "$NS" --wait=true --timeout=5m
  echo "$(stamp) disposed"
}
on_exit() { local status=$?; if [ "$status" -ne 0 ]; then collect; hold; fi; }
trap on_exit EXIT
mkdir -p "$OUT"

# Map the kind node names in the example onto this cluster.
awk -v workers="$WORKER_NODES" -v workers2="$WORKER2_NODES" -v control="$NODE_CONTROL_PLANE" '
  BEGIN { n = split(workers, w, " "); n2 = split(workers2, w2, " ") }
  /hostname: stacks-k8s-worker$/ { sub(/stacks-k8s-worker$/, w[i++ % n + 1]) }
  /hostname: stacks-k8s-worker2$/ { sub(/stacks-k8s-worker2$/, w2[j++ % n2 + 1]) }
  /hostname: stacks-k8s-control-plane$/ { sub(/stacks-k8s-control-plane$/, control) }
  { print }' "$EXAMPLE" > "$OUT/network-manifest.yaml"

requests > "$OUT/requests-before.txt"
k apply -f "$OUT/network-manifest.yaml" >/dev/null
applied=$(date +%s)
kn wait stacksnetwork/network --for=jsonpath='{.status.conditions[?(@.type=="Resolved")].status}'=True \
  --timeout="${RESOLVE_TIMEOUT}s"
echo "resolved_seconds=$(( $(date +%s) - applied ))" | tee "$OUT/resolved.env"
requests > "$OUT/requests-resolved.txt"
request_delta "$OUT/requests-before.txt" "$OUT/requests-resolved.txt" > "$OUT/resolution-requests.txt"
# Aggregate API server workload during resolution, summed over every client: not per-pass
# or per-controller timing.
awk '{ total += $1; if ($2 == "GET" && $3 == "stacksnetworks") gets += $1; if ($4 == 409) conflicts += $1 }
  END { printf "aggregate_requests=%d\naggregate_network_gets=%d\naggregate_conflicts=%d\n", total, gets, conflicts }' \
  "$OUT/resolution-requests.txt" | tee -a "$OUT/resolved.env"
head -8 "$OUT/resolution-requests.txt"
echo "$(stamp) resolved: $(conditions)"
if [ "$RESOLVE_ONLY" = true ]; then
  collect
  dispose
  exit 0
fi

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

rpc=$(kn get svc -o name | grep -m1 -E 'stacksnode-.+-rpc' | cut -d/ -f2)
info() { k get --raw "/api/v1/namespaces/$NS/services/$rpc:20443/proxy/v2/info" | jq -c '{burn: .burn_block_height, stacks: .stacks_tip_height}'; }
echo "$(stamp) progress $(info)"
sleep 60
echo "$(stamp) progress $(info)"
k get --raw "/api/v1/namespaces/$NS/services/$rpc:20443/proxy/v2/pox" | jq -c '{epoch: .current_epoch, cycle: .current_cycle.id}'
collect
hold
dispose

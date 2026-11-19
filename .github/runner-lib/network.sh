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
# have processed, Bitcoin initialization phase/reason, the cadence (interval@fromHeight) that
# set the current opportunity anchor, and ready/total Pods per participant kind with total
# restarts.
progress() {
  local btc init cadence burn stx pods
  btc="$(highest bitcoinexecutions.bitcoin.stacks.org observation.height)"
  init="$(kn get bitcoininitializations.bitcoin.stacks.org \
    -o jsonpath='{range .items[*]}{.status.phase}/{.status.reason}{","}{end}' 2>/dev/null)"
  init="${init%,}"
  cadence="$(kn get bitcoininitializations.bitcoin.stacks.org \
    -o jsonpath='{range .items[*]}{.status.cadence.cadence.interval}@{.status.cadence.fromHeight}{","}{end}' 2>/dev/null)"
  cadence="${cadence%,}"
  burn="$(highest stacksnetworkparticipants runtime.protocol.burnHeight)"
  stx="$(highest stacksnetworkparticipants runtime.protocol.stacksHeight)"
  pods="$(kn get pods -o jsonpath='{range .items[*]}{.metadata.labels.network\.stacks\.org/participant-kind}{"\t"}{.status.phase}{"\t"}{.status.containerStatuses[*].ready}{"\t"}{.status.containerStatuses[*].restartCount}{"\n"}{end}' 2>/dev/null |
    awk -F'\t' '$1 != "" && $2 != "Succeeded" {
        total[$1]++; if ($2 == "Running" && $3 !~ /false/) ready[$1]++
        n = split($4, r, " "); for (i = 1; i <= n; i++) restarts += r[i]
      }
      END { for (k in total) printf "%s=%d/%d\n", k, ready[k], total[k]; printf "~restarts=%d\n", restarts }' |
    sort | tr -d '~' | paste -sd' ' -)"
  echo "bitcoin=${btc:--} init=${init:--} cadence=${cadence:--} burn=${burn:--} stacks=${stx:--} $pods"
}
# Age in seconds of the oldest Stacks node protocol observation; empty before any exists.
observation_age() {
  kn get stacksnetworkparticipants -o json 2>/dev/null |
    jq -r '[.items[] | select(.spec.kind == "StacksNode") | .status.runtime.protocol.observedAt // empty
      | now - fromdateiso8601] | max // empty | floor'
}
# Appends the Bitcoin target's last receipt every 2 s as "receivedAt expectedHeight count";
# receipts closer together than a sample are missed, not merged.
sample_receipts() {
  while :; do
    kn get bitcoinexecutions.bitcoin.stacks.org -o json 2>/dev/null |
      jq -r '.items[].status.lastReceipt // empty | select(.request.offer != null)
        | "\(.receivedAt) \(.request.offer.expectedHeight) \(.request.offer.count // 0)"' >> "$1" || true
    sleep 2
  done
}
# Initialization evidence as KEY=value lines. Block spacing uses consecutive single-block
# receipts, grouped by the admitted cadence interval selected at the receipt's height:
# n/p50/p90/mean seconds. CPU is this host's /proc/stat between two snapshots: busy,
# iowait and steal percentages. Passes are count/mean milliseconds from pass accounting:
# all Stacks node passes, their observe/converge split where the image records it, and the
# Bitcoin bootstrap scheduler.
initialization_summary() {
  local schedule
  schedule="$(kn get stacksnetworkparticipants -o json |
    jq -c '[.items[] | select(.spec.kind == "BitcoinBlockProduction")
      | .status.admission.configuration.bitcoinBlockProduction.schedule][0]
      | if .cadence then [{fromHeight: 0, interval: .cadence.interval}]
        else [.cadences[] | {fromHeight, interval: .cadence.interval}] end')"
  sort -u "$OUT/receipts.txt" | jq -Rrs --argjson schedule "$schedule" '
    def interval($h): [$schedule[] | select(.fromHeight <= $h)] | max_by(.fromHeight).interval;
    def pct($p): length as $n | .[[($n * $p | floor), $n - 1] | min];
    [split("\n")[] | select(. != "") | split(" ")
      | {at: (.[0] | sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601), height: (.[1] | tonumber), count: (.[2] | tonumber)}
      | .after = .height + (if .count > 1 then .count else 1 end)]
    | sort_by(.at) | [range(1; length) as $i | {a: .[$i - 1], b: .[$i]}]
    | map(select(.b.count <= 1 and .b.after - .a.after == 1) | {interval: interval(.b.height), gap: (.b.at - .a.at)})
    | group_by(.interval)[]
    | (map(.gap) | sort) as $g
    | "blocks_\(.[0].interval)=\($g | length)/\($g | pct(0.5))/\($g | pct(0.9))/\(($g | add) / ($g | length) * 10 | round / 10)"'
  sort -n "$OUT/observation-ages.txt" | awk '{ a[NR] = $1 } END {
    if (NR) { i = int(NR * 0.9); if (i < 1) i = 1
      printf "observation_age_max=%d\nobservation_age_p90=%d\n", a[NR], a[i] } }'
  if [ -s "$OUT/cpu-running.txt" ] && [ -s "$OUT/cpu-initialized.txt" ]; then
    cat "$OUT/cpu-running.txt" "$OUT/cpu-initialized.txt" | awk '
      { for (i = 2; i <= 9; i++) v[NR, i] = $i }
      END {
        for (i = 2; i <= 9; i++) { d[i] = v[2, i] - v[1, i]; total += d[i] }
        printf "cpu_busy=%.0f\ncpu_iowait=%.0f\ncpu_steal=%.0f\n",
          100 * (total - d[5] - d[6]) / total, 100 * d[6] / total, 100 * d[9] / total }'
  fi
  awk '$1 !~ /^#/ {
      if ($3 == "stacks_network_actor_pass_seconds" && $4 ~ /kind=StacksNode/) {
        split($4, l, /path=/); path = l[2]; sub(/,.*/, "", path); n[path] += $1; s[path] += $2 }
      if ($3 == "controller_runtime_reconcile_time_seconds" && $4 ~ /controller=bitcoin-initialization-v1alpha2/) {
        n["scheduler"] += $1; s["scheduler"] += $2 }
      if ($3 == "controller_runtime_reconcile_time_seconds" && $4 ~ /controller=participant-stacksnode-workload/) {
        n["stacksnode"] += $1; s["stacksnode"] += $2 }
    }
    END { for (k in n) printf "passes_%s=%d/%.0f\n", k, n[k], 1000 * s[k] / n[k] }' "$OUT/operator-passes-initialization.txt"
  awk '$1 !~ /^#/ { n += $1 } END { if (NR) printf "direct_reads=%d\n", n }' "$OUT/operator-reads-initialization.txt"
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
  # Complete logs (k3s-node.sh raises kubelet rotation so the current file holds them).
  for pod in $(kn get pods -o name 2>/dev/null); do
    { kn logs "$pod" --all-containers --prefix 2>&1 || true; } | gzip > "$OUT/logs/${pod#pod/}.log.gz"
    if kn logs "$pod" --all-containers --prefix --previous > "$OUT/logs/previous" 2>/dev/null; then
      gzip -c "$OUT/logs/previous" > "$OUT/logs/${pod#pod/}.previous.log.gz"
    fi
    rm -f "$OUT/logs/previous"
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
# Experiment-only operator builds log cumulative totals at startup and every interval:
# direct reads as "api-accounting" ({verb, kind, outcome, helper, entry}) and reconcile
# passes as "pass-accounting" ({metric, labels}), each with count and summed seconds.
# accounting_window LOGGER KEY START END FROM TO prints, for the operator's current Pod, the
# difference between the last snapshot at or before START and the first at or after END
# (RFC 3339): a "# window" line with both snapshot times and their offsets from START (the
# FROM event) and END (the TO event), so the window covers activity on both sides of the
# span, then "count seconds key" rows, largest first. KEY is a jq expression naming a
# series. Prints nothing when the operator logs no snapshots, and an explicit
# "# incomplete" line when no snapshot closes the window. Seconds are summed durations,
# which overlap across concurrent callers and controllers.
accounting_window() {
  k -n stacks-network-system logs deploy/network --tail=-1 2>/dev/null |
    jq -c --arg logger "$1" 'select(.logger == $logger) | {ts, series}' 2>/dev/null |
    jq -rs --arg logger "$1" --arg start "$3" --arg end "$4" --arg from "$5" --arg to "$6" "def key: $2;"'
      def epoch: sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601;
      def totals: map({key: key, value: [.count, (.seconds // 0)]}) | from_entries;
      if length == 0 then empty else
        (map(select(.ts <= $start)) | last) as $first
        | (map(select(.ts >= $end)) | first) as $last
        | if $last == null then "# incomplete: no \($logger) snapshot at or after \($end)" else
            ($first // {ts: "operator start", series: []}) as $first
            | "# window: \($first.ts)" +
                (if $first.series == [] and $first.ts == "operator start" then ""
                 else " (\(($start | epoch) - ($first.ts | epoch))s before \($from))" end) +
                " to \($last.ts) (\(($last.ts | epoch) - ($end | epoch))s after \($to))",
              ( ($first.series | totals) as $before
                | [$last.series | totals | to_entries[]
                    | [.value[0] - ($before[.key][0] // 0), .value[1] - ($before[.key][1] // 0), .key]
                    | select(.[0] > 0)]
                | sort_by(-.[0])[]
                | "\(.[0]) \(.[1] * 10 | round / 10) \(.[2])" )
          end
      end'
}
# Direct reads as "count seconds VERB Kind outcome helper <- entry" rows.
operator_reads() {
  accounting_window api-accounting \
    '[.verb, .kind, .outcome, (.helper // "-"), (.entry // "-")] | join(" ")' "$@" |
    sed -E '/^#/!s/ ([^ ]+) ([^ ]+)$/ \1 <- \2/'
}
# Reconcile passes as "count seconds metric label=value,..." rows.
operator_passes() {
  accounting_window pass-accounting \
    '.metric + " " + (.labels | to_entries | sort_by(.key) | map("\(.key)=\(.value)") | join(","))' "$@"
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
sampler=""
on_exit() {
  local status=$?
  [ -z "$sampler" ] || kill "$sampler" 2>/dev/null || true
  if [ "$status" -ne 0 ]; then collect; hold; fi
}
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
applied_at="$(date -u +%FT%TZ)"
k apply -f "$OUT/network-manifest.yaml" >/dev/null
applied=$(date +%s)
kn wait stacksnetwork/network --for=jsonpath='{.status.conditions[?(@.type=="Resolved")].status}'=True \
  --timeout="${RESOLVE_TIMEOUT}s"
resolved_at="$(date -u +%FT%TZ)"
echo "resolved_seconds=$(( $(date +%s) - applied ))" | tee "$OUT/resolved.env"
requests > "$OUT/requests-resolved.txt"
request_delta "$OUT/requests-before.txt" "$OUT/requests-resolved.txt" > "$OUT/resolution-requests.txt"
# Aggregate API server workload during resolution, summed over every client: not per-pass
# or per-controller timing.
awk '{ total += $1; if ($2 == "GET" && $3 == "stacksnetworks") gets += $1; if ($4 == 409) responses409 += $1 }
  END { printf "aggregate_requests=%d\naggregate_network_gets=%d\naggregate_409s=%d\n", total, gets, responses409 }' \
  "$OUT/resolution-requests.txt" | tee -a "$OUT/resolved.env"
head -8 "$OUT/resolution-requests.txt"
# Let the operator log the snapshot that closes the resolution window (5s interval in CI).
sleep 7
operator_reads "$applied_at" "$resolved_at" apply Resolved > "$OUT/operator-reads.txt"
if [ -s "$OUT/operator-reads.txt" ]; then
  echo "operator direct reads (count, summed call seconds, verb, kind, outcome, call site):"
  head -14 "$OUT/operator-reads.txt"
fi
echo "$(stamp) resolved: $(conditions)"
if [ "$RESOLVE_ONLY" = true ]; then
  collect
  dispose
  exit 0
fi

kn patch stacksnetwork network --type=merge -p '{"spec":{"operation":"Running"}}' >/dev/null
start=$(date +%s)
running_at="$(date -u +%FT%TZ)"
head -1 /proc/stat > "$OUT/cpu-running.txt" 2>/dev/null || true
: > "$OUT/receipts.txt"
: > "$OUT/observation-ages.txt"
sample_receipts "$OUT/receipts.txt" &
sampler=$!
echo "$(stamp) running"
last=""
until [[ "$last" == *Initialized=True* ]]; do
  if (( $(date +%s) - start > INIT_TIMEOUT )); then echo "$(stamp) not initialized after ${INIT_TIMEOUT}s"; exit 1; fi
  sleep 30
  current="$(conditions)"
  if [ "$current" != "$last" ]; then echo "$(stamp) conditions: $current"; last="$current"; fi
  age="$(observation_age)"
  [ -z "$age" ] || echo "$age" >> "$OUT/observation-ages.txt"
  echo "$(stamp) +$(( $(date +%s) - start ))s $(progress) observation_age=${age:--}"
  # A latched failure never initializes; the network must be recreated.
  if [[ "$last" == *Failed=True* ]]; then echo "$(stamp) network failed before initialization"; exit 1; fi
done
initialized_at="$(date -u +%FT%TZ)"
head -1 /proc/stat > "$OUT/cpu-initialized.txt" 2>/dev/null || true
kill "$sampler" 2>/dev/null || true
sampler=""
echo "initialized_seconds=$(( $(date +%s) - start ))" | tee "$OUT/initialized.env"
# Let the operator log the snapshots that close the initialization window.
sleep 7
operator_reads "$running_at" "$initialized_at" running Initialized > "$OUT/operator-reads-initialization.txt"
operator_passes "$running_at" "$initialized_at" running Initialized > "$OUT/operator-passes-initialization.txt"
initialization_summary | tee -a "$OUT/initialized.env"

rpc=$(kn get svc -o name | grep -m1 -E 'stacksnode-.+-rpc' | cut -d/ -f2)
info() { k get --raw "/api/v1/namespaces/$NS/services/$rpc:20443/proxy/v2/info" | jq -c '{burn: .burn_block_height, stacks: .stacks_tip_height}'; }
echo "$(stamp) progress $(info)"
sleep 60
echo "$(stamp) progress $(info)"
k get --raw "/api/v1/namespaces/$NS/services/$rpc:20443/proxy/v2/pox" | jq -c '{epoch: .current_epoch, cycle: .current_cycle.id}'
collect
hold
dispose

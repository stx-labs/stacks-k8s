#!/usr/bin/env bash
# Usage: rounds.sh EXAMPLE OUTPUT_DIR TITLE ROUND...
# Resolution-only comparison within one retained cluster. Each ROUND is
# "label:node:image": the operator is moved to node running image (rollout and leader
# acquisition complete first), then a fresh network from EXAMPLE is applied, resolved
# (the same Resolved criterion each round) and disposed in the documented order. Repeat a
# configuration (A-B-A) to expose warm-up and drift. Request counts are aggregate API
# server workload across all clients, not per-controller or per-pass measurements; other
# differences between rounds (scheduling, image caches, background load) are not
# controlled.
# Env: KCFG, CTX and network.sh placement variables (see example-env.sh).
set -euo pipefail
EXAMPLE=$1 OUT=$2 TITLE=$3
shift 3
here="$(dirname "$0")"
mkdir -p "$OUT"
n=0
for round in "$@"; do
  n=$((n + 1))
  IFS=: read -r label node image <<< "$round"
  dir="$OUT/round-$n-$label"
  mkdir -p "$dir"
  echo "== round $n: $label, operator on $node running $image"
  "$here/place-operator.sh" "$node" "$image" | tee "$dir/operator.txt"
  RESOLVE_ONLY=true "$here/../runner-lib/network.sh" "$EXAMPLE" "$KCFG" "$CTX" "$dir"
done
{
  echo "### $TITLE (resolution only, $(basename "$EXAMPLE" .yaml))"
  echo
  echo "Requests, network GETs and 409 responses are aggregate API server workload (all clients)."
  echo "Topology inventory reads are the topology reconciler's direct participant GET/LIST calls"
  echo "(count / summed call seconds) over the accounting window in each round's operator-reads.txt."
  echo
  echo "| Round | Operator | Node | Image ID | Resolved (s) | Requests | Network GETs | 409 responses | Topology inventory reads |"
  echo "| --- | --- | --- | --- | --- | --- | --- | --- | --- |"
  n=0
  for round in "$@"; do
    n=$((n + 1))
    IFS=: read -r label node _ <<< "$round"
    dir="$OUT/round-$n-$label"
    id="$(grep -o 'sha256:[0-9a-f]\{12\}' "$dir/operator.txt" | tail -1)"
    # Missing evidence must not read as eliminated reads.
    reads="$dir/operator-reads.txt"
    if ! grep -q '^# window:' "$reads" 2>/dev/null; then
      inventory=unavailable
      grep -q '^# incomplete' "$reads" 2>/dev/null && inventory=incomplete
    else
      inventory="$(awk '$6 ~ /reconcileTopology/ && $4 ~ /^StacksNetworkParticipant(List)?$/ { n += $1; s += $2 }
        END { printf "%d / %.1f", n, s }' "$reads")"
    fi
    # shellcheck disable=SC1090,SC1091,SC2154 # Keys written by network.sh.
    (
      . "$dir/resolved.env"
      echo "| $n | $label | ${node#"${NODE_PREFIX:-}-"} | ${id:--} | $resolved_seconds | $aggregate_requests | $aggregate_network_gets | $aggregate_409s | $inventory |"
    )
  done
  echo
  for node in $(printf '%s\n' "$@" | cut -d: -f2 | sort -u); do
    rtt="$(tailscale ping -c 5 "$node" 2>/dev/null | grep -o 'in [0-9.]*ms' | tail -1 || true)"
    echo "Server ↔ $node: ${rtt:-same node or unmeasured}"
    echo
  done
} | tee "$OUT/summary.md"

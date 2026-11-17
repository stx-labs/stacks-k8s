#!/usr/bin/env bash
# Usage: placement.sh EXAMPLE OUTPUT_DIR AGENT_NODE SERVER_NODE
# Resolution-only operator placement comparison within one retained cluster: the same
# example, operator image and Resolved criterion in three rounds, with the operator on
# AGENT_NODE, SERVER_NODE, then AGENT_NODE again (A-B-A, exposing warm-up and drift). Each
# round applies a fresh network and disposes it. Request counts are aggregate API server
# workload across all clients, not per-controller or per-pass measurements. The server
# round moves the operator next to the API server, but other differences between rounds
# (scheduling, image caches, background load) are not controlled.
# Env: KCFG, CTX and network.sh placement variables (see example-env.sh).
set -euo pipefail
EXAMPLE=$1 OUT=$2 AGENT_NODE=$3 SERVER_NODE=$4
here="$(dirname "$0")"
mkdir -p "$OUT"
# Context only: round trip from the API server's node to the agent.
tailscale ping -c 5 "$AGENT_NODE" > "$OUT/agent-ping.txt" 2>&1 || true
rounds=("1 agent $AGENT_NODE" "2 server $SERVER_NODE" "3 agent $AGENT_NODE")
for round in "${rounds[@]}"; do
  read -r n placement node <<< "$round"
  echo "== round $n: operator on $placement ($node)"
  "$here/place-operator.sh" "$node"
  RESOLVE_ONLY=true "$here/../runner-lib/network.sh" "$EXAMPLE" "$KCFG" "$CTX" "$OUT/round-$n-$placement"
done
{
  echo "### Operator placement (resolution only, $(basename "$EXAMPLE" .yaml))"
  echo
  echo "Request counts are aggregate API server workload (all clients) during resolution."
  echo
  echo "| Round | Operator | Resolved (s) | Requests | Network GETs | 409 conflicts |"
  echo "| --- | --- | --- | --- | --- | --- |"
  for round in "${rounds[@]}"; do
    read -r n placement _ <<< "$round"
    # shellcheck disable=SC1090,SC2154 # Keys written by network.sh.
    (
      . "$OUT/round-$n-$placement/resolved.env"
      echo "| $n | $placement | $resolved_seconds | $aggregate_requests | $aggregate_network_gets | $aggregate_conflicts |"
    )
  done
  echo
  echo "Agent ↔ server: $(grep -m1 -o 'in [0-9.]*ms' "$OUT/agent-ping.txt" || echo unmeasured)"
} | tee "$OUT/summary.md"

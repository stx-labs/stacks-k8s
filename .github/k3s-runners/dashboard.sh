#!/usr/bin/env bash
# Usage: dashboard.sh KUBECONFIG CONTEXT
# Installs the repository's Headlamp dashboard and exposes it over plain HTTP on this
# node's Tailscale address (port 8080), reachable by peers the tailnet policy allows.
# access.sh publishes how to reach it.
set -euo pipefail
KCFG=$1 CTX=$2
# Reuse the local-cluster install (pinned, digest-checked chart); k3s ships metrics-server.
install -m 0600 "$KCFG" tools/local-cluster/kubeconfig
make -C tools/local-cluster headlamp-install PROVIDER=k3d CONTEXT="$CTX" METRICS_SERVER=false >/dev/null
# A supervised port-forward through the API server: it needs no Service routing from this
# node and follows Headlamp Pod replacements. Addressed by IP, so MagicDNS is optional.
ip="$(tailscale ip -4)"
sudo systemd-run --unit=headlamp-forward -p Restart=always -p RestartSec=2 \
  /usr/local/bin/k3s kubectl --kubeconfig "$KCFG" --context "$CTX" -n headlamp \
  port-forward --address "$ip" service/headlamp 8080:80 >/dev/null
for _ in $(seq 30); do curl -sf -o /dev/null "http://$ip:8080/" && break; sleep 2; done
curl -sf -o /dev/null "http://$ip:8080/" || { echo "dashboard did not answer on $ip:8080" >&2; exit 1; }
echo "dashboard on http://$ip:8080"

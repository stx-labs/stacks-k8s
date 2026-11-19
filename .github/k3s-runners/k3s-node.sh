#!/usr/bin/env bash
# Usage: k3s-node.sh server|agent
# Starts k3s over the tailnet: node IPs are Tailscale addresses and flannel's VXLAN
# runs on tailscale0. Env: TOKEN_SEED (per-run and secret, shared by all jobs of a
# run), optional NODE_NAME (default: this tailnet hostname); agents also need SERVER_IP
# (tailnet address) or SERVER_HOST (tailnet hostname, resolved when SERVER_IP is unset).
set -euo pipefail
role=$1
token="$(printf '%s' "$TOKEN_SEED" | sha256sum | cut -d' ' -f1)"
ip="$(tailscale ip -4)"
# Runner VM hostnames can repeat across jobs; the per-run tailnet hostname is unique.
name="${NODE_NAME:-$(tailscale status --json | jq -r .Self.HostName)}"
args=(--node-name "$name" --node-ip "$ip" --flannel-iface tailscale0)
# Record the runner's Azure region, when the instance metadata service answers.
region="$(curl -sf --max-time 2 -H Metadata:true \
  'http://169.254.169.254/metadata/instance/compute/location?api-version=2021-02-01&format=text' || true)"
args+=(--node-label "ci.stacks.org/region=${region:-unknown}")
stamp() { date -u +%H:%M:%S; }
started=$(date +%s)

case "$role" in
  server)
    args+=(--advertise-address "$ip" --tls-san "$ip" --write-kubeconfig-mode 0644
      --disable traefik --disable servicelb
      --node-taint node-role.kubernetes.io/control-plane:NoSchedule)
    ;;
  agent)
    # Agents may start before the server; wait for its tailnet address and supervisor.
    until server_ip="${SERVER_IP:-$(tailscale ip -4 "$SERVER_HOST" 2>/dev/null)}" &&
      [ "$(curl -sk --max-time 5 "https://$server_ip:6443/ping")" = pong ]; do
      (( $(date +%s) - started < 900 )) || { echo "$(stamp) server $SERVER_HOST unreachable" >&2; exit 1; }
      sleep 5
    done
    echo "$(stamp) server $SERVER_HOST at $server_ip after $(( $(date +%s) - started ))s"
    echo "SERVER_IP=$server_ip" >> "${GITHUB_ENV:-/dev/null}"
    args+=(--server "https://$server_ip:6443")
    ;;
  *) echo "role must be server or agent" >&2; exit 1 ;;
esac

# Type=notify returns once k3s reports ready.
sudo systemd-run --unit=k3s --setenv=K3S_TOKEN="$token" \
  -p Type=notify -p Delegate=yes -p KillMode=process -p TimeoutStartSec=300 \
  -p LimitNOFILE=1048576 -p TasksMax=infinity \
  /usr/local/bin/k3s "$role" "${args[@]}"
echo "$(stamp) k3s $role ready on $ip after $(( $(date +%s) - started ))s"

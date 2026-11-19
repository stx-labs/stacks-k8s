#!/usr/bin/env bash
# Usage: assume-role.sh ELECTED RANK LEADER_IP
# Renames this runner on the tailnet to its elected k3s role for readable names (SERVER_HOST
# or $NODE_PREFIX-agent-RANK), and exports ROLE (server|agent), NODE_NAME and SERVER_IP, the
# elected server's tailnet address, so joining never waits on a renamed hostname.
# Env: NODE_PREFIX, SERVER_HOST.
set -euo pipefail
if [ "$1" = true ]; then
  role=server name=$SERVER_HOST
else
  [[ "$2" =~ ^[1-9][0-9]*$ ]] || { echo "agent rank must be positive, got '$2'" >&2; exit 1; }
  role=agent name=$NODE_PREFIX-agent-$2
fi
sudo tailscale set --hostname "$name"
echo "$name: k3s $role"
{ echo "ROLE=$role"; echo "NODE_NAME=$name"; echo "SERVER_IP=$3"; } >> "$GITHUB_ENV"

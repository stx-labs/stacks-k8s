#!/usr/bin/env bash
# Usage: access.sh [limit-seconds]
# Waits (default 1500s) until the server's dashboard answers over the tailnet, then prints
# how to reach the live cluster as Markdown. Run from a short job: a job summary appears
# only when its job ends, and the server job runs until teardown. The login token is
# minted over Tailscale SSH, never written to logs (the repository is public).
# Env: SERVER_HOST (tailnet hostname), HOLD_RELEASE (ends network.sh's inspection hold).
set -euo pipefail
limit=${1:-1500}
started=$(date +%s)
until ip="$(tailscale ip -4 "$SERVER_HOST" 2>/dev/null)" && curl -sf -o /dev/null --max-time 5 "http://$ip:8080/"; do
  (( $(date +%s) - started < limit )) || { echo "dashboard on $SERVER_HOST did not answer" >&2; exit 1; }
  sleep 10
done
# Runner host keys are ephemeral, and Tailscale already authenticates the node behind the
# address, so these commands neither prompt for nor store them.
ssh="ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR runner@$ip"
cat <<EOF
### Live cluster (tailnet only)

| What | How |
| --- | --- |
| Dashboard | http://$ip:8080 |
| Shell | \`$ssh\` |
| Dashboard token (to clipboard) | \`$ssh sudo k3s kubectl -n headlamp create token headlamp \\| tr -d '\\n' \\| pbcopy\` |
| End a hold early | \`$ssh touch $HOLD_RELEASE\` |

EOF

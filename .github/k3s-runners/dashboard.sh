#!/usr/bin/env bash
# Usage: dashboard.sh KUBECONFIG CONTEXT
# Installs the repository's Headlamp dashboard and serves it over plain HTTP to tailnet
# peers allowed by the tailnet policy, then prints how to reach it and log in. The login
# token is minted over Tailscale SSH, never written to logs (the repository is public).
# Env: HOLD_RELEASE, the file that ends minimal-network.sh's inspection hold.
set -euo pipefail
KCFG=$1 CTX=$2
# Reuse the local-cluster install (pinned, digest-checked chart); k3s ships metrics-server.
install -m 0600 "$KCFG" tools/local-cluster/kubeconfig
make -C tools/local-cluster headlamp-install PROVIDER=k3d CONTEXT="$CTX" METRICS_SERVER=false >/dev/null
# Nodes route ClusterIPs, so Serve keeps working across Headlamp Pod restarts.
ip="$(kubectl --kubeconfig "$KCFG" --context "$CTX" -n headlamp get service headlamp -o jsonpath='{.spec.clusterIP}')"
sudo tailscale serve --bg --http=8080 "http://$ip:80" >/dev/null
host="$(tailscale status --json | jq -r .Self.HostName)"
cat <<EOF
### Live cluster (tailnet only)

| What | How |
| --- | --- |
| Dashboard | http://$host:8080 (http://$(tailscale ip -4):8080) |
| Shell | \`ssh runner@$host\` |
| Dashboard token | \`ssh runner@$host sudo k3s kubectl -n headlamp create token headlamp\` |
| End a hold early | \`ssh runner@$host touch $HOLD_RELEASE\` |

EOF

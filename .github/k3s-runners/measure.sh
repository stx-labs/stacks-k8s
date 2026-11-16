#!/usr/bin/env bash
# Usage: measure.sh AGENTS
# Run on the server once k3s is up. Waits for AGENTS workers, then prints Markdown
# with join timing, Tailscale paths, a pod-to-pod latency matrix and iperf3 throughput.
set -uo pipefail
agents=$1
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
started=$(date +%s)
workers() { kubectl get nodes -l '!node-role.kubernetes.io/control-plane' --no-headers 2>/dev/null | grep -cw Ready; }
until [ "$(workers)" -ge "$agents" ]; do
  if (( $(date +%s) - started > 900 )); then echo "only $(workers)/$agents workers joined within 900s"; break; fi
  sleep 10
done

echo "### Nodes ($(workers)/$agents workers after $(( $(date +%s) - started ))s)"
echo
echo '| Node | Tailscale IP | Region | Ready since |'
echo '| --- | --- | --- | --- |'
kubectl get nodes -o json | jq -r '.items[] | "| \(.metadata.name) | \(.status.addresses[] | select(.type=="InternalIP") | .address) | \(.metadata.labels["ci.stacks.org/region"] // "unknown") | \(.status.conditions[] | select(.type=="Ready") | .lastTransitionTime) |"'

echo
echo '### Tailscale paths from the server'
echo
echo '| Peer | Path | Ping |'
echo '| --- | --- | --- |'
for ip in $(kubectl get nodes -l '!node-role.kubernetes.io/control-plane' -o jsonpath='{.items[*].status.addresses[?(@.type=="InternalIP")].address}'); do
  pong="$(tailscale ping -c 5 --until-direct=false "$ip" 2>&1 | tail -1)"
  path="$(tailscale status --json | jq -r --arg ip "$ip" '.Peer[] | select(.TailscaleIPs | index($ip)) | if .CurAddr != "" then "direct \(.CurAddr)" else "relay \(.Relay)" end')"
  echo "| $ip | $path | ${pong//|/ } |"
done

# One probe Pod per worker; control-plane taint keeps the server out.
kubectl apply -f - >/dev/null <<'EOF'
apiVersion: apps/v1
kind: DaemonSet
metadata: {name: netprobe, namespace: default}
spec:
  selector: {matchLabels: {app: netprobe}}
  template:
    metadata: {labels: {app: netprobe}}
    spec:
      containers:
        - name: probe
          image: alpine:3.20
          command: [sh, -c, "apk add --no-cache iperf3 >/dev/null && exec iperf3 -s"]
          readinessProbe: {tcpSocket: {port: 5201}, periodSeconds: 2}
EOF
kubectl rollout status ds/netprobe --timeout=300s >/dev/null
mapfile -t pods < <(kubectl get pods -l app=netprobe -o jsonpath='{range .items[*]}{.metadata.name},{.status.podIP},{.spec.nodeName}{"\n"}{end}')

echo
echo '### Pod-to-pod RTT (ms, avg of 5)'
echo
printf '| From \\ To |'; for p in "${pods[@]}"; do printf ' %s |' "${p##*,}"; done; echo
printf '| --- |'; for _ in "${pods[@]}"; do printf ' --- |'; done; echo
for a in "${pods[@]}"; do
  printf '| %s |' "${a##*,}"
  for b in "${pods[@]}"; do
    if [ "$a" = "$b" ]; then printf ' - |'; continue; fi
    ip="${b#*,}"; ip="${ip%%,*}"
    rtt="$(kubectl exec "${a%%,*}" -- ping -c 5 -q "$ip" 2>/dev/null | awk -F/ '/round-trip/ {print $4}')"
    printf ' %s |' "${rtt:-fail}"
  done
  echo
done

echo
echo '### Pod-to-pod iperf3 (10 s, TCP)'
echo
echo '| From | To | Mbit/s |'
echo '| --- | --- | --- |'
for i in $(seq 0 $(( ${#pods[@]} - 2 ))); do
  a="${pods[$i]}" b="${pods[$((i + 1))]}"
  ip="${b#*,}"; ip="${ip%%,*}"
  mbps="$(kubectl exec "${a%%,*}" -- iperf3 -c "$ip" -t 10 -J 2>/dev/null | jq -r '.end.sum_received.bits_per_second / 1e6 | floor')"
  echo "| ${a##*,} | ${b##*,} | ${mbps:-fail} |"
done

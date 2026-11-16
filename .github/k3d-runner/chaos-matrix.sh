#!/usr/bin/env bash
# Usage: chaos-matrix.sh KUBECONFIG CONTEXT
# Runs one small instance of each Chaos Mesh fault type against busybox Pods on
# two different worker nodes and prints injected / effect / recovered results.
set -uo pipefail
KCFG=$1 CTX=$2 NS=chaos-matrix
k() { kubectl --kubeconfig "$KCFG" --context "$CTX" "$@"; }
kn() { k -n "$NS" "$@"; }
now() { perl -MTime::HiRes=time -e 'printf "%.3f\n", time'; }
calc() { awk "BEGIN { print $* }"; }
elapsed() { local s e; s=$(now); "$@" >/dev/null 2>&1; e=$(now); calc "$e - $s"; }
rtt() { kn exec src -- ping -c 3 -W 1 -q "$DST" 2>/dev/null | awk -F/ '/round-trip/ {print $4}'; }
# An iptables DROP on egress makes ping fail before it prints a summary.
loss() { kn exec src -- ping -c 3 -W 1 -q "$DST" 2>&1 | awk '/packet loss/ {print $7; found=1} END {if (!found) print "100%"}'; }
fetch() { elapsed kn exec src -- wget -q -O /dev/null -T 10 "http://$DST:8080/index.html"; }
io() { elapsed kn exec src -- sh -c 'for i in 1 2 3 4 5 6 7 8 9 10; do echo x > /data/f$i; cat /data/f$i; done'; }
# TimeChaos affects only PID 1 and its children; clk's PID 1 records time via a bash builtin.
clock_skew() { sleep 2; calc "$(kn exec clk -- cat /tmp/now) - $(date +%s)"; }
# Millicores used by src's cgroup over 5 s; independent of the metrics API.
src_cpu() { kn exec src -- sh -c 'a=$(awk "/usage_usec/ {print \$2}" /sys/fs/cgroup/cpu.stat); sleep 5; b=$(awk "/usage_usec/ {print \$2}" /sys/fs/cgroup/cpu.stat); echo $(( (b - a) / 5000 ))m' 2>/dev/null; }
gt() { [ -n "$1" ] && awk "BEGIN { exit !(($1) > ($2)) }"; }
lt() { [ -n "$1" ] && awk "BEGIN { exit !(($1) < ($2)) }"; }

RESULTS=()
record() { RESULTS+=("$(printf '| %s | %s | %s | %s | %s |' "$@")"); echo "  -> $*"; }

injected() { # kind name -> True/False after up to 60s
  for _ in $(seq 30); do
    s=$(kn get "$1" "$2" -o jsonpath='{.status.conditions[?(@.type=="AllInjected")].status}' 2>/dev/null)
    [ "$s" = True ] && { echo True; return; }
    sleep 2
  done
  echo "False ($(kn get "$1" "$2" -o jsonpath='{.status.experiment.containerRecords[0].events[-1].message}' 2>/dev/null | cut -c1-80))"
}
apply() { kn apply -f - >/dev/null; }
remove() { kn delete "$1" "$2" --wait --timeout=90s >/dev/null 2>&1; sleep 3; }

# Fixtures on two distinct non-control-plane nodes.
read -r NODE_A NODE_B < <(k get nodes -l '!node-role.kubernetes.io/control-plane' -o jsonpath='{.items[*].metadata.name}')
k create namespace "$NS" >/dev/null
k annotate namespace "$NS" chaos-mesh.org/inject=enabled >/dev/null
cat <<EOF | apply
apiVersion: v1
kind: Pod
metadata: {name: src, labels: {app: src}}
spec:
  nodeName: $NODE_A
  containers:
    - {name: main, image: busybox:1.37, command: [sleep, "3600"], volumeMounts: [{name: data, mountPath: /data}]}
  volumes: [{name: data, emptyDir: {}}]
---
apiVersion: v1
kind: Pod
metadata: {name: dst, labels: {app: dst}}
spec:
  nodeName: $NODE_B
  containers:
    - name: main
      image: busybox:1.37
      command: [sh, -c, "mkdir -p /www && echo ok > /www/index.html && exec httpd -f -p 8080 -h /www"]
      ports: [{containerPort: 8080}]
---
apiVersion: v1
kind: Pod
metadata: {name: victim, labels: {app: victim}}
spec:
  nodeName: $NODE_A
  containers: [{name: main, image: busybox:1.37, command: [sleep, "3600"]}]
---
apiVersion: v1
kind: Pod
metadata: {name: clk, labels: {app: clk}}
spec:
  nodeName: $NODE_A
  containers:
    - name: main
      image: debian:bookworm-slim
      command: [bash, -c, "while true; do printf '%(%s)T\\\\n' -1 > /tmp/now; read -t 1 < /dev/zero || true; done"]
EOF
kn wait --for=condition=Ready pod/src pod/dst pod/victim pod/clk --timeout=180s >/dev/null || { echo "fixtures not ready"; exit 1; }
DST=$(kn get pod dst -o jsonpath='{.status.podIP}')
echo "context=$CTX nodes=$NODE_A,$NODE_B dst=$DST"

net() { # name action-spec
  cat <<EOF | apply
apiVersion: chaos-mesh.org/v1alpha1
kind: NetworkChaos
metadata: {name: $1}
spec:
  mode: all
  selector: {namespaces: [$NS], labelSelectors: {app: src}}
  duration: 120s
$2
EOF
}

echo "NetworkChaos delay"
base=$(rtt); net delay "  action: delay
  delay: {latency: 300ms}"
inj=$(injected networkchaos delay); during=$(rtt); remove networkchaos delay; after=$(rtt)
gt "$during" 250 && e=yes || e=no; lt "$after" 50 && r=yes || r=no
record "NetworkChaos delay 300 ms" "$inj" "$e" "$r" "RTT ${base} → ${during} → ${after} ms"

echo "NetworkChaos loss"
net loss "  action: loss
  loss: {loss: '100'}"
inj=$(injected networkchaos loss); during=$(loss); remove networkchaos loss; after=$(loss)
[ "$during" = 100% ] && e=yes || e=no; [ "$after" = 0% ] && r=yes || r=no
record "NetworkChaos loss 100%" "$inj" "$e" "$r" "loss $during → $after"

echo "NetworkChaos partition"
net partition "  action: partition
  direction: both
  target: {mode: all, selector: {namespaces: [$NS], labelSelectors: {app: dst}}}"
inj=$(injected networkchaos partition); during=$(loss); remove networkchaos partition; after=$(loss)
[ "$during" = 100% ] && e=yes || e=no; [ "$after" = 0% ] && r=yes || r=no
record "NetworkChaos partition (cross-node)" "$inj" "$e" "$r" "loss $during → $after"

echo "HTTPChaos delay"
base=$(fetch)
cat <<EOF | apply
apiVersion: chaos-mesh.org/v1alpha1
kind: HTTPChaos
metadata: {name: http}
spec:
  mode: all
  selector: {namespaces: [$NS], labelSelectors: {app: dst}}
  target: Request
  port: 8080
  path: '*'
  delay: 2s
  duration: 120s
EOF
inj=$(injected httpchaos http); during=$(fetch); remove httpchaos http; after=$(fetch)
gt "$during" "$base + 1.5" && e=yes || e=no; lt "$after" "$base + 0.5" && r=yes || r=no
record "HTTPChaos delay 2 s" "$inj" "$e" "$r" "fetch ${base} → ${during} → ${after} s"

echo "IOChaos latency"
base=$(io)
cat <<EOF | apply
apiVersion: chaos-mesh.org/v1alpha1
kind: IOChaos
metadata: {name: io}
spec:
  action: latency
  mode: all
  selector: {namespaces: [$NS], labelSelectors: {app: src}}
  volumePath: /data
  path: '/data/**/*'
  delay: 100ms
  percent: 100
  duration: 120s
EOF
inj=$(injected iochaos io); during=$(io); remove iochaos io; after=$(io)
gt "$during" "$base + 1.5" && e=yes || e=no; lt "$after" "$base + 0.5" && r=yes || r=no
record "IOChaos latency 100 ms" "$inj" "$e" "$r" "20 file ops ${base} → ${during} → ${after} s"

echo "TimeChaos offset"
base=$(clock_skew)
cat <<EOF | apply
apiVersion: chaos-mesh.org/v1alpha1
kind: TimeChaos
metadata: {name: clock}
spec:
  mode: all
  selector: {namespaces: [$NS], labelSelectors: {app: clk}}
  timeOffset: -1h
  duration: 120s
EOF
inj=$(injected timechaos clock); during=$(clock_skew); remove timechaos clock; after=$(clock_skew)
lt "$during" -3590 && e=yes || e=no; lt "${after#-}" 5 && r=yes || r=no
record "TimeChaos offset -1 h" "$inj" "$e" "$r" "PID 1 clock skew ${base} → ${during} → ${after} s"

echo "StressChaos cpu"
base=$(src_cpu)
cat <<EOF | apply
apiVersion: chaos-mesh.org/v1alpha1
kind: StressChaos
metadata: {name: stress}
spec:
  mode: all
  selector: {namespaces: [$NS], labelSelectors: {app: src}}
  stressors: {cpu: {workers: 1, load: 100}}
  duration: 120s
EOF
inj=$(injected stresschaos stress); sleep 5; during=$(src_cpu); remove stresschaos stress; sleep 5; after=$(src_cpu)
gt "${during%m}" 500 && e=yes || e=no; lt "${after%m}" 100 && r=yes || r=no
record "StressChaos cpu 1 worker" "$inj" "$e" "$r" "src CPU ${base} → ${during} → ${after}"

echo "PodChaos container-kill"
before=$(kn get pod src -o jsonpath='{.status.containerStatuses[0].restartCount}')
cat <<EOF | apply
apiVersion: chaos-mesh.org/v1alpha1
kind: PodChaos
metadata: {name: ckill}
spec:
  action: container-kill
  mode: all
  containerNames: [main]
  selector: {namespaces: [$NS], labelSelectors: {app: src}}
EOF
inj=$(injected podchaos ckill); sleep 10
after=$(kn get pod src -o jsonpath='{.status.containerStatuses[0].restartCount}'); remove podchaos ckill
kn wait --for=condition=Ready pod/src --timeout=60s >/dev/null 2>&1 && r=yes || r=no
[ "${after:-0}" -gt "${before:-0}" ] && e=yes || e=no
record "PodChaos container-kill" "$inj" "$e" "$r" "restarts $before → $after"

echo "PodChaos pod-kill"
uid=$(kn get pod victim -o jsonpath='{.metadata.uid}')
cat <<EOF | apply
apiVersion: chaos-mesh.org/v1alpha1
kind: PodChaos
metadata: {name: pkill}
spec:
  action: pod-kill
  mode: all
  selector: {namespaces: [$NS], labelSelectors: {app: victim}}
EOF
inj=$(injected podchaos pkill); sleep 5
now_uid=$(kn get pod victim -o jsonpath='{.metadata.uid}' 2>/dev/null)
[ "$now_uid" != "$uid" ] && e=yes || e=no; remove podchaos pkill
record "PodChaos pod-kill" "$inj" "$e" "n/a" "victim deleted: $e"

echo "DNSChaos error"
cat <<EOF | apply
apiVersion: chaos-mesh.org/v1alpha1
kind: DNSChaos
metadata: {name: dns}
spec:
  action: error
  mode: all
  patterns: [example.com]
  selector: {namespaces: [$NS], labelSelectors: {app: src}}
  duration: 120s
EOF
inj=$(injected dnschaos dns); remove dnschaos dns
record "DNSChaos error" "$inj" "n/a" "n/a" "repository values disable the Chaos DNS server"

k delete namespace "$NS" --wait --timeout=180s >/dev/null
echo
echo "| Fault | Injected | Effect | Recovered | Observation |"
echo "| --- | --- | --- | --- | --- |"
printf '%s\n' "${RESULTS[@]}"

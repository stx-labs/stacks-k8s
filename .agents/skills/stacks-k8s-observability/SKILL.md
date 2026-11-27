---
name: stacks-k8s-observability
description: Record, reach and query a running stacks-k8s network. Install the telemetry operator and GreptimeDB backend, enroll a namespace, create a NetworkTelemetry recording, reach actor RPC endpoints, run preflight checks, query logs, objects and metrics with SQL, capture native protocol facts with stacks-inspect, export a checksummed evidence bundle, and take a one-shot identity snapshot. Use to observe an experiment or diagnose a stalled network.
---

# Observe a stacks-k8s network

Observability is optional and read-only: networks run without it, and nothing here can
change a network. Everything it retains is an observation with explicit gaps, never
proof of complete capture or of protocol health. Run commands from the stacks-k8s root
with `KUBECONFIG="$PWD/tools/local-cluster/kubeconfig"`.

## Quick facts without telemetry

The participant status already carries the actor's identity, endpoints and recent
protocol progress:

```bash
kubectl -n lab-x get stacksnetwork network -o jsonpath='{.status.conditions}'
kubectl -n lab-x get stacksnetworkparticipant -o \
  custom-columns=NAME:.spec.participantName,POD:.status.runtime.podRef.name,IP:.status.runtime.podIP,BURN:.status.runtime.protocol.burnHeight,STACKS:.status.runtime.protocol.highestStacksHeight
kubectl -n lab-x get stacksnetworkparticipant -o \
  jsonpath='{range .items[?(@.spec.participantName=="miner-01")]}{.status.runtime.endpoints}{end}'
```

`WorkloadReady`, `Initialized` and `Operational` are controller reports;
`status.burnchainObservations` on the root helps diagnosis but does not gate anything.
Service names are hashed; find a participant's Pod by label
`network.stacks.org/participant=<name>` and port-forward to it:

```bash
POD=$(kubectl -n lab-x get pod -l network.stacks.org/participant=miner-01,network.stacks.org/role=actor -o name)
kubectl -n lab-x port-forward "$POD" 20443:20443   # Stacks RPC; Bitcoin RPC is 18443
```

Record the Pod UID before and after each forward; a replacement Pod is a different
actor process.

## Install telemetry once per cluster

1. Create the namespace and the `greptime-auth` Secret with a `passwd` key of three
   generated lines, `admin:readwrite=…`, `ingest:writeonly=…` and
   `reader:readonly=…`. Keep the file outside Git.
2. Build, load and install the bundled operator and GreptimeDB backend:

   ```bash
   kubectl create namespace stacks-observation-system
   kubectl -n stacks-observation-system create secret generic greptime-auth --from-file=passwd=/private/passwd
   TAG="dev-$(git rev-parse --short HEAD)-$(date +%s)"
   docker build -f operators/observability/Dockerfile -t "stacks-observability-operator:$TAG" .
   kind load docker-image "stacks-observability-operator:$TAG" --name stacks-k8s
   make -C deploy/telemetry install HELM_ARGS="--set image.repository=stacks-observability-operator \
     --set image.tag=$TAG --set watchNamespaces[0]=lab-a --set watchNamespaces[1]=lab-x"
   ```

**Enrollment is a list, not an addition.** Each install resets values, so pass every
namespace that must stay enrolled. To enroll a namespace on an existing installation,
keep its image instead of rebuilding:

```bash
helm get values telemetry -n stacks-observation-system -a -o json \
  | python3 -c 'import json,sys;v=json.load(sys.stdin);print(v["image"]["repository"],v["image"]["tag"],v["watchNamespaces"])'
make -C deploy/telemetry install HELM_ARGS="--set image.repository=<repository> \
  --set image.tag=<tag> --set watchNamespaces[0]=<existing> --set watchNamespaces[1]=lab-x"
```

Enrolled namespaces must exist. Remove a namespace's
`NetworkTelemetry` before unenrolling it. Telemetry also needs Metrics Server.

The dashboard is at <http://127.0.0.1:14000/dashboard/> (database `public`, user
`reader`).

## Record a network

Create `telemetry-storage` in the experiment namespace, with `stringData.endpoint:
http://greptime.stacks-observation-system.svc:4000` and `stringData.authorization:
Basic <base64(ingest:password)>`. Then:

```yaml
apiVersion: observation.stacks.org/v1alpha2
kind: NetworkTelemetry
metadata: {name: capture, namespace: lab-x}
spec:
  networkName: network
  networkUID: "<root UID>"
  storageSecretRef: telemetry-storage
  retention: {window: 24h}            # 1h, 6h or 24h
  sources: {objects: true, logs: true, metrics: true}
```

Wait for `WorkloadsReady=True` and a recent `status.recording.heartbeatAt`. Note
`status.tablePrefix`: tables are `<prefix>_objects` and `<prefix>_logs`; native metric
tables are shared and filtered by `network_uid` and `telemetry_uid`. Start recording
before the network starts if initialization matters.

## Investigate

| Task | Tool |
| --- | --- |
| Check the recorder, sources, enrollment and Chaos opt-in before an experiment | `tools/stacks-preflight` |
| Query retained rows | `POST /v1/sql` with the reader credential |
| Capture native Stacks tip, PoX and reward-set facts, or Bitcoin hash walks | `tools/stacks-inspect` |
| Export a bounded, checksummed bundle | `tools/stacks-evidence bundle`, then `verify` |
| Prove which images and configuration actors run | `NetworkObservation` |

Request shapes and limits are in [tools.md](references/tools.md).

```bash
curl -s -u "reader:$READER_PASSWORD" http://127.0.0.1:14000/v1/sql \
  --data-urlencode "sql=SELECT timestamp, participant_uid, body FROM ${PREFIX}_logs \
WHERE network_uid = '$NETWORK_UID' AND timestamp >= '$FROM' AND timestamp < '$TO' \
ORDER BY timestamp LIMIT 200"
```

Always filter by network UID and an absolute time window; `LIMIT` alone does not bound
the scan. Start with one minute and one participant, then widen. Tested templates for
inventory, fault timelines, capture gaps, heights, scrape health and resources are in
`docs/observability/queries.sql`; Bitcoin observer queries in
`docs/observability/bitcoin-observer-queries.sql`.

## Read evidence carefully

- A `CaptureGap` row means possible loss or duplication, not an exact count. No gap is
  not proof of uninterrupted capture.
- Faults appear in the journal only when the fault object carries the
  `network.stacks.org/network-uid` label.
- Metric values and log text are actor claims; scrape `up` is separate from progress.
- Raw log counts are not event counts; replay can duplicate lines.
- Set `BitcoinNode.spec.observer.enabled: true` for structured Bitcoin branch and hash
  observations; enable both logs and metrics to keep them.

## Clean up

Deleting `NetworkTelemetry` stops recording but keeps backend tables until their TTL;
export first. `make -C deploy/telemetry uninstall` removes the release and keeps the
database PVC. Leave the shared backend running between experiments.

## Reference

- [tools.md](references/tools.md): preflight, inspect and evidence request shapes
- [networktelemetry.md](references/networktelemetry.md),
  [networkobservation.md](references/networkobservation.md): generated field tables

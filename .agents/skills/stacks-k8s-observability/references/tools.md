# Investigation tools

Standalone read-only Go commands under `tools/` in the stacks-k8s repository. Each reads
one JSON request on stdin. Supply credentials through stdin only; never save a
credential-bearing request with evidence. Full contracts are in each tool's README.

```bash
GOWORK=off go -C tools/stacks-preflight run . < request.json > preflight.json
GOWORK=off go -C tools/stacks-inspect run . stacks < request.json > snapshot.jsonl
GOWORK=off go -C tools/stacks-evidence build -o /tmp/stacks-evidence .
```

## stacks-preflight

Point-in-time `Pass`/`Fail`/`Unknown` per check; any non-pass exits nonzero.

```json
{
  "kubeconfig": "/abs/path/tools/local-cluster/kubeconfig",
  "context": "kind-stacks-k8s",
  "namespace": "lab-x",
  "network": "network",
  "networkUID": "<root UID>",
  "telemetry": "capture",
  "operatorNamespace": "stacks-observation-system",
  "operatorDeployment": "telemetry-stacks-observability-operator",
  "chaos": true,
  "maxAgeSeconds": 60,
  "requiredSources": ["stacksnetworks.network.stacks.org/v1alpha2"],
  "backend": {
    "endpoint": "http://127.0.0.1:14000",
    "authorization": "Basic <base64(reader:password)>",
    "tables": [{"name": "<prefix>_logs", "timeColumn": "timestamp"}]
  }
}
```

Checks the root UID, recording admission and heartbeat, requested sources, enrollment
in the operator's `--watch-namespace`, rollout, the namespace's Chaos opt-in, and
recent rows for the network (15–600s). The kubeconfig must use static credentials;
exec plugins are rejected. A pass does not prove fault admission, per-actor coverage or
protocol health. Choose tables that emit continuously, such as logs.

## stacks-inspect

Stacks: tip, PoX and reward-set facts pinned to one `/v2/info` index block.

```json
{"endpoint": "http://127.0.0.1:20443", "attribution": "<network-uid>/<participant-uid>/<pod-uid>",
 "timeoutSeconds": 30, "cycles": [18, 19]}
```

Omit `cycles` for the current and next cycle; at most eight. `pox.facts` gives
`rewardCycle`, `cycleLength` and signed `blocksUntilPrepare`. A reward set reported
unavailable is a captured fact; `PoXAnchorBlockRequired` is only the node's report.

Bitcoin: bounded header walks, optionally comparing two tips.

```json
{"endpoint": "http://127.0.0.1:18443", "credentials": {"username": "…", "password": "…"},
 "attribution": "<network-uid>/<participant-uid>/<pod-uid>", "timeoutSeconds": 30,
 "blocks": 12, "compareTip": "<block hash>"}
```

Use a Core principal allowed only `getblockchaininfo`, `getblockheader` and `getblock`.
Ancestry `unknown-within-bound` does not prove divergence.

Boundaries: `attribution` is copied, not verified; capture the Pod UID yourself.
Records are independent reads, not an atomic snapshot. Use a new output file per
capture; a failed read exits nonzero and keeps earlier records.

## stacks-evidence

`export` runs one bounded query; `bundle` exports a recording into a new directory.

```json
{
  "endpoint": "http://127.0.0.1:14000",
  "authorization": "Basic <base64(reader:password)>",
  "networkUID": "<root UID>",
  "telemetryUID": "<NetworkTelemetry UID>",
  "tablePrefix": "<status.tablePrefix>",
  "from": "2026-09-15T00:00:00Z",
  "to": "2026-09-15T00:10:00Z",
  "outputDir": "./evidence-run",
  "metrics": ["up", "stacks_node_stacks_tip_height"],
  "windowSeconds": 45,
  "maxQueries": 4000,
  "timeoutSeconds": 300
}
```

```bash
/tmp/stacks-evidence bundle < bundle.json > export-summary.json
/tmp/stacks-evidence verify ./evidence-run
```

- Windows are half-open UTC, at most 24h, ending before invocation. Export a settled
  window, before TTL expiry, that includes network creation if context matters.
- Default budgets are smaller than 24h allows: five tables at 45s windows already need
  about 9,600 queries. Shorten the range, select fewer metrics or raise `maxQueries`
  (max 10,000).
- Exit zero means the traversal finished within budget, not complete evidence.
  `coverage` stays `Unknown`; read `CaptureGap` counts, `contextMissing` and partial
  reasons in `manifest.json`.
- `outputDir` must not exist yet. It receives `manifest.json` (tables, states, row
  counts, gaps, `coverage`), `context.json` (last root, genesis, participant and Pod
  bodies in the window) and one JSON page per query.
- `verify` checks byte integrity of manifest-listed files only; the manifest is
  unsigned.

Keep `stacks-workload` receipts and the network manifest beside the bundle.

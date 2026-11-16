# Local Kubernetes 1.37 qualification

## k3d provider (2026-11-16)

Checked `PROVIDER=k3d` with k3d 5.9.0 (Homebrew), the pinned
`rancher/k3s:v1.37.0-k3s1` image, Docker Desktop on arm64 and the checked-in
`k3d.yaml`. The kind cluster was stopped for the run (shared host port 14000)
and restarted afterwards.

| Check | Result |
| --- | --- |
| `cluster-create` with both add-ons | Passed; three nodes Ready on `v1.37.0+k3s1`, Metrics Server owned by the repository release, Headlamp installed. |
| Bundled components | Traefik, ServiceLB and k3s metrics server absent; `local-path` is the default storage class. |
| Exposure and kubeconfig | API and port 14000 bound to `127.0.0.1`; only `tools/local-cluster/kubeconfig` gained the `k3d-stacks-k8s` context. |
| `cluster-stop`, `cluster-start` | Passed; nodes returned Ready. |
| `cluster-chaos-install` | Passed with the k3s overlay; daemons on all nodes use the containerd runtime at `/run/k3s/containerd`. |
| `NetworkChaos` smoke | 300 ms delay on one Pod: median RTT 0.15 ms → 302 ms → 0.13 ms after deletion. |
| `cluster-destroy` | Passed; no containers or volumes remained, and the kind context was retained. |

k3d creates an empty `~/.config/k3d` directory.

### Fault-type comparison

One instance of each Chaos Mesh 2.8.4 fault type ran against busybox Pods on two
different workers in each provider, with the repository values. Results were
identical:

| Fault | kind | k3d | Notes |
| --- | --- | --- | --- |
| `NetworkChaos` delay, loss, cross-node partition | Effective, recovered | Effective, recovered | RTT ~0.1 ms → 301 ms; 100% loss during loss and partition. |
| `HTTPChaos` 2 s request delay | Effective, recovered | Effective, recovered | Fetch 0.05 s → 2.06 s. |
| `StressChaos` one CPU worker | Effective, recovered | Effective, recovered | Target cgroup 0 → 1000 millicores. |
| `PodChaos` container-kill, pod-kill | Effective | Effective | Restart count +1; Pod replaced. |
| `TimeChaos` −1 h | Injected, no observed effect | Injected, no observed effect | Probe: bash builtin clock in a glibc PID 1. |
| `IOChaos` latency | Not injected | Not injected | Injector `toda` cancelled; daemons log `fail to find device cgroup` for `/dev/fuse`. |
| `DNSChaos` | Not available | Not available | The values then disabled the Chaos DNS server. |

The IO and time results are common to both providers on Docker Desktop arm64,
not k3d gaps. Time injection into a static busybox process hung the daemon call
and blocked later time faults until the Pod and Chaos Mesh were restarted.
`KernelChaos`, `BlockChaos` and `JVMChaos` were not tested. These are smoke
results, not fault qualification.

After the values enabled the DNS server, a kind `DNSChaos` error fault on one
Pod failed resolution of the targeted Service name while
`kubernetes.default` still resolved. The Pod's resolver switched from cluster
DNS to the `chaos-mesh-dns-server` Service and back after deletion; the CoreDNS
ConfigMap was unchanged. k3d was not rerun.

The k3d server was then tainted `NoSchedule`. On recreation, only CoreDNS and
the local-path provisioner ran on it; Headlamp, Metrics Server, the Chaos Mesh
controller, its DNS server and both chaos daemons ran on the agents, matching kind. The fault
comparison above predates the taint.

### Minimal network on k3d

The [minimal example](design/public-api/examples/minimal.yaml) ran on the
tainted k3d cluster, with node names mapped to `k3d-stacks-k8s-agent-0`,
`agent-1` and `server-0`. The operator image was built from `cab994a` plus this
change (`sha256:94a52258697ab07f9b18b01f4b34c5d257c526345ad014d91a3f0488751ba3c1`)
and loaded with `k3d image import`. Actors used `bitcoin/bitcoin:31.1` and the
official 4.0.3 Stacks images, resolving to the same digests as the
[kind qualification](network-operator/public-api-qualification.md#official-403-image-qualification--2026-09-15).

| Stage | Result on 2026-11-16 UTC |
| --- | --- |
| Paused apply | `Resolved=True`; accounts, wallets and participants resolved. |
| Running | Set at 21:53:39; `BitcoinPrepared=True` within 60 s. |
| Initialization | 22:04:14 (634 s): `Initialized`, `Running` and `Operational` True. |
| Placement | Bitcoin and Stacks node on `agent-0`, signer on `agent-1`, workers on `server-0` via the example's toleration; no restarts. |
| Native progress | Over 60 s: Bitcoin 210 → 222, Stacks 173 → 197; `Epoch40`, PoX cycle 10, 1,000,000,000 STX stacked. |
| Disposal | `Stopped` after 61 s; root deleted in 1 s with no participants, PVCs or actor Pods left; namespace removed. |

One transient `VolumeBinding` scheduling conflict on the Bitcoin actor resolved
without intervention. Observability, actions and faults were not exercised on
this network. This is a single smoke run, not k3s qualification.

## Dashboard add-ons (2026-09-08)

Qualified on the existing three-node `stacks-k8s` cluster, Kubernetes 1.37.0
on arm64. Headlamp chart/application 0.45.0 and Metrics Server chart 3.14.0 /
application 0.9.0 used the checked-in values and verified archive checksums.

| Check | Result |
| --- | --- |
| Install and repeated install | Both releases became ready; repeat upgraded the existing releases. |
| Resource metrics | Metrics API available; `kubectl top nodes` returned all three nodes. |
| Dashboard access | `cluster-headlamp HEADLAMP_PORT=18080` served HTML over loopback, HTTP 200. |
| Token and integration | A token from `cluster-headlamp-token`, held only in memory, authenticated Headlamp proxy requests for three nodes, three node metrics and 26 pod metrics. |
| Uninstall, repeated uninstall, reinstall | Passed; Headlamp release resources removed, Metrics Server remained usable, Headlamp reinstalled successfully. |
| Final state | Both add-ons and the cluster left running; test port-forward stopped. |

Rendered Headlamp RBAC binds its ServiceAccount to `cluster-admin`; live
authorization confirms that grant. Token login stays enabled. The metrics API
is consumed through Headlamp's standard integration without a plugin.

`make verify`, `make vuln`, `make docker-check`, Markdown lint and whitespace
checks passed. CLI-substitute tests cover default creation, independent opt-outs,
explicit context selection, external metrics API reuse, download/checksum/readiness
failures, and leaving a created cluster intact after add-on failure. Cluster
creation and stop/start were not repeated live in this pass. Authenticated proxy
responses were checked directly; browser interaction and graph rendering were
not automated. Dependency checks cover repository dependencies, not an upstream
container vulnerability scan.

Local logs: `/tmp/stacks-dashboard-{install,upgrade,uninstall,reinstall}.log`
and `/tmp/stacks-dashboard-{verify,vuln,docker-check}.log`.
See the [usage guide](development.md#dashboard-and-resource-metrics).

## Network and lifecycle qualification (2026-09-07)

Tested 2026-09-07 with standalone kind 0.33.0, Kubernetes 1.37.0,
Docker Desktop on arm64, containerd and kindnet. The checked-in three-node
configuration created `stacks-k8s`; all commands selected its private kubeconfig.
No commands targeted the unrelated `local` cluster.

Cluster lifecycle, installation, Bitcoin actions, native Bitcoin faults and
ordinary Stacks transfers passed. **Stacks recovery after partition expiry
failed** despite completed native cleanup. This is not a blanket compatibility
or protocol-recovery qualification; M0.5 remains open.

`make verify`, image builds, Markdown lint and staged/unstaged whitespace checks
passed. Dependency and container checks passed in the preceding upgrade pass;
this live qualification changed documentation only.

## Inputs

Images were built from the staged Kubernetes upgrade and local-cluster helpers.
The observability image was built but not deployed in these live checks.

| Image | Local content ID |
| --- | --- |
| `stacks-network-operator:verify` | `sha256:f217a4782af22684026de8794de5d67c1ca6380e7aa8471b9fb3fee3783bb089` |
| `stacks-action-operator:verify` | `sha256:49c6216091419074c456093177b745b85e2cc9f2631a191d79b326e3f1d8aeb9` |
| `stacks-transaction-worker:verify` | `sha256:2ff097f8e2b5e7b85cf13ed6b137bde63fd2834bd6b67c9c282ecff953f33f72` |
| `stacks-core-attacknet:a12-normal` | `sha256:5b0283ee442bc02dd73807cc9b70ec94b6fd339b82d3de0d863a76c118272b81` |

Bitcoin used the pinned Core 31.1 image; Chaos Mesh used the checksum-verified
2.8.4 chart and checked-in kind/containerd values. The normal Stacks artifact
and its source-attestation limits are described in the
[initial Stacks qualification](network-operator/stacks-qualification.md#images-and-inputs).

## Live results

| Check | Result |
| --- | --- |
| `cluster-create`, `cluster-stop`, `cluster-start` | Passed; three nodes returned Ready and the local kubeconfig remained usable. |
| `cluster-chaos-install`, repeated | Passed; the same release advanced from revision 1 to 2. |
| Independent action controller restart | Passed; two receipts retained until lifecycle acknowledgement. |
| Finite Bitcoin generation | Passed; counts, baseline exclusion, latest policy and idle worker restart verified. |
| Bitcoin reorganization | Passed in correctly provisioned `local-reorg`; higher-work replacement and compensated deadline verified. |
| Native delay across workers | Passed cancellation and expiry, including Chaos controller restart; median RPC 58.74 ms → 1060.34 ms → 52.84 ms. |
| Native Bitcoin partition across workers | Passed cancellation and expiry; bidirectional RPC loss, independent production and heavier-chain convergence verified. |
| Stacks bootstrap and transfers | Passed; four exact inclusions, pause/resume, cadence edit and pending-worker replacement verified. |
| Stacks/Bitcoin partition cancellation | Passed; confirmed transfers advanced 26 → 27 after cleanup. |
| Stacks/Bitcoin partition expiry | Failed its 120-second protocol recovery wait; native cleanup passed. |
| Strictly ordered final cleanup | Passed in fresh `local-cleanup`: producers drained, network and ledgers removed, namespace confirmed absent, Chaos Mesh uninstalled, then cluster destroyed. |

The live suites are `TestLiveIndependentActionOperator`,
`TestLiveFiniteGeneration`, `TestLiveReorganizationAndCompensatedDeadline`,
`TestLiveNativeDelay`, `TestLiveBitcoinPartition`, `TestLiveStacksTransfers`, and
`TestLiveStacksBitcoinPartition`. Commands and fixture requirements remain in the
[operator](action-operator/qualification.md) and [native-fault](chaos/operations.md) guides.

## Stacks recovery limit

The fresh baseline advanced from 25 to 26 confirmed transfers before injection.
The 45-second expiry fault UID was `97f92088-62b0-4271-acb8-d056daf9b5db`.
At the recovery timeout Core was at height 320; both Stacks nodes reported burn
height 300. Transfers remained at 27 confirmed, with nonce 27 outstanding and
TxID `41f2ad57f3e8b0ca09859b76458a5dafb97521f3e8a3414b3a58416a4dde6a8b`.
Miner logs reported a missing canonical PoX anchor for cycle 15 while processing
burn block 301. No native faults remained. No test condition was relaxed.

The [earlier 1.36.1 run](chaos/partition-qualification.md#repeat-run-recovery-limit)
showed the same class of symptom at another reward cycle. Neither observation
establishes the cause or attributes it to Kubernetes 1.37.

## Execution corrections and evidence limits

The initial `local-actions` manifest omitted `--reorganization`. Its reorg request
correctly stayed unadmitted and expired; the combined run therefore failed.
The same suite passed in fresh `local-reorg` with the required RPC grants.
Its two preceding generation/lifecycle tests had passed in `local-actions`.

The initial teardown removed every network's ledgers and actor leaves, but
cluster deletion began before the final `local-stacks` namespace wait completed.
That wait failed. A fresh productive `local-cleanup` fixture separately passed
strictly ordered removal. Both test clusters are removed; the final Docker
inventory contains no `stacks-k8s` nodes.

Logs also contain a non-failing test-client logger initialization warning and
denied Bitcoin-worker leader-election Event writes. They are not evidence of
failed leader election; controller progress was observed.

Public API snapshots and actor/test logs are retained locally under
`~/Code/stacks-k8s-evidence/20260907-local-cluster-137-public/`, with SHA-256 checksums.
That archive excludes Secrets, private provisioning manifests, images and PVCs;
it is diagnostic evidence, not a restorable failed fixture. Private manifests
remain under `/tmp/stacks-k8s-local-live/`, outside Git. Raw run logs use the
`/tmp/stacks-k8s-local-live-*` and `/tmp/stacks-k8s-local-cleanup-*` prefixes.

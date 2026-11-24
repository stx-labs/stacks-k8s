# Native Chaos Mesh faults

Chaos Mesh is optional. Agents with authority over a disposable experiment namespace
create native Chaos Mesh faults directly; stacks-k8s does not install a fault
profile, impose a fault quota or grant experiment-agent permissions. The network
and action operators neither depend on Chaos Mesh nor read, pause or delete faults.
Cross-kind combinations, `Schedule` and `Workflow` remain native Chaos Mesh APIs
that the agent uses as it chooses.

## Install and enroll

On the repository's `stacks-k8s` kind cluster, run:

```bash
make cluster-chaos-install
```

This installs the upstream Chaos Mesh 2.8.4 chart, checksum-verified, with
[`upstream-values.yaml`](../../examples/chaos/upstream-values.yaml) (containerd
runtime, namespace filtering on, dashboard and DNS server off). Enable injection on
each experiment namespace before creating faults:

```bash
kubectl --kubeconfig tools/local-cluster/kubeconfig --context kind-stacks-k8s \
  annotate namespace chaos-live chaos-mesh.org/inject=enabled
```

Other clusters need a compatible Chaos Mesh installation and the same namespace
opt-in when upstream namespace filtering is enabled. Select Kubernetes credentials
appropriate to the agent's granted authority; stacks-k8s does not provision an
experiment-agent ServiceAccount. Confirm the context and namespace before creating
or deleting a fault.

## Select and observe

Start with the [delay](../../examples/chaos/network-delay.yaml) or
[partition](../../examples/chaos/network-partition.yaml) example. Substitute the
current root and participant UIDs, inspect the selected Pods and server-dry-run
the resulting manifest. Replace the example's namespace and actor names too.
For actor-specific faults, prefer selectors containing
`network.stacks.org/network-uid`, `network.stacks.org/participant-uid` and
`network.stacks.org/role: actor`. Support workers and configuration Jobs may
share the first two labels, so a selector without the role also matches them.
These are targeting conventions, not admission constraints. An agent may
intentionally target workers, observers, other Pods or nodes when its experiment
calls for it. A worker fault can end managed operation
and leave submission uncertainty; an observer fault can create evidence gaps.
Faulting a node or a shared host can affect more than the selected network.

Label the **fault object** with `network.stacks.org/network-uid` for recorder
attribution in the network namespace. Add `actions.stacks.org/correlation-id`
as a search hint. Selector labels alone do not establish journal attribution. The
recorder watches the namespaced Chaos Mesh 2.8.4 fault and orchestration kinds
listed in [observability](../observability/README.md); a missing CRD or failed
watch is reported as a source gap. Kubernetes Events, native status (such as
`AllInjected`) and recorded objects are observations, not proof that packets, CPU,
disk or protocol behavior changed as intended. Capture the selected Pod UIDs and
measure before, during and after the intervention. Replacements can match the same
labels, and a selector that matches nothing is a valid fault with no effect.

Record with each measurement the Kubernetes version, node OS, architecture, kernel,
container runtime, CNI, Chaos Mesh version, selected Pod UIDs, native status and
measured effect. IO and time faults also depend on the platform's storage and clock
mechanisms.

### Control paths

The network operator keeps reconciling declared topology during faults; a fault
does not retain or recreate a removed participant.

An experiment that needs production and observation to continue must preserve
worker-to-actor RPC, worker-to-Kubernetes-API and consensus support paths. A fault
that removes the producer's control path tests a different hypothesis. Separate
Services do not isolate traffic to one Pod; measure the actual CNI and runtime paths.
Broad Pod, node or IO faults may remove that access. Exclude worker-hosting nodes
from actor-only host faults; kind nodes share one physical host.

### Side effects

- A 500 ms `NetworkChaos` delay raised median actor-pair HTTP round trips to about
  1.1–1.5 s (from 6–100 ms): connection setup and the response each pay the delay.
  Size delays from measured round trips, not the configured latency.
- `direction: to` on a delay can affect the selected actor's RPC traffic as
  well as peer traffic. An unselected peer can bridge a pairwise partition.
- Partitioning a miner from its Bitcoin node makes its reported burn height lag
  while other nodes advance. It may keep proposing from existing tenure state, so a
  partition does not prove that mining stopped. Neither does an RPC client timeout.
- An actor-wide partition can interrupt external metric scrapes and the in-Pod
  Bitcoin observer sidecar. The observer and Greptime run in
  `stacks-observation-system`; faulting that namespace can disrupt recording
  for every network.
- A Stacks node whose RPC stalls under a fault leaves its Services after about 15
  seconds of failed readiness probes, cutting off peers that use them; see
  [readiness gating](../network-operator/operations.md).
- The recorder journals attributable fault objects in its enrolled network
  namespace. Fault objects created elsewhere are outside that network's journal.
- To freeze an actor process, send `SIGSTOP`/`SIGCONT` to its PID as seen from the
  hosting kind node (`crictl inspect` → `info.pid`). `kill -STOP 1` inside the
  container has no effect, because PID-namespace init ignores it.

For example:

```bash
# Set these from the current StacksNetwork and StacksNetworkParticipant objects.
export NETWORK_UID SOURCE_PARTICIPANT_UID TARGET_PARTICIPANT_UID
envsubst '${NETWORK_UID} ${SOURCE_PARTICIPANT_UID} ${TARGET_PARTICIPANT_UID}' \
  < examples/chaos/network-delay.yaml > /tmp/current-network-delay.yaml
kubectl --kubeconfig tools/local-cluster/kubeconfig --context kind-stacks-k8s \
  create --dry-run=server -f /tmp/current-network-delay.yaml
kubectl --kubeconfig tools/local-cluster/kubeconfig --context kind-stacks-k8s \
  create -f /tmp/current-network-delay.yaml
kubectl --kubeconfig tools/local-cluster/kubeconfig --context kind-stacks-k8s \
  -n chaos-live get networkchaos actor-delay -o yaml
```

The examples are scoped to one actor pair. Native Chaos Mesh supports other
fault kinds and selectors; their effects and cleanup behavior need separate
measurement. Only actor-pair `NetworkChaos` delay and partition effects have been
measured; other kinds and modes are unmeasured.

## Recovery and cleanup

Fault objects are user-owned, not children of the `StacksNetwork`. Pausing the
network does not pause a fault or its duration and cleanup clocks, and deleting the
network neither waits for nor deletes faults. A surviving fault keeps its old UID
selectors and stays active until it is deleted or recovers.

Delete faults and wait for finalized deletion before removing the namespace
annotation, controller or daemon. Duration expiry requests recovery, but it is
not a hard deadline when the upstream cleanup path is unavailable. Check actual
traffic or protocol recovery independently; restoring a network link does not
roll back chain changes. Preserve the fault UID, status, selected Pod identities
and measurements externally before deleting the network or namespace.

```bash
kubectl --kubeconfig tools/local-cluster/kubeconfig --context kind-stacks-k8s \
  -n chaos-live delete networkchaos actor-delay --wait=true --timeout=90s
```

For Bitcoin network teardown, follow the
[network cleanup guide](../network-operator/operations.md).

## Opt-in live checks

`tools/chart-policy/internal/integration` holds live tests behind the `live` build
tag. All need `STACKS_CHAOS_KUBECONFIG` and `STACKS_CHAOS_CONTEXT`.

`TestLiveCurrentActorSelectors` checks current v1alpha2 actor identities with a
native selector dry-run and injects no fault. It also needs
`STACKS_CHAOS_IDENTITY_LIVE=1`, `STACKS_CHAOS_NAMESPACE`, `STACKS_CHAOS_SOURCE` and
`STACKS_CHAOS_TARGET`.

`TestLiveBitcoinPartition`, `TestLiveStacksBitcoinPartition` and
`TestLiveProducerControlLoss` read `bitcoin.stacks.org` `BitcoinProductionTarget`
objects, which no chart serves. They do not run against current installations and
do not cover fault behavior in the current runtime.

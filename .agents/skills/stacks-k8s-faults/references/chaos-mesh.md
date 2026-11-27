# Chaos Mesh faults

Native Chaos Mesh objects created directly by the agent. stacks-k8s installs no fault
profile, quota or agent Role, and its operators do not depend on Chaos Mesh. Use the
Kubernetes credentials the agent was granted for the experiment namespace.

## Prerequisites

```bash
make cluster-chaos-install    # once per cluster; pinned Chaos Mesh 2.8.4
kubectl annotate namespace lab-x chaos-mesh.org/inject=enabled
```

The controller filters namespaces: keep the annotation until every fault in the
namespace has finished recovering.

## Select targets

Every actor Pod carries these labels. Support Pods (workers, configuration Jobs) share
the network and participant labels but have `role: support`.

| Label | Value |
| --- | --- |
| `network.stacks.org/network-uid` | Root UID |
| `network.stacks.org/participant-uid` | Generated `StacksNetworkParticipant` UID |
| `network.stacks.org/participant` | Participant name from the root list |
| `network.stacks.org/participant-kind` | `BitcoinNode`, `StacksNode` or `StacksSigner` |
| `network.stacks.org/actor` | Participant name; actors only |
| `network.stacks.org/role` | `actor` or `support` |

For actor faults select network UID, participant UID and `role: actor`. UIDs pin one
incarnation; names alone can match a same-named participant of a later network.
Selecting workers, observers or nodes is allowed when the hypothesis needs it.

## Attribute the fault

Put `network.stacks.org/network-uid` in the **fault object's** `metadata.labels`;
telemetry journals only faults labelled that way (selector labels do not count). Add
`actions.stacks.org/correlation-id` as a free-form search hint.

## Example: partition two actors

```yaml
apiVersion: chaos-mesh.org/v1alpha1
kind: NetworkChaos
metadata:
  name: partition-miner-signer
  namespace: lab-x
  labels:
    network.stacks.org/network-uid: "${NETWORK_UID}"
    actions.stacks.org/correlation-id: miner-signer-split
spec:
  action: partition        # or delay, loss, duplicate, corrupt, bandwidth
  mode: one
  direction: both
  duration: 20s
  selector:
    namespaces: [lab-x]
    labelSelectors:
      network.stacks.org/network-uid: "${NETWORK_UID}"
      network.stacks.org/participant-uid: "${SOURCE_UID}"
      network.stacks.org/role: actor
  target:
    mode: one
    selector:
      namespaces: [lab-x]
      labelSelectors:
        network.stacks.org/network-uid: "${NETWORK_UID}"
        network.stacks.org/participant-uid: "${TARGET_UID}"
        network.stacks.org/role: actor
```

For a delay, set `action: delay` and add `delay: {latency: 500ms, jitter: 0ms,
correlation: "0"}`. The repository has qualified examples in `examples/chaos/`.

```bash
envsubst '${NETWORK_UID} ${SOURCE_UID} ${TARGET_UID}' < fault.yaml > /tmp/fault.yaml
kubectl create --dry-run=server -f /tmp/fault.yaml
kubectl create -f /tmp/fault.yaml
kubectl -n lab-x get networkchaos partition-miner-signer -o yaml   # record UID and status
```

## Side effects

- `direction: to` on a delay also slows the selected actor's RPC replies.
- An unselected third peer can bridge a pairwise partition; partition every path or
  select groups.
- Partitioning an actor interrupts metric scrapes and the in-Pod Bitcoin observer, so
  telemetry shows gaps for it.
- Faults against `stacks-observation-system` disrupt recording for every network.
- Replacement Pods can match the same selector; record selected Pod UIDs.

Only `NetworkChaos` delay and partition have repository qualification records, from
an earlier runtime. Other kinds and modes need their effects measured.

## Recover and clean up

```bash
kubectl -n lab-x delete networkchaos partition-miner-signer --wait=true --timeout=90s
```

Wait for the deletion to finish before removing the namespace annotation, the network
or Chaos Mesh. Save the fault UID, status, selected Pod UIDs and measurements first.

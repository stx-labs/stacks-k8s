# Network operations

Install the [network chart](../../charts/stacks-network-operator/README.md) once.
Use a fresh namespace for each independent experiment, load the selected actor
images, and apply reusable inputs together with `StacksNetwork/network`.
The [30-actor example](../api/examples/30-actors.yaml) sets `operation: Paused`
so images, placement and resolved inputs can be inspected before genesis capture.

## Desired operation

```bash
kubectl -n lab-30 patch stacksnetwork network --type=merge \
  -p '{"spec":{"operation":"Running"}}'
kubectl -n lab-30 get stacksnetwork network -o yaml
kubectl -n lab-30 get stacksnetworkparticipants
```

`Paused` cooperatively stops managed sends while preserving worker processes.
It does not stop actor consensus or networking. Set `Running` to resume.
`Stopped` is terminal for that network: workers drain and actors scale to zero
with confirmed termination. A stopped network cannot be resumed; create a new
root after deletion. Participant-level controls are workload-specific; see
[lifecycle controls](../api/lifecycle.md).

Reapplying a manifest also reapplies its `operation`; a manifest containing
`Paused` pauses an already running network. Edit the desired declaration rather
than modifying generated Pods, configurations or admitted status directly.

## Readiness

`WorkloadReady` describes Kubernetes workloads. `Initialized` records completion
of frozen protocol gates. `Operational` requires fresh native chain/transaction
progress and required contract postconditions. Independent attributed Bitcoin/miner
height samples in `status.burnchainObservations` aid diagnosis without gating Operational.
A Ready Pod is not proof of consensus progress. Examine root conditions, participant
admission/workload/execution, Bitcoin records and worker logs when initialization stops
advancing. Initialization time depends on the Bitcoin cadence; see
[Bitcoin production](bitcoin-production.md#cadence-and-targets). The
[runtime limits](runtime.md#limits) describe known stalls.

Stacks management-worker loss fails the experiment. Preserve relevant evidence,
then recreate the network. No replacement worker guesses a nonce or resends an
uncertain transaction. Bitcoin ambiguity closes the affected target; it does not
claim that an interrupted RPC stopped executing. See
[execution records](bitcoin-production.md#execution-records).

Actor Pods validate their rendered configuration in a `config-check` init container
that discards the native command's output, so its logs are empty when validation fails.
A common cause is an operator checkout older than the selected actor image: run the
operator from current `main` or a release that supports that image.

## Reaching actors

Service names are hashed per participant incarnation. Read actual addresses from the
generated participant rather than guessing names:

```bash
kubectl -n lab-30 get stacksnetworkparticipants -o \
  custom-columns=NAME:.spec.participantName,POD:.status.runtime.podRef.name,IP:.status.runtime.podIP
kubectl -n lab-30 get stacksnetworkparticipant <generated-name> \
  -o jsonpath='{.status.runtime.endpoints}'
```

`status.runtime.endpoints` lists each Service host and port (`rpc` and `p2p` for
nodes). Actor Pods also carry `network.stacks.org/participant=<name>` and
`network.stacks.org/role=actor`, which select one participant's Pod for a
port-forward. Record the Pod UID with every capture; a replacement Pod is a new process.

### Readiness gating

Stacks node `rpc` and `p2p` Services route only to Ready Pods. The readiness probe is
`GET /v2/info` every 5 seconds with a 3-second timeout and the default three failures,
so a node whose RPC handling stalls for about 15 seconds leaves both Services. Peers
and external clients using the Service then see refused connections, which production
nodes without such a gate would not. Bitcoin `p2p` Services publish not-ready
addresses. Reach a stalled Stacks node directly at `status.runtime.podIP`, port 20443
(RPC) or 20444 (P2P), and account for the gating when attributing lost connectivity.

### Suspending an actor process

To freeze an actor process, for example to simulate a Core RPC outage, send `SIGSTOP`
and later `SIGCONT` to its host PID from the kind node running the Pod. `kill -STOP 1`
inside the container has no effect: a PID-namespace init ignores signals it does not
handle.

```bash
pod=<actor-pod>
node=$(kubectl -n lab-30 get pod "$pod" -o jsonpath='{.spec.nodeName}')
cid=$(kubectl -n lab-30 get pod "$pod" \
  -o jsonpath='{.status.containerStatuses[?(@.name=="bitcoin")].containerID}')
pid=$(docker exec "$node" crictl inspect --output go-template \
  --template '{{.info.pid}}' "${cid#containerd://}")
docker exec "$node" kill -STOP "$pid"   # resume: kill -CONT "$pid"
```

Use container `stacks-node` or `stacks-signer` for Stacks actors. Suspension creates no
Kubernetes object, so record its timing yourself.

## Removal and cleanup

Removing a participant entry destroys its runtime; its name cannot be reused
within that root. Collect logs and ephemeral evidence before removal. PVC survival
depends on admitted storage retention; observability is optional.

Remove native faults and wait for their controller cleanup before destroying the
network. Then stop and delete the root before deleting the namespace:

```bash
kubectl -n lab-30 patch stacksnetwork network --type=merge \
  -p '{"spec":{"operation":"Stopped"}}'
kubectl -n lab-30 wait stacksnetwork/network \
  --for=jsonpath='{.status.phase}'=Stopped --timeout=5m
kubectl -n lab-30 delete stacksnetwork network --wait=true --timeout=5m
kubectl -n lab-30 get stacksnetworkparticipants,pods,persistentvolumeclaims
```

Review retained PVCs before deleting the namespace. Namespace deletion also
removes reusable accounts and definitions. Keep the local kind cluster running;
uninstall per-experiment action or observability releases after their resources
have finished cleanup. Do not remove finalizers to manufacture successful shutdown.

## Recovery after node loss

When kind node containers are killed or restarted during initialization, for example
by the Docker VM's out-of-memory killer, restart them with `make cluster-start` and
check every network. Actor Pods can then fail in their init container. Termination of
a process that never started is not yet confirmed, so the
`network.stacks.org/actor-termination` finalizer holds those Pods: `Stopped` never
completes and a deleted root stays `Destroying`.

There is no supported recovery for such a network. Do not remove finalizers.
Record the namespace and its state as abandoned, leave it in place, and continue in a
fresh namespace with a new root. Recreating the cluster removes abandoned namespaces.

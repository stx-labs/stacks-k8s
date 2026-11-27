---
name: stacks-k8s-faults
description: Apply and remove bounded interventions on a running stacks-k8s network. Covers Chaos Mesh network, Pod and stress faults against selected actors; on-demand Bitcoin blocks and local reorganizations; temporary block-cadence overrides; suspending actors, pausing workers and toggling mining. Use when an experiment needs to perturb a network and measure the effect and recovery.
---

# Intervene in a stacks-k8s network

The agent plans and sequences the experiment; each intervention is one small, bounded,
separately observable object. Nothing in the cluster replays a scenario or judges an
outcome. A created, admitted or completed intervention proves controller action only:
measure the actual actor and protocol effect yourself, before, during and after.

## Choose an intervention

| Goal | Mechanism | Reference |
| --- | --- | --- |
| Delay, loss, partition between actors | Chaos Mesh `NetworkChaos` | [chaos-mesh.md](references/chaos-mesh.md) |
| Kill or fail an actor Pod; CPU, IO or clock faults | Chaos Mesh `PodChaos`, `StressChaos`, `IOChaos`, `TimeChaos` | [chaos-mesh.md](references/chaos-mesh.md) |
| Stop an actor and keep its storage | Root entry `control.suspended: true` | This page |
| Hold a stacker, contract, traffic or production worker | Root entry `control.paused: true` | This page |
| Stop a miner mining | `StacksNode.spec.mining.enabled: false` | This page |
| Mine N Bitcoin blocks now | `BitcoinBlockGeneration` | [bitcoin-actions.md](references/bitcoin-actions.md) |
| Replace the last 1–6 Bitcoin blocks | `BitcoinReorganization` | [bitcoin-actions.md](references/bitcoin-actions.md) |
| Change block cadence for up to 1h | `BitcoinBlockScheduleOverride` | `stacks-k8s-topology` skill |
| Transaction load | `tools/stacks-workload` | `stacks-k8s` skill |
| Run a patched actor | Participant image override | `stacks-k8s-images` skill |

## Time faults against PoX

Compute the schedule before choosing a fault window. One reward cycle lasts
`rewardCycleLength` Bitcoin blocks, the last `prepareLength` of which are the prepare
phase. With the examples' 20/5 lengths and 5s blocks, a cycle is 100s and a prepare
phase only 25s.

A fault that covers a whole prepare phase can leave the next cycle without a canonical
anchor block. Nodes then log a missing PoX anchor and the chain may stay halted after
the fault ends. That is stacks-core behavior, not an environment failure: avoid it
unless it is the subject, and report it separately from the effect under test.

- Read the current cycle and blocks until the prepare phase with `stacks-inspect`
  (`pox.facts`) or a node's `/v2/pox` before applying a fault.
- For longer fault windows, create the network with larger `genesis.pox` lengths or a
  slower block cadence. Both are frozen at creation and lengthen initialization.

## Workflow

1. **Identify targets.** Read the root UID and each target participant's UID, Pod and
   current protocol facts:

   ```bash
   kubectl -n lab-x get stacksnetwork network -o jsonpath='{.metadata.uid}'
   kubectl -n lab-x get stacksnetworkparticipant -o \
     custom-columns=NAME:.spec.participantName,UID:.metadata.uid,POD:.status.runtime.podRef.name,IP:.status.runtime.podIP
   ```

2. **Record a baseline**, then emit a phase Event (see the `stacks-k8s` skill).
3. **Server-dry-run** the intervention, then create it with the network UID label on
   the object itself, so telemetry attributes it.
4. **Measure during** the window: protocol facts, logs and metrics from the affected
   and unaffected actors.
5. **Remove** it and wait for deletion to finish; expiry requests recovery but is not
   a hard deadline.
6. **Measure recovery** independently. Restored links never roll back chain changes.

## Participant controls

Edit the root's participant entry; the controllers act on the declared state.

```bash
kubectl -n lab-x get stacksnetwork network -o jsonpath='{.spec.participants[*].name}'
# follower-01 is the 7th name, so index 6:
kubectl -n lab-x patch stacksnetwork network --type=json -p \
  '[{"op":"add","path":"/spec/participants/6/control","value":{"suspended":true}}]'
# resume:
kubectl -n lab-x patch stacksnetwork network --type=json -p \
  '[{"op":"remove","path":"/spec/participants/6/control"}]'
```

Look up the index immediately before patching; a merge patch would replace the whole
participants list. `suspended` scales `BitcoinNode`, `StacksNode` or
`StacksSigner` to zero with storage retained. `paused` cooperatively holds
`StacksStacker`, `StacksContractSet`, `StacksTransactionProduction` and
`BitcoinBlockProduction`. Losing RPC control of a worker never proves its actor
stopped. Root `operation: Paused` stops managed sends network-wide but not consensus.

## Experiment realism

- Stacks node P2P and RPC Services route only to Ready Pods. The readiness probe is
  `GET /v2/info` every 5s with a 3s timeout, so a node whose RPC stalls for about 15s
  drops out of both Services, cutting peers off in a way production nodes are not.
  Bitcoin P2P Services publish not-ready addresses. To reach a stalled node, use
  `status.runtime.podIP` with port 20443 (RPC) or 20444 (P2P).
- Faults against workers, collectors, `stacks-observation-system` or a whole node
  can end managed operation or create evidence gaps for every network.

## Reference

- [chaos-mesh.md](references/chaos-mesh.md): enrollment, selector labels, attribution,
  example manifest, side effects, cleanup
- [bitcoin-actions.md](references/bitcoin-actions.md): installing the action operator,
  filling and observing requests
- [bitcoinblockgeneration.md](references/bitcoinblockgeneration.md),
  [bitcoinreorganization.md](references/bitcoinreorganization.md): generated field tables

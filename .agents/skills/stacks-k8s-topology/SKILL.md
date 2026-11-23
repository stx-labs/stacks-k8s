---
name: stacks-k8s-topology
description: Author, review or change a stacks-k8s StacksNetwork topology. Choose participants, wire Bitcoin and Stacks resources by reference, set images, placement, genesis and epochs, and validate the manifest before activation. Use when writing or editing a network manifest or diagnosing Resolved=False; for experiments on a running network use the stacks-k8s skill.
---

# Compose a stacks-k8s network

A namespace holds one root, `StacksNetwork/network`, plus the independent resources it
selects. The root's `spec.participants` list names runtime instances; the operator
generates one `StacksNetworkParticipant` per entry and owns all workloads.

| Layer | Kinds | Referenced by |
| --- | --- | --- |
| Identity | `StacksAccount`, `BitcoinWallet` | Resource name |
| Schedules | `StacksEpochSchedule`, `BitcoinBlockSchedule` | Resource name |
| Participant definitions | `BitcoinNode`, `StacksNode`, `StacksSigner`, `StacksStacker`, `BitcoinBlockProduction`, `StacksTransactionProduction`, `StacksContractSet`, `StacksFaucet` | Entry `definition.ref`, or inline |
| Root | `StacksNetwork` | — |
| Requests | `StacksFaucetRequest`, `BitcoinBlockScheduleOverride` | `spec.networkUID` plus participant name |
| Generated | `StacksNetworkParticipant`, `StacksGenesis`, `BitcoinExecution`, `BitcoinInitialization` | Never applied by users |

## Rules that cause most mistakes

- **Two name spaces.** Wiring fields (`bitcoinNodeRef`, `nodeRef`, `signerRef`,
  `targetNodeRef`, peer `nodeRefs`, production `targets`) name **participants** in the
  root's list. Account, wallet, schedule and `definition.ref` fields name **resources**.
  The examples reuse each definition's name as its participant name; when they differ,
  wire by participant name. See [graph.md](references/graph.md).
- **Root inputs are frozen at creation.** `profile`, epochs, `genesis` and `defaults`
  (images, storage, resources, placement) are immutable. Decide images before applying;
  change one actor later with a participant `overrides` image.
- **Precedence** is root `defaults` → definition → participant `overrides`, then profile
  fallbacks. Objects merge recursively; lists replace whole.
- **Every spending account needs genesis funds.** Holders, administrators, miner keys,
  the contract deployer and the traffic account appear in `genesis.allocations`; the
  faucet funds itself through `genesisBalanceMicroSTX`.
- **Removal is destructive and names are single-use.** Removing an entry destroys its
  runtime; a replacement needs a new participant name.

## Workflow

1. **Pick a recipe.** Copy the closest example from [recipes.md](references/recipes.md).
   For miner and signers in separate processes, start from `separate-roles.yaml`.
2. **Adapt it.** Rename the namespace in every document. Set
   `spec.defaults.images` to images already loaded into the cluster. Match every
   `placement.nodeSelector` and `defaults.workerPlacement` to `kubectl get nodes`.
   Keep `operation: Paused`.
3. **Check the required cohort.** The `regtest-pox4-pox5-v1` profile requires at least
   one `BitcoinNode`; one enabled miner `StacksNode` whose wallet is loaded by its
   Bitcoin node and listed in production `initialization.minerWalletRefs`; at least one
   `StacksSigner` with a `StacksStacker`; and exactly one each of
   `BitcoinBlockProduction`, `StacksTransactionProduction` and `StacksContractSet`.
   `StacksFaucet` is optional.
4. **Validate the schema.** Server dry-run needs the namespace to exist first, because
   a dry-run never persists the manifest's own `Namespace`:

   ```bash
   kubectl create namespace lab-x
   kubectl apply --dry-run=server -f network.yaml
   ```

   This checks schema and CEL rules only, not references.
5. **Resolve while paused.** Apply, then wait for `Resolved=True` and a non-empty
   `status.inputDigest`. `ResolutionError` names the unresolved input; correct it and
   reapply. Inspect `kubectl -n lab-x get stacksnetworkparticipants`.
6. **Activate.** Finish building and loading actor images first: an image build on the
   same Docker VM can starve kind during initialization. Then patch
   `spec.operation` to `Running`. Genesis freezes and initialization starts; wait for
   `Initialized=True` and then `Operational=True`.

Reapplying a manifest also reapplies `operation`, so a manifest that still says
`Paused` pauses a running network. Set `Stopped` only to shut down; it is terminal.

## Changing a running network

| Change | How |
| --- | --- |
| Actor image, resources | Participant `overrides` or the definition; rolls that actor only |
| Mining on/off, miner fee rate | `StacksNode.spec.mining`; rolls that node |
| Stake amount, lock cycles | `StacksStacker` spec; applies at next enrollment |
| Block cadence, targets | `BitcoinBlockProduction` targets/schedule, or a bounded `BitcoinBlockScheduleOverride` |
| Traffic amount, fee, interval | `StacksTransactionProduction` spec |
| Suspend or pause one participant | Entry `control.suspended` (actors) or `control.paused` (workers) |
| Node/key bindings, storage class, placement after a placement error | New participant name |
| Genesis, epochs, root defaults | New network in a fresh namespace |

## Reference

Load only the page the task needs.

- [recipes.md](references/recipes.md): examples and wiring patterns
- [graph.md](references/graph.md): every reference field and what it names
- Root: [stacksnetwork.md](references/stacksnetwork.md)
- Identity: [stacksaccount.md](references/stacksaccount.md),
  [bitcoinwallet.md](references/bitcoinwallet.md)
- Schedules: [stacksepochschedule.md](references/stacksepochschedule.md),
  [bitcoinblockschedule.md](references/bitcoinblockschedule.md)
- Actors: [bitcoinnode.md](references/bitcoinnode.md),
  [stacksnode.md](references/stacksnode.md), [stackssigner.md](references/stackssigner.md)
- Workers: [stacksstacker.md](references/stacksstacker.md),
  [bitcoinblockproduction.md](references/bitcoinblockproduction.md),
  [stackstransactionproduction.md](references/stackstransactionproduction.md),
  [stackscontractset.md](references/stackscontractset.md),
  [stacksfaucet.md](references/stacksfaucet.md)
- Requests: [stacksfaucetrequest.md](references/stacksfaucetrequest.md),
  [bitcoinblockscheduleoverride.md](references/bitcoinblockscheduleoverride.md)
- Generated: [stacksnetworkparticipant.md](references/stacksnetworkparticipant.md),
  [stacksgenesis.md](references/stacksgenesis.md),
  [bitcoinexecution.md](references/bitcoinexecution.md),
  [bitcoininitialization.md](references/bitcoininitialization.md)

Each kind page has usage notes followed by a field table generated from the served CRD.
`kubectl explain <kind>.spec --recursive` shows the same schema on a live cluster.

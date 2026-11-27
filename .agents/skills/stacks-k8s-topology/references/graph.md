# Reference graph

Every reference field in a user-authored spec, and what its `name` selects.

- **Participant in the same network**: an entry name in the root's
  `spec.participants`, of the stated kind. A wrong or missing kind reports
  `UnselectedDependency`; nothing is instantiated implicitly. Inside a reusable
  definition these names are network-relative and only resolve once a network selects
  both sides.
- **Same-namespace resource**: an independent resource, resolved regardless of
  network membership.
- **Participant of the network named by `spec.networkUID`**: requests pin the exact
  network UID; admission pins the participant UID.

`config.serviceRefs[]` (on `BitcoinNode`, `StacksNode` and `StacksSigner`) are not in the
table: each entry names a participant of its `kind` and substitutes that participant's
Service DNS name for `${SERVICE:<alias>}` in a custom configuration.

Common chains:

- Miner: `StacksNode.mining.bitcoinWalletRef` → `BitcoinWallet` →
  `keySource.stacksMinerAccountRef` → the node's `identityAccountRef` account. The
  node's `bitcoinNodeRef` participant must list that wallet in `walletRefs`.
- Signer: `StacksStacker.signerRef` → `StacksSigner.nodeRef` → `StacksNode`. The
  stacker's `targetNodeRef` is where it submits, usually the same node.
- Bootstrap: `BitcoinBlockProduction.initialization.targetNodeRef` must also be one of
  its initial `targets`.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

Generated from the chart CRDs; do not edit.

| From | Field | Names a | Resolved as |
| --- | --- | --- | --- |
| [BitcoinBlockProduction](bitcoinblockproduction.md) | `spec.initialization.minerWalletRefs` | [BitcoinWallet](bitcoinwallet.md) | same-namespace resource |
| [BitcoinBlockProduction](bitcoinblockproduction.md) | `spec.initialization.targetNodeRef` | [BitcoinNode](bitcoinnode.md) | participant in the same network |
| [BitcoinBlockProduction](bitcoinblockproduction.md) | `spec.payoutWalletRef` | [BitcoinWallet](bitcoinwallet.md) | same-namespace resource |
| [BitcoinBlockProduction](bitcoinblockproduction.md) | `spec.scheduleRef` | [BitcoinBlockSchedule](bitcoinblockschedule.md) | same-namespace resource |
| [BitcoinBlockProduction](bitcoinblockproduction.md) | `spec.targets[].nodeRef` | [BitcoinNode](bitcoinnode.md) | participant in the same network |
| [BitcoinBlockScheduleOverride](bitcoinblockscheduleoverride.md) | `spec.productionRef` | [BitcoinBlockProduction](bitcoinblockproduction.md) | participant of the network named by `spec.networkUID` |
| [BitcoinBlockScheduleOverride](bitcoinblockscheduleoverride.md) | `spec.scheduleRef` | [BitcoinBlockSchedule](bitcoinblockschedule.md) | same-namespace resource |
| [BitcoinNode](bitcoinnode.md) | `spec.config.secretRef` | Secret | same-namespace Secret key |
| [BitcoinNode](bitcoinnode.md) | `spec.peers.nodeRefs` | [BitcoinNode](bitcoinnode.md) | participant in the same network |
| [BitcoinNode](bitcoinnode.md) | `spec.walletRefs` | [BitcoinWallet](bitcoinwallet.md) | same-namespace resource |
| [BitcoinWallet](bitcoinwallet.md) | `spec.keySource.secretRef` | Secret | same-namespace Secret key |
| [BitcoinWallet](bitcoinwallet.md) | `spec.keySource.stacksMinerAccountRef` | [StacksAccount](stacksaccount.md) | same-namespace resource |
| [StacksAccount](stacksaccount.md) | `spec.key.secretRef` | Secret | same-namespace Secret key |
| [StacksContractSet](stackscontractset.md) | `spec.deployerAccountRef` | [StacksAccount](stacksaccount.md) | same-namespace resource |
| [StacksContractSet](stackscontractset.md) | `spec.initialization.aggregateKeyAccountRef` | [StacksAccount](stacksaccount.md) | same-namespace resource |
| [StacksContractSet](stackscontractset.md) | `spec.initialization.signerAccountRefs` | [StacksAccount](stacksaccount.md) | same-namespace resource |
| [StacksContractSet](stackscontractset.md) | `spec.targetNodeRef` | [StacksNode](stacksnode.md) | participant in the same network |
| [StacksFaucet](stacksfaucet.md) | `spec.accountRef` | [StacksAccount](stacksaccount.md) | same-namespace resource |
| [StacksFaucet](stacksfaucet.md) | `spec.targetNodeRef` | [StacksNode](stacksnode.md) | participant in the same network |
| [StacksFaucetRequest](stacksfaucetrequest.md) | `spec.destination.accountRef` | [StacksAccount](stacksaccount.md) | same-namespace resource |
| [StacksFaucetRequest](stacksfaucetrequest.md) | `spec.faucetRef` | [StacksFaucet](stacksfaucet.md) | participant of the network named by `spec.networkUID` |
| [StacksNetwork](stacksnetwork.md) | `spec.epochScheduleRef` | [StacksEpochSchedule](stacksepochschedule.md) | same-namespace resource |
| [StacksNetwork](stacksnetwork.md) | `spec.genesis.allocations[].accountRef` | [StacksAccount](stacksaccount.md) | same-namespace resource |
| [StacksNetwork](stacksnetwork.md) | `spec.participants[].definition.ref` | entry `kind` | same-namespace definition of the entry's kind |
| [StacksNode](stacksnode.md) | `spec.bitcoinNodeRef` | [BitcoinNode](bitcoinnode.md) | participant in the same network |
| [StacksNode](stacksnode.md) | `spec.config.secretRef` | Secret | same-namespace Secret key |
| [StacksNode](stacksnode.md) | `spec.identityAccountRef` | [StacksAccount](stacksaccount.md) | same-namespace resource |
| [StacksNode](stacksnode.md) | `spec.mining.bitcoinWalletRef` | [BitcoinWallet](bitcoinwallet.md) | same-namespace resource |
| [StacksNode](stacksnode.md) | `spec.peers.nodeRefs` | [StacksNode](stacksnode.md) | participant in the same network |
| [StacksSigner](stackssigner.md) | `spec.accountRef` | [StacksAccount](stacksaccount.md) | same-namespace resource |
| [StacksSigner](stackssigner.md) | `spec.config.secretRef` | Secret | same-namespace Secret key |
| [StacksSigner](stackssigner.md) | `spec.nodeRef` | [StacksNode](stacksnode.md) | participant in the same network |
| [StacksStacker](stacksstacker.md) | `spec.administratorAccountRef` | [StacksAccount](stacksaccount.md) | same-namespace resource |
| [StacksStacker](stacksstacker.md) | `spec.holderAccountRef` | [StacksAccount](stacksaccount.md) | same-namespace resource |
| [StacksStacker](stacksstacker.md) | `spec.signerRef` | [StacksSigner](stackssigner.md) | participant in the same network |
| [StacksStacker](stacksstacker.md) | `spec.targetNodeRef` | [StacksNode](stacksnode.md) | participant in the same network |
| [StacksTransactionProduction](stackstransactionproduction.md) | `spec.accountRef` | [StacksAccount](stacksaccount.md) | same-namespace resource |
| [StacksTransactionProduction](stackstransactionproduction.md) | `spec.recipient.accountRef` | [StacksAccount](stacksaccount.md) | same-namespace resource |
| [StacksTransactionProduction](stackstransactionproduction.md) | `spec.targetNodeRef` | [StacksNode](stacksnode.md) | participant in the same network |

<!-- END GENERATED: crd-reference -->

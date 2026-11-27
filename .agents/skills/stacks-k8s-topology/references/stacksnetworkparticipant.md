# StacksNetworkParticipant

Generated per root entry by the network controller, `network.stacks.org/v1alpha2`.
Never apply or edit it; change the root entry instead.

## Usage

- `spec.participantName` and `spec.kind` identify the entry. `spec.configuration` is the
  complete compiled policy after defaults, definition and overrides.
- `status.admission` shows the admitted source, dependencies and policy digest; runtime
  and execution status show workloads and worker progress. Use it to see what an entry
  actually resolved to.
- Deleting one directly while its entry remains reports `InstanceLost`; the operator does
  not recreate it.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`network.stacks.org/v1alpha2`, kind `StacksNetworkParticipant`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.configuration` | object | yes | Configuration contains complete compiled values. | |
| `spec.configuration.bitcoinBlockProduction` | object | | BitcoinBlockProduction configures baseline Bitcoin opportunities. Same fields as BitcoinBlockProduction `spec`. | |
| `spec.configuration.bitcoinNode` | object | | BitcoinNode configures Core. Same fields as BitcoinNode `spec`. | |
| `spec.configuration.stacksContractSet` | object | | StacksContractSet configures required contracts. Same fields as StacksContractSet `spec`. | |
| `spec.configuration.stacksFaucet` | object | | StacksFaucet configures explicit funding. Same fields as StacksFaucet `spec`. | |
| `spec.configuration.stacksNode` | object | | StacksNode configures a Stacks peer. Same fields as StacksNode `spec`. | |
| `spec.configuration.stacksSigner` | object | | StacksSigner configures consensus signing. Same fields as StacksSigner `spec`. | |
| `spec.configuration.stacksStacker` | object | | StacksStacker configures protocol maintenance. Same fields as StacksStacker `spec`. | |
| `spec.configuration.stacksTransactionProduction` | object | | StacksTransactionProduction configures baseline traffic. Same fields as StacksTransactionProduction `spec`. | |
| `spec.control` | object | | Control projects the user's workload-specific operation. | |
| `spec.control.paused` | boolean | | Paused cooperatively holds supported baseline workers. | |
| `spec.control.suspended` | boolean | | Suspended scales actors down with retained storage. | |
| `spec.kind` | string | yes | Kind selects a domain controller. | one of `BitcoinNode`, `StacksNode`, `StacksSigner`, `StacksStacker`, `StacksFaucet`, `StacksContractSet`, `StacksTransactionProduction`, `BitcoinBlockProduction` |
| `spec.networkUID` | string | yes | NetworkUID binds the canonical root. | |
| `spec.participantName` | string | yes | ParticipantName identifies the root map-list entry. | |
| `spec.source` | object | yes | Source records pinned input provenance. | |
| `spec.source.digest` | string | yes | Digest identifies inline or reusable public spec content. | |
| `spec.source.generation` | integer (int64) | yes | Generation identifies the resolved input revision. | |
| `spec.source.name` | string | | Name identifies a reusable definition; empty for inline. | |
| `spec.source.uid` | string | | UID pins a reusable definition; empty for inline. | |

<!-- END GENERATED: crd-reference -->

# BitcoinInitialization

Generated, network-owned record of Bitcoin bootstrap funding and scheduling,
`bitcoin.stacks.org/v1alpha2`. Never applied by users.

## Usage

It captures the initialization inputs and the durable scheduler state, so miner funding
is never repeated by a reconcile, pause or producer replacement. Read it when the first
bootstrap gate does not advance.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`bitcoin.stacks.org/v1alpha2`, kind `BitcoinInitialization`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.batchGeneration` | boolean | | BatchGeneration is the frozen first-gate batching choice; omission is single-block generation. | |
| `spec.genesis` | object | yes | Genesis binds the frozen requirements artifact. | |
| `spec.genesis.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.genesis.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.genesis.name` | string | yes | Name is the public resource name. | |
| `spec.genesis.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.matureOutputsPerMiner` | integer (int32) | yes | MatureOutputsPerMiner is the frozen per-miner funding requirement. | ≥ 1 |
| `spec.minerWallets` | []object | yes | MinerWallets is the exact immutable initial funding cohort. | items ≤ 100 |
| `spec.minerWallets[].address` | string | yes | Address is the fixed coinbase destination. | |
| `spec.minerWallets[].descriptor` | string | yes | Descriptor is the fixed public watch-only descriptor. | |
| `spec.minerWallets[].name` | string | yes | Name is the local Core wallet name. | |
| `spec.minerWallets[].wallet` | object | yes | Wallet binds the reusable wallet incarnation and public digest. | |
| `spec.minerWallets[].wallet.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.minerWallets[].wallet.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.minerWallets[].wallet.name` | string | yes | Name is the public resource name. | |
| `spec.minerWallets[].wallet.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.minimumHeight` | integer (int64) | yes | MinimumHeight is the first frozen ceiling and target height. | ≥ 1 |
| `spec.networkUID` | string | yes | NetworkUID identifies the owning independent environment. | |
| `spec.nodes` | []object | yes | Nodes pins every Bitcoin participant required to converge for the first gate. | items ≤ 1000 |
| `spec.nodes[].fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.nodes[].kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.nodes[].name` | string | yes | Name is the public resource name. | |
| `spec.nodes[].uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.payoutWallet` | object | yes | PayoutWallet is the pinned destination for maturity/height advancement blocks. | |
| `spec.payoutWallet.address` | string | yes | Address is the fixed coinbase destination. | |
| `spec.payoutWallet.descriptor` | string | yes | Descriptor is the fixed public watch-only descriptor. | |
| `spec.payoutWallet.name` | string | yes | Name is the local Core wallet name. | |
| `spec.payoutWallet.wallet` | object | yes | Wallet binds the reusable wallet incarnation and public digest. | |
| `spec.payoutWallet.wallet.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.payoutWallet.wallet.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.payoutWallet.wallet.name` | string | yes | Name is the public resource name. | |
| `spec.payoutWallet.wallet.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.production` | object | yes | Production binds the captured initial production participant. | |
| `spec.production.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.production.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.production.name` | string | yes | Name is the public resource name. | |
| `spec.production.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.target` | object | yes | Target binds the single initialization generation participant. | |
| `spec.target.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.target.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.target.name` | string | yes | Name is the public resource name. | |
| `spec.target.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |

<!-- END GENERATED: crd-reference -->

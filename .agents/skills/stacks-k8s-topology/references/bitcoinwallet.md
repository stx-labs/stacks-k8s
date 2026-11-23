# BitcoinWallet

A reusable Bitcoin Core wallet identity, `bitcoin.stacks.org/v1alpha2`. It is not one
wallet database: every `BitcoinNode` that lists it in `walletRefs` loads its own copy.

## Usage

- `keySource` is one of `generate: true` (default), `stacksMinerAccountRef` (derive
  the public descriptor from a miner's `StacksAccount`), or `secretRef` (a supported
  descriptor in a Secret).
- Miner wallets use `stacksMinerAccountRef` with the same account as the miner node's
  `identityAccountRef`.
- `walletName` defaults to the resource name and must be unique among the wallets one
  node loads.
- Only `watchOnly: true` (the default) is supported; `false` is rejected at admission.

## Updates

Identity and `walletName` are immutable once resolved; select a new wallet instead.
Removing a wallet from a node's `walletRefs` requests an unload; an active miner
dependency reports `InUse` until the miner binding is removed.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`bitcoin.stacks.org/v1alpha2`, kind `BitcoinWallet`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.keySource` | object | | KeySource defines wallet identity; omission requests generation. | |
| `spec.keySource.generate` | boolean | | Generate requests a new wallet key. | one of `true` |
| `spec.keySource.secretRef` | ref → Secret key | | SecretRef imports a supported descriptor. | |
| `spec.keySource.stacksMinerAccountRef` | ref → StacksAccount | | StacksMinerAccountRef derives a public watch-only miner descriptor. | |
| `spec.walletName` | string | | WalletName is the local Core wallet name; omission uses the CR name. | length ≤ 253 |
| `spec.watchOnly` | boolean | | WatchOnly omits private material from Core; only true or omission is supported. | |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec` | only watch-only Core wallets are supported; omit watchOnly or set it to true | `!has(self.watchOnly) \|\| self.watchOnly` |
| `spec.keySource` | select exactly one wallet key source | `(has(self.generate) ? 1 : 0) + (has(self.secretRef) ? 1 : 0) + (has(self.stacksMinerAccountRef) ? 1 : 0) == 1` |

<!-- END GENERATED: crd-reference -->

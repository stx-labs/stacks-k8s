# StacksAccount

A reusable Stacks identity, `stacks.stacks.org/v1alpha2`. It has no network binding and
survives network deletion.

## Usage

- `key` is exactly one of `generate: true` (default when omitted), `secretRef`
  (an imported private key in a user-owned Secret), or `publicIdentity`
  (address and public key only).
- A public-only identity can receive funds and appear in public configuration, but any
  signing role (miner, signer, stacker, deployer, traffic, faucet) needs a private key.
- `status` publishes `address`, `publicKey` and `credentialsRef` once `Resolved=True`.
  Resolved does not mean funded or stacked.
- Several roles may share one account. The examples use separate keys because shared
  senders can produce nonce conflicts between independent workers.

## Updates

The key specification is immutable once resolved. Deleting the account can delete its
generated Secret and break active consumers. Fund accounts through
`StacksNetwork.spec.genesis.allocations` or, after initialization, a
`StacksFaucetRequest`.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`stacks.stacks.org/v1alpha2`, kind `StacksAccount`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.key` | object | | Key defines imported/public/generated identity. | |
| `spec.key.generate` | boolean | | Generate requests a new reusable key. | one of `true` |
| `spec.key.publicIdentity` | object | | PublicIdentity provides no signing authority. | |
| `spec.key.publicIdentity.address` | string | yes | Address is a testnet Stacks address. | length ≤ 64; pattern `^ST[0-9A-HJKMNP-TV-Z]{26,39}$` |
| `spec.key.publicIdentity.publicKey` | string | yes | PublicKey is a compressed secp256k1 public key. | pattern `^(02\|03)[0-9a-fA-F]{64}$` |
| `spec.key.secretRef` | ref → Secret key | | SecretRef imports private material into a scoped resolver. | |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec.key` | select exactly one key source | `(has(self.generate) ? 1 : 0) + (has(self.secretRef) ? 1 : 0) + (has(self.publicIdentity) ? 1 : 0) == 1` |

<!-- END GENERATED: crd-reference -->

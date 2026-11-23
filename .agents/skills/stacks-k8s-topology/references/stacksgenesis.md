# StacksGenesis

The generated, immutable genesis artifact for one network, `network.stacks.org/v1alpha2`.
Created at freeze when the root first runs; never applied by users.

## Usage

- `spec.chain` holds the resolved profile, epochs, PoX settings, allocations and
  contract bindings; `status.genesisDigest` covers it.
- `spec.bootstrap` records the gates and requirements initialization must meet.
- Missing or altered genesis fails the network closed. Later participants reuse it and
  need existing allocations or a faucet request for funds.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`network.stacks.org/v1alpha2`, kind `StacksGenesis`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.bootstrap` | object | yes | Bootstrap retains requirements outside the chain digest. | |
| `spec.bootstrap.gates` | []object | yes | Gates defines the profile's fixed initialization boundaries. | items ≤ 16 |
| `spec.bootstrap.gates[].bitcoinCeiling` | integer (int64) | yes | BitcoinCeiling bounds new generation. | |
| `spec.bootstrap.gates[].name` | string | yes | Name identifies one convergence gate. | |
| `spec.bootstrap.gates[].targetCycle` | integer (int64) | | TargetCycle identifies required enrollment coverage when applicable. | |
| `spec.bootstrap.requirements` | []object | yes | Requirements preserves independently evaluable public cohort values. | items ≤ 1000 |
| `spec.bootstrap.requirements[].accounts` | []object | | Accounts preserves public account values used by this participant. | items ≤ 1000 |
| `spec.bootstrap.requirements[].accounts[].binding` | object | yes | Binding pins the source identity. | |
| `spec.bootstrap.requirements[].accounts[].binding.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.bootstrap.requirements[].accounts[].binding.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.bootstrap.requirements[].accounts[].binding.name` | string | yes | Name is the public resource name. | |
| `spec.bootstrap.requirements[].accounts[].binding.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.bootstrap.requirements[].accounts[].identity` | object | yes | Identity retains the exact public key and address. | |
| `spec.bootstrap.requirements[].accounts[].identity.address` | string | yes | Address is a testnet Stacks address. | length ≤ 64; pattern `^ST[0-9A-HJKMNP-TV-Z]{26,39}$` |
| `spec.bootstrap.requirements[].accounts[].identity.publicKey` | string | yes | PublicKey is a compressed secp256k1 public key. | pattern `^(02\|03)[0-9a-fA-F]{64}$` |
| `spec.bootstrap.requirements[].amountMicroSTX` | string | | AmountMicroSTX preserves required stake. | length ≤ 20; pattern `^(0\|[1-9][0-9]{0,19})$` |
| `spec.bootstrap.requirements[].bitcoinInitialization` | object | | BitcoinInitialization preserves initial wallet/maturity requirements. | |
| `spec.bootstrap.requirements[].bitcoinInitialization.batchGeneration` | boolean | | BatchGeneration funds each miner and reaches MinimumHeight in as few first-gate requests as the bounded RPC receipt allows; omission or false generates one block per opportunity. Later gates always generate one block per opportunity. | |
| `spec.bootstrap.requirements[].bitcoinInitialization.matureOutputsPerMiner` | integer (int32) | yes | MatureOutputsPerMiner is required for every bootstrap miner. | 1–100 |
| `spec.bootstrap.requirements[].bitcoinInitialization.minerWalletRefs` | []ref | | MinerWalletRefs selects wallets receiving mature coinbase outputs. | items ≤ 100; keyed by `name` |
| `spec.bootstrap.requirements[].bitcoinInitialization.minimumHeight` | integer (int64) | yes | MinimumHeight is the initial Bitcoin height. | 1–4294967295 |
| `spec.bootstrap.requirements[].bitcoinInitialization.targetNodeRef` | ref | yes | TargetNodeRef selects the initial generation target. | |
| `spec.bootstrap.requirements[].bitcoinPayoutWallet` | object | | BitcoinPayoutWallet pins the public payout identity across production replacement. | |
| `spec.bootstrap.requirements[].bitcoinPayoutWallet.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.bootstrap.requirements[].bitcoinPayoutWallet.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.bootstrap.requirements[].bitcoinPayoutWallet.name` | string | yes | Name is the public resource name. | |
| `spec.bootstrap.requirements[].bitcoinPayoutWallet.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.bootstrap.requirements[].dependencies` | []object | | Dependencies pins public credential identities. | items ≤ 1000 |
| `spec.bootstrap.requirements[].dependencies[].fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.bootstrap.requirements[].dependencies[].kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.bootstrap.requirements[].dependencies[].name` | string | yes | Name is the public resource name. | |
| `spec.bootstrap.requirements[].dependencies[].uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.bootstrap.requirements[].kind` | string | yes | Kind freezes the domain role independently of whether its instance still exists. | one of `BitcoinNode`, `StacksNode`, `StacksSigner`, `StacksStacker`, `StacksFaucet`, `StacksContractSet`, `StacksTransactionProduction`, `BitcoinBlockProduction` |
| `spec.bootstrap.requirements[].lockCycles` | integer (int32) | | LockCycles preserves required coverage. | |
| `spec.bootstrap.requirements[].miningEnabled` | boolean | | MiningEnabled preserves the initial Stacks node's mining role through bootstrap. | |
| `spec.bootstrap.requirements[].participant` | object | yes | Participant is the exact initial instance identity. | |
| `spec.bootstrap.requirements[].participant.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.bootstrap.requirements[].participant.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.bootstrap.requirements[].participant.name` | string | yes | Name is the public resource name. | |
| `spec.bootstrap.requirements[].participant.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.bootstrap.requirements[].policyDigest` | string | yes | PolicyDigest identifies its initially admitted configuration. | |
| `spec.bootstrap.requirements[].registryInitialization` | object | | RegistryInitialization preserves exact initial bridge state. | |
| `spec.bootstrap.requirements[].registryInitialization.aggregateKeyAccountRef` | ref | yes | AggregateKeyAccountRef supplies the explicit aggregate key. | |
| `spec.bootstrap.requirements[].registryInitialization.mode` | string | yes | Mode is the supported initialization authority. | one of `ExplicitTestRegistry` |
| `spec.bootstrap.requirements[].registryInitialization.signerAccountRefs` | []ref | yes | SignerAccountRefs supplies public bridge identities. | items 2–100; keyed by `name` |
| `spec.bootstrap.requirements[].registryInitialization.threshold` | integer (int32) | yes | Threshold bounds registry authorization. | 1–100 |
| `spec.bootstrap.requirements[].renewWhenRemainingCycles` | integer (int32) | | RenewWhenRemainingCycles preserves maintenance needed before initial gates finish. | |
| `spec.chain` | object | yes | Chain is the digest-bearing public chain configuration. | |
| `spec.chain.allocations` | []object | yes | Allocations contains deduplicated initial balances. | items ≤ 1000 |
| `spec.chain.allocations[].address` | string | yes | Address is a resolved account principal. | |
| `spec.chain.allocations[].amountMicroSTX` | string | yes | AmountMicroSTX is a normalized decimal quantity. | length ≤ 20; pattern `^(0\|[1-9][0-9]{0,19})$` |
| `spec.chain.contracts` | object | yes | Contracts binds required public contract principals. | |
| `spec.chain.contracts.bundle` | string | yes | Bundle names the release contract manifest. | |
| `spec.chain.contracts.deployer` | string | yes | Deployer is the resolved deployment principal. | |
| `spec.chain.contracts.sourceHashes` | map[string]string | yes | SourceHashes identifies contract sources. | properties ≤ 32 |
| `spec.chain.epochs` | []object | yes | Epochs contains ordered activation boundaries. | items ≤ 14 |
| `spec.chain.epochs[].name` | string | yes | Name identifies the protocol epoch as a string. | one of `1.0`, `2.0`, `2.05`, `2.1`, `2.2`, `2.3`, `2.4`, `2.5`, `3.0`, `3.1`, `3.2`, `3.3`, `3.4`, `4.0` |
| `spec.chain.epochs[].startHeight` | integer (int64) | yes | StartHeight is the activation burn height. | 0–4294967295 |
| `spec.chain.pox` | object | yes | PoX contains resolved cycle lengths. | |
| `spec.chain.pox.prepareLength` | integer (int32) | yes | PrepareLength counts preparation blocks. | 1–99999 |
| `spec.chain.pox.rewardCycleLength` | integer (int32) | yes | RewardCycleLength counts Bitcoin blocks per cycle. | 2–100000 |
| `spec.chain.profile` | string | yes | Profile identifies the pinned profile revision. | |
| `spec.source` | object | yes | Source records captured network provenance. | |
| `spec.source.dependencies` | []object | | Dependencies pins genesis-source identities. | items ≤ 1000 |
| `spec.source.dependencies[].fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.source.dependencies[].kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.source.dependencies[].name` | string | yes | Name is the public resource name. | |
| `spec.source.dependencies[].uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.source.inputDigest` | string | yes | InputDigest identifies captured semantic inputs. | |
| `spec.source.networkUID` | string | yes | NetworkUID identifies this independent environment. | |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec.bootstrap.requirements[].amountMicroSTX` | amount must fit uint64 | `uint(self) <= 18446744073709551615u` |
| `spec.bootstrap.requirements[].registryInitialization` | threshold must not exceed signer count | `self.threshold <= size(self.signerAccountRefs)` |
| `spec.bootstrap.requirements[].registryInitialization` | threshold must be a strict signer majority | `self.threshold > size(self.signerAccountRefs) / 2` |
| `spec.chain.allocations[].amountMicroSTX` | amount must fit uint64 | `uint(self) <= 18446744073709551615u` |
| `spec.chain.pox` | prepare must be shorter than reward cycle | `self.prepareLength < self.rewardCycleLength` |

<!-- END GENERATED: crd-reference -->

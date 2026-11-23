# StacksNetwork

The root, `network.stacks.org/v1alpha2`. Exactly one per namespace, named `network`.
It selects participants, freezes genesis and requests operation.

## Usage

- `participants[]` is keyed by `name` (DNS label, at most 63 characters). Each entry has
  a `kind`, exactly one of `definition.ref` (same-namespace definition of that kind) or
  `definition.inline.<branch>`, optional `overrides.<branch>`, and optional `control`.
  The branch must match `kind`.
- Limits: 100 entries per actor kind, 100 stackers, and at most one each of
  `BitcoinBlockProduction`, `StacksTransactionProduction`, `StacksContractSet` and
  `StacksFaucet`. All but the faucet are required.
- `genesis.allocations[]` funds accounts by `accountRef`. Equal duplicate entries count
  once; conflicting amounts are invalid. `genesis.pox` defaults to cycle 20, prepare 5.
- `epochScheduleRef` or inline `epochSchedule`, not both. Omitting both selects the
  built-in schedule, which starts epoch 3.0 at height 252 and 4.0 at 282.
- `defaults.images`, `defaults.storage` and `defaults.actorResources` fill holes in
  every actor. `defaults.workerPlacement` places support Pods (workers, resolvers),
  not actors.
- `operation`: `Running` (default) resolves, freezes genesis and initializes. `Paused`
  before activation only resolves; after activation it cooperatively holds new
  submissions. `Stopped` is terminal.
- `expectedInputDigest` optionally pins a reviewed `status.inputDigest`.
- `control.suspended` applies to `BitcoinNode`, `StacksNode` and `StacksSigner`;
  `control.paused` to stacker, contract-set, traffic and Bitcoin production entries.

## Status

Check conditions `Resolved`, `Initialized`, `Running`, `Operational` and `Failed`, not
phase alone. `status.participants[]` maps entry names to generated participant UIDs.
`status.identities` records every participant name ever allocated; names cannot be
reused.

## Updates

`profile`, epochs, `genesis` and `defaults` are immutable (CEL-enforced). `participants`
is mutable: add entries, remove entries (destructive), change overrides and controls.
Entry `kind` is immutable while the entry exists.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`network.stacks.org/v1alpha2`, kind `StacksNetwork`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.defaults` | object | | Defaults supplies immutable per-network fallbacks. | |
| `spec.defaults.actorResources` | object | | ActorResources supplies inherited resource requirements. | |
| `spec.defaults.actorResources.bitcoin` | core/v1 ResourceRequirements | | Bitcoin is the Core workload budget. | |
| `spec.defaults.actorResources.stacksNode` | core/v1 ResourceRequirements | | StacksNode is the peer/miner workload budget. | |
| `spec.defaults.actorResources.stacksSigner` | core/v1 ResourceRequirements | | StacksSigner is the consensus workload budget. | |
| `spec.defaults.images` | object | | Images selects per-kind binaries. | |
| `spec.defaults.images.bitcoin` | string | | Bitcoin selects Core. | length ≤ 512 |
| `spec.defaults.images.stacksNode` | string | | StacksNode selects Stacks peers. | length ≤ 512 |
| `spec.defaults.images.stacksSigner` | string | | StacksSigner selects consensus signing. | length ≤ 512 |
| `spec.defaults.storage` | object | | Storage supplies inherited actor storage. | |
| `spec.defaults.storage.class` | string | | Class preserves explicit empty storageClassName. | length ≤ 253 |
| `spec.defaults.storage.ephemeral` | boolean | | Ephemeral selects emptyDir. | |
| `spec.defaults.storage.retainOnDelete` | boolean | | RetainOnDelete preserves a PVC after instance removal. | |
| `spec.defaults.storage.size` | string | | Size is a Kubernetes capacity quantity. | length ≤ 32 |
| `spec.defaults.workerPlacement` | object | | WorkerPlacement applies to support Pods only. | |
| `spec.defaults.workerPlacement.nodeSelector` | map[string]string | | NodeSelector constrains eligible nodes. | properties ≤ 32 |
| `spec.defaults.workerPlacement.spreadAcrossNodes` | boolean | | SpreadAcrossNodes requests preferred same-kind anti-affinity. | |
| `spec.defaults.workerPlacement.tolerations` | []core/v1 Toleration | | Tolerations allow explicit support-node placement. | items ≤ 32 |
| `spec.epochSchedule` | object | | EpochSchedule supplies inline schedule inputs. | |
| `spec.epochSchedule.epochs` | []object | yes | Epochs are complete, ordered and nondecreasing. | items 14–14 |
| `spec.epochSchedule.epochs[].name` | string | yes | Name identifies the protocol epoch as a string. | one of `1.0`, `2.0`, `2.05`, `2.1`, `2.2`, `2.3`, `2.4`, `2.5`, `3.0`, `3.1`, `3.2`, `3.3`, `3.4`, `4.0` |
| `spec.epochSchedule.epochs[].startHeight` | integer (int64) | yes | StartHeight is the activation burn height. | 0–4294967295 |
| `spec.epochScheduleRef` | ref → StacksEpochSchedule | | EpochScheduleRef supplies a reusable schedule. | |
| `spec.expectedInputDigest` | string | | ExpectedInputDigest optionally pins reviewed initial semantic inputs. | pattern `^sha256:[0-9a-f]{64}$` |
| `spec.genesis` | object | | Genesis configures funded accounts and PoX. | |
| `spec.genesis.allocations` | []object | | Allocations enumerates funded accounts explicitly. | items ≤ 1000 |
| `spec.genesis.allocations[].accountRef` | ref → StacksAccount | yes | AccountRef selects a public principal. | |
| `spec.genesis.allocations[].amountMicroSTX` | string | yes | AmountMicroSTX is the initial balance. | length ≤ 20; pattern `^(0\|[1-9][0-9]{0,19})$` |
| `spec.genesis.pox` | object | | PoX specifies regtest cycle lengths. | |
| `spec.genesis.pox.prepareLength` | integer (int32) | yes | PrepareLength counts preparation blocks. | 1–99999 |
| `spec.genesis.pox.rewardCycleLength` | integer (int32) | yes | RewardCycleLength counts Bitcoin blocks per cycle. | 2–100000 |
| `spec.operation` | string | | Operation requests resolution, cooperative pause or terminal stop. | one of `Running`, `Paused`, `Stopped`; default `"Running"` |
| `spec.participants` | []object | | Participants selects named runtime instances. | items ≤ 1000; keyed by `name` |
| `spec.participants[].control` | object | | Control applies independently of substantive updates. | |
| `spec.participants[].control.paused` | boolean | | Paused cooperatively holds supported baseline workers. | |
| `spec.participants[].control.suspended` | boolean | | Suspended scales actors down with retained storage. | |
| `spec.participants[].definition` | object | yes | Definition supplies reusable or inline inputs. | |
| `spec.participants[].definition.inline` | object | | Inline supplies the corresponding spec directly. | |
| `spec.participants[].definition.inline.bitcoinBlockProduction` | object | | BitcoinBlockProduction configures baseline Bitcoin opportunities. Same fields as BitcoinBlockProduction `spec`. | |
| `spec.participants[].definition.inline.bitcoinNode` | object | | BitcoinNode configures Core. Same fields as BitcoinNode `spec`. | |
| `spec.participants[].definition.inline.stacksContractSet` | object | | StacksContractSet configures required contracts. Same fields as StacksContractSet `spec`. | |
| `spec.participants[].definition.inline.stacksFaucet` | object | | StacksFaucet configures explicit funding. Same fields as StacksFaucet `spec`. | |
| `spec.participants[].definition.inline.stacksNode` | object | | StacksNode configures a Stacks peer. Same fields as StacksNode `spec`. | |
| `spec.participants[].definition.inline.stacksSigner` | object | | StacksSigner configures consensus signing. Same fields as StacksSigner `spec`. | |
| `spec.participants[].definition.inline.stacksStacker` | object | | StacksStacker configures protocol maintenance. Same fields as StacksStacker `spec`. | |
| `spec.participants[].definition.inline.stacksTransactionProduction` | object | | StacksTransactionProduction configures baseline traffic. Same fields as StacksTransactionProduction `spec`. | |
| `spec.participants[].definition.ref` | ref → entry `kind` | | Ref selects the reusable CR of the outer kind. | |
| `spec.participants[].kind` | string | yes | Kind selects the typed branch and referenced kind. | one of `BitcoinNode`, `StacksNode`, `StacksSigner`, `StacksStacker`, `StacksFaucet`, `StacksContractSet`, `StacksTransactionProduction`, `BitcoinBlockProduction` |
| `spec.participants[].name` | string | yes | Name is the network-relative instance identity. | length 1–63; pattern `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` |
| `spec.participants[].overrides` | object | | Overrides applies explicit per-instance values after inherited inputs. | |
| `spec.participants[].overrides.bitcoinBlockProduction` | object | | BitcoinBlockProduction configures baseline Bitcoin opportunities. Same fields as BitcoinBlockProduction `spec`. | |
| `spec.participants[].overrides.bitcoinNode` | object | | BitcoinNode configures Core. Same fields as BitcoinNode `spec`. | |
| `spec.participants[].overrides.stacksContractSet` | object | | StacksContractSet configures required contracts. Same fields as StacksContractSet `spec`. | |
| `spec.participants[].overrides.stacksFaucet` | object | | StacksFaucet configures explicit funding. Same fields as StacksFaucet `spec`. | |
| `spec.participants[].overrides.stacksNode` | object | | StacksNode configures a Stacks peer. Same fields as StacksNode `spec`. | |
| `spec.participants[].overrides.stacksSigner` | object | | StacksSigner configures consensus signing. Same fields as StacksSigner `spec`. | |
| `spec.participants[].overrides.stacksStacker` | object | | StacksStacker configures protocol maintenance. Same fields as StacksStacker `spec`. | |
| `spec.participants[].overrides.stacksTransactionProduction` | object | | StacksTransactionProduction configures baseline traffic. Same fields as StacksTransactionProduction `spec`. | |
| `spec.profile` | string | | Profile selects the supported immutable release profile. | one of `regtest-pox4-pox5-v1`; default `"regtest-pox4-pox5-v1"` |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec` | schedule sources are exclusive | `!(has(self.epochScheduleRef) && has(self.epochSchedule))` |
| `spec` | profile is immutable | `has(self.profile) == has(oldSelf.profile) && (!has(self.profile) \|\| self.profile == oldSelf.profile)` |
| `spec` | epochScheduleRef is immutable | `has(self.epochScheduleRef) == has(oldSelf.epochScheduleRef) && (!has(self.epochScheduleRef) \|\| self.epochScheduleRef == oldSelf.epochScheduleRef)` |
| `spec` | epochSchedule is immutable | `has(self.epochSchedule) == has(oldSelf.epochSchedule) && (!has(self.epochSchedule) \|\| self.epochSchedule == oldSelf.epochSchedule)` |
| `spec` | genesis is immutable | `has(self.genesis) == has(oldSelf.genesis) && (!has(self.genesis) \|\| self.genesis == oldSelf.genesis)` |
| `spec` | defaults is immutable | `has(self.defaults) == has(oldSelf.defaults) && (!has(self.defaults) \|\| self.defaults == oldSelf.defaults)` |
| `spec` | participant kind limit exceeded | `!has(self.participants) \|\| size(self.participants.filter(p, p.kind == 'BitcoinNode')) <= 100`; `!has(self.participants) \|\| size(self.participants.filter(p, p.kind == 'StacksNode')) <= 100`; `!has(self.participants) \|\| size(self.participants.filter(p, p.kind == 'StacksSigner')) <= 100`; `!has(self.participants) \|\| size(self.participants.filter(p, p.kind == 'StacksStacker')) <= 100`; `!has(self.participants) \|\| size(self.participants.filter(p, p.kind == 'StacksFaucet')) <= 1`; `!has(self.participants) \|\| size(self.participants.filter(p, p.kind == 'StacksContractSet')) <= 1`; `!has(self.participants) \|\| size(self.participants.filter(p, p.kind == 'StacksTransactionProduction')) <= 1`; `!has(self.participants) \|\| size(self.participants.filter(p, p.kind == 'BitcoinBlockProduction')) <= 1` |
| `spec.defaults.storage` | ephemeral storage excludes PVC settings | `!has(self.ephemeral) \|\| !self.ephemeral \|\| (!has(self.size) && !has(self.class) && !has(self.retainOnDelete))` |
| `spec.epochSchedule.epochs` | epochs must be in supported order | `self.map(e, e.name) == ['1.0','2.0','2.05','2.1','2.2','2.3','2.4','2.5','3.0','3.1','3.2','3.3','3.4','4.0']` |
| `spec.epochSchedule.epochs` | epoch heights must be ordered with first two zero | `[0,1,2,3,4,5,6,7,8,9,10,11,12].all(i, self[i].startHeight <= self[i+1].startHeight) && self[0].startHeight == 0 && self[1].startHeight == 0` |
| `spec.genesis.allocations[].amountMicroSTX` | amount must fit uint64 | `uint(self) <= 18446744073709551615u` |
| `spec.genesis.pox` | prepare must be shorter than reward cycle | `self.prepareLength < self.rewardCycleLength` |
| `spec.operation` | Stopped is terminal | `oldSelf != 'Stopped' \|\| self == 'Stopped'` |
| `spec.participants[]` | kind is immutable while a participant entry exists | `self.kind == oldSelf.kind` |
| `spec.participants[]` | configuration branch must match participant kind | `!has(self.definition.inline) \|\| (has(self.definition.inline.bitcoinNode) == (self.kind == 'BitcoinNode'))`; `!has(self.definition.inline) \|\| (has(self.definition.inline.stacksNode) == (self.kind == 'StacksNode'))`; `!has(self.definition.inline) \|\| (has(self.definition.inline.stacksSigner) == (self.kind == 'StacksSigner'))`; `!has(self.definition.inline) \|\| (has(self.definition.inline.stacksStacker) == (self.kind == 'StacksStacker'))`; `!has(self.definition.inline) \|\| (has(self.definition.inline.stacksFaucet) == (self.kind == 'StacksFaucet'))`; `!has(self.definition.inline) \|\| (has(self.definition.inline.stacksContractSet) == (self.kind == 'StacksContractSet'))`; `!has(self.definition.inline) \|\| (has(self.definition.inline.stacksTransactionProduction) == (self.kind == 'StacksTransactionProduction'))`; `!has(self.definition.inline) \|\| (has(self.definition.inline.bitcoinBlockProduction) == (self.kind == 'BitcoinBlockProduction'))`; `!has(self.overrides) \|\| (has(self.overrides.bitcoinNode) == (self.kind == 'BitcoinNode'))`; `!has(self.overrides) \|\| (has(self.overrides.stacksNode) == (self.kind == 'StacksNode'))`; `!has(self.overrides) \|\| (has(self.overrides.stacksSigner) == (self.kind == 'StacksSigner'))`; `!has(self.overrides) \|\| (has(self.overrides.stacksStacker) == (self.kind == 'StacksStacker'))`; `!has(self.overrides) \|\| (has(self.overrides.stacksFaucet) == (self.kind == 'StacksFaucet'))`; `!has(self.overrides) \|\| (has(self.overrides.stacksContractSet) == (self.kind == 'StacksContractSet'))`; `!has(self.overrides) \|\| (has(self.overrides.stacksTransactionProduction) == (self.kind == 'StacksTransactionProduction'))`; `!has(self.overrides) \|\| (has(self.overrides.bitcoinBlockProduction) == (self.kind == 'BitcoinBlockProduction'))` |
| `spec.participants[]` | control is unsupported for this kind | `!has(self.control) \|\| ((!has(self.control.suspended) \|\| self.kind in ['BitcoinNode','StacksNode','StacksSigner']) && (!has(self.control.paused) \|\| self.kind in ['StacksStacker','StacksContractSet','StacksTransactionProduction','BitcoinBlockProduction']))` |
| `spec.participants[].definition` | select exactly one definition source | `has(self.ref) != has(self.inline)` |

<!-- END GENERATED: crd-reference -->

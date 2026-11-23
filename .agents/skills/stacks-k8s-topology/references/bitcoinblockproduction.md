# BitcoinBlockProduction

Ongoing Bitcoin block generation, `bitcoin.stacks.org/v1alpha2`. Exactly one per network.
It also funds and matures the initial miners during bootstrap.

## Usage

- `targets[]` are weighted `BitcoinNode` participants (positive integer weights, at most
  100, no duplicates).
- `scheduleRef` or inline `schedule`; neither means Fixed 60s.
- `payoutWalletRef` receives generated blocks that do not fund a miner.
- `initialization.targetNodeRef` must be one of the initial targets. Initialization
  funds every wallet in `minerWalletRefs` with `matureOutputsPerMiner` coinbase outputs,
  matures them and reaches `minimumHeight` (normally the epoch 2.05 start height).
- `initialization.batchGeneration: true` generates the first gate in a few large
  requests instead of one block per opportunity.

## Updates

Targets, weights, schedule and `control.paused` can change live. Payout and
initialization inputs are frozen with genesis; changing them needs a new network.
For a temporary cadence change, create a `BitcoinBlockScheduleOverride`.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`bitcoin.stacks.org/v1alpha2`, kind `BitcoinBlockProduction`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.initialization` | object | | Initialization freezes initial funding behavior. | |
| `spec.initialization.batchGeneration` | boolean | | BatchGeneration funds each miner and reaches MinimumHeight in as few first-gate requests as the bounded RPC receipt allows; omission or false generates one block per opportunity. Later gates always generate one block per opportunity. | |
| `spec.initialization.matureOutputsPerMiner` | integer (int32) | yes | MatureOutputsPerMiner is required for every bootstrap miner. | 1–100 |
| `spec.initialization.minerWalletRefs` | []ref → BitcoinWallet | | MinerWalletRefs selects wallets receiving mature coinbase outputs. | items ≤ 100; keyed by `name` |
| `spec.initialization.minimumHeight` | integer (int64) | yes | MinimumHeight is the initial Bitcoin height. | 1–4294967295 |
| `spec.initialization.targetNodeRef` | ref → BitcoinNode | yes | TargetNodeRef selects the initial generation target. | |
| `spec.payoutWalletRef` | ref → BitcoinWallet | | PayoutWalletRef supplies baseline payout identity. | |
| `spec.schedule` | object | | Schedule supplies inline timing. | |
| `spec.schedule.cadence` | object | yes | Cadence controls block opportunities. | |
| `spec.schedule.cadence.interval` | string | | Interval is the fixed duration. | length 2–32 |
| `spec.schedule.cadence.maximumInterval` | string | | MaximumInterval bounds uniform timing. | length 2–32 |
| `spec.schedule.cadence.minimumInterval` | string | | MinimumInterval bounds uniform timing. | length 2–32 |
| `spec.schedule.cadence.mode` | string | yes | Mode selects fixed or uniform timing. | one of `Fixed`, `Uniform` |
| `spec.scheduleRef` | ref → BitcoinBlockSchedule | | ScheduleRef selects immutable reusable timing. | |
| `spec.targets` | []object | | Targets selects weighted generation targets. | items ≤ 100 |
| `spec.targets[].nodeRef` | ref → BitcoinNode | yes | NodeRef selects a BitcoinNode participant. | |
| `spec.targets[].weight` | integer (int32) | yes | Weight controls relative selection probability. | ≥ 1 |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec` | select one schedule source | `!(has(self.scheduleRef) && has(self.schedule))` |
| `spec.schedule.cadence` | interval must be between 1s and 1h | `!has(self.interval) \|\| (duration(self.interval) >= duration('1s') && duration(self.interval) <= duration('1h'))` |
| `spec.schedule.cadence` | minimumInterval must be between 1s and 1h | `!has(self.minimumInterval) \|\| (duration(self.minimumInterval) >= duration('1s') && duration(self.minimumInterval) <= duration('1h'))` |
| `spec.schedule.cadence` | maximumInterval must be between 1s and 1h | `!has(self.maximumInterval) \|\| (duration(self.maximumInterval) >= duration('1s') && duration(self.maximumInterval) <= duration('1h'))` |
| `spec.schedule.cadence` | maximumInterval must not be below minimumInterval | `!has(self.minimumInterval) \|\| !has(self.maximumInterval) \|\| duration(self.maximumInterval) >= duration(self.minimumInterval)` |
| `spec.schedule.cadence` | cadence fields must match mode | `self.mode == 'Fixed' ? (has(self.interval) && !has(self.minimumInterval) && !has(self.maximumInterval)) : (!has(self.interval) && has(self.minimumInterval) && has(self.maximumInterval))` |

<!-- END GENERATED: crd-reference -->

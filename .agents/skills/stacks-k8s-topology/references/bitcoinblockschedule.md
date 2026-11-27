# BitcoinBlockSchedule

A reusable, immutable block cadence, `bitcoin.stacks.org/v1alpha2`. It creates no
workload.

## Usage

- `cadence.mode: Fixed` with `interval`, or `Uniform` with `minimumInterval` and
  `maximumInterval` (maximum at least minimum). Bounds 1s to 1h.
- Referenced by `BitcoinBlockProduction.scheduleRef` or
  `BitcoinBlockScheduleOverride.scheduleRef`. Production without a schedule uses
  Fixed 60s.
- The examples use 5s for fast local runs and 60s in `30-actors.yaml`.

## Updates

Immutable. Switch production to another schedule by changing its `scheduleRef`.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`bitcoin.stacks.org/v1alpha2`, kind `BitcoinBlockSchedule`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.cadence` | object | yes | Cadence controls block opportunities. | |
| `spec.cadence.interval` | string | | Interval is the fixed duration. | length 2–32 |
| `spec.cadence.maximumInterval` | string | | MaximumInterval bounds uniform timing. | length 2–32 |
| `spec.cadence.minimumInterval` | string | | MinimumInterval bounds uniform timing. | length 2–32 |
| `spec.cadence.mode` | string | yes | Mode selects fixed or uniform timing. | one of `Fixed`, `Uniform` |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec.cadence` | interval must be between 1s and 1h | `!has(self.interval) \|\| (duration(self.interval) >= duration('1s') && duration(self.interval) <= duration('1h'))` |
| `spec.cadence` | minimumInterval must be between 1s and 1h | `!has(self.minimumInterval) \|\| (duration(self.minimumInterval) >= duration('1s') && duration(self.minimumInterval) <= duration('1h'))` |
| `spec.cadence` | maximumInterval must be between 1s and 1h | `!has(self.maximumInterval) \|\| (duration(self.maximumInterval) >= duration('1s') && duration(self.maximumInterval) <= duration('1h'))` |
| `spec.cadence` | maximumInterval must not be below minimumInterval | `!has(self.minimumInterval) \|\| !has(self.maximumInterval) \|\| duration(self.maximumInterval) >= duration(self.minimumInterval)` |
| `spec.cadence` | cadence fields must match mode | `self.mode == 'Fixed' ? (has(self.interval) && !has(self.minimumInterval) && !has(self.maximumInterval)) : (!has(self.interval) && has(self.minimumInterval) && has(self.maximumInterval))` |

<!-- END GENERATED: crd-reference -->

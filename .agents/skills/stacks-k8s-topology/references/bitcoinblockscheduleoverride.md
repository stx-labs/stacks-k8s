# BitcoinBlockScheduleOverride

A temporary cadence change for the network's Bitcoin production,
`bitcoin.stacks.org/v1alpha2`. It is not a participant.

## Usage

- `networkUID` must equal the current root's UID; `productionRef` names the production
  participant.
- One `schedule` or `scheduleRef`, and a `duration` up to 1h from activation. It changes
  timing only, not targets or pause state.
- One override is active at a time; others wait in creation order and expire if not
  admitted within 5 minutes.
- When it ends, the latest baseline schedule resumes with a fresh timer; there is no
  catch-up.

## Updates

Immutable. Delete it to cancel; already submitted generation is not undone.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`bitcoin.stacks.org/v1alpha2`, kind `BitcoinBlockScheduleOverride`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.duration` | string | yes | Duration starts at activation, including time spent paused. | length 2–32 |
| `spec.networkUID` | string | yes | NetworkUID pins the canonical network incarnation. | length 1–128 |
| `spec.productionRef` | ref → BitcoinBlockProduction | yes | ProductionRef selects the logical BitcoinBlockProduction participant. | |
| `spec.schedule` | object | | Schedule contains immutable inline timing. | |
| `spec.schedule.cadence` | object | yes | Cadence controls block opportunities. | |
| `spec.schedule.cadence.interval` | string | | Interval is the fixed duration. | length 2–32 |
| `spec.schedule.cadence.maximumInterval` | string | | MaximumInterval bounds uniform timing. | length 2–32 |
| `spec.schedule.cadence.minimumInterval` | string | | MinimumInterval bounds uniform timing. | length 2–32 |
| `spec.schedule.cadence.mode` | string | yes | Mode selects fixed or uniform timing. | one of `Fixed`, `Uniform` |
| `spec.scheduleRef` | ref → BitcoinBlockSchedule | | ScheduleRef selects one immutable same-namespace schedule. | |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec` | select exactly one schedule source | `has(self.schedule) != has(self.scheduleRef)` |
| `spec.duration` | duration must be positive and at most 1h | `duration(self) > duration('0s') && duration(self) <= duration('1h')` |
| `spec.schedule.cadence` | interval must be between 1s and 1h | `!has(self.interval) \|\| (duration(self.interval) >= duration('1s') && duration(self.interval) <= duration('1h'))` |
| `spec.schedule.cadence` | minimumInterval must be between 1s and 1h | `!has(self.minimumInterval) \|\| (duration(self.minimumInterval) >= duration('1s') && duration(self.minimumInterval) <= duration('1h'))` |
| `spec.schedule.cadence` | maximumInterval must be between 1s and 1h | `!has(self.maximumInterval) \|\| (duration(self.maximumInterval) >= duration('1s') && duration(self.maximumInterval) <= duration('1h'))` |
| `spec.schedule.cadence` | maximumInterval must not be below minimumInterval | `!has(self.minimumInterval) \|\| !has(self.maximumInterval) \|\| duration(self.maximumInterval) >= duration(self.minimumInterval)` |
| `spec.schedule.cadence` | cadence fields must match mode | `self.mode == 'Fixed' ? (has(self.interval) && !has(self.minimumInterval) && !has(self.maximumInterval)) : (!has(self.interval) && has(self.minimumInterval) && has(self.maximumInterval))` |

<!-- END GENERATED: crd-reference -->

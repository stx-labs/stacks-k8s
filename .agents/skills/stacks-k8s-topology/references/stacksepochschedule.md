# StacksEpochSchedule

A reusable, immutable epoch activation plan, `network.stacks.org/v1alpha2`. A network
selects it with `epochScheduleRef` and copies it at genesis freeze.

## Usage

- List all fourteen epochs, `1.0` through `4.0`, in protocol order. The first two start
  at height 0; heights are nondecreasing.
- Quote epoch names: `"2.05"` must not become a number.
- Earlier epoch heights shorten initialization. `minimal.yaml` starts 2.05 at 103 and 3.0
  at 152, 100 blocks earlier than the built-in schedule; the bootstrap gate windows keep
  their lengths.
- Set `BitcoinBlockProduction.initialization.minimumHeight` to the 2.05 start height.

## Updates

The spec is immutable. Create another schedule for another plan; a network that already
froze genesis is unaffected by later edits or deletion.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`network.stacks.org/v1alpha2`, kind `StacksEpochSchedule`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.epochs` | []object | yes | Epochs are complete, ordered and nondecreasing. | items 14–14 |
| `spec.epochs[].name` | string | yes | Name identifies the protocol epoch as a string. | one of `1.0`, `2.0`, `2.05`, `2.1`, `2.2`, `2.3`, `2.4`, `2.5`, `3.0`, `3.1`, `3.2`, `3.3`, `3.4`, `4.0` |
| `spec.epochs[].startHeight` | integer (int64) | yes | StartHeight is the activation burn height. | 0–4294967295 |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec.epochs` | epochs must be in supported order | `self.map(e, e.name) == ['1.0','2.0','2.05','2.1','2.2','2.3','2.4','2.5','3.0','3.1','3.2','3.3','3.4','4.0']` |
| `spec.epochs` | epoch heights must be ordered with first two zero | `[0,1,2,3,4,5,6,7,8,9,10,11,12].all(i, self[i].startHeight <= self[i+1].startHeight) && self[0].startHeight == 0 && self[1].startHeight == 0` |

<!-- END GENERATED: crd-reference -->

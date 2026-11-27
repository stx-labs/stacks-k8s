# StacksTransactionProduction

Baseline STX transfer traffic, `stacks.stacks.org/v1alpha2`. Exactly one per network;
`Operational` requires its transfers to be included.

## Usage

- `accountRef` is the funded sender; `targetNodeRef` is the `StacksNode` participant it
  submits to.
- `recipient` is an `accountRef` or a concrete testnet address.
- `amountMicroSTX` and `feeMicroSTX` are positive; `interval` is 1s–1h. It offers one
  pending submission at a time, so slow inclusion lowers the actual rate.

## Updates

Amount, fee, interval and `control.paused` can change live. Account, target and
recipient changes need a new participant.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`stacks.stacks.org/v1alpha2`, kind `StacksTransactionProduction`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.accountRef` | ref → StacksAccount | | AccountRef selects signing identity. | |
| `spec.amountMicroSTX` | string | | AmountMicroSTX is transfer amount. | length ≤ 20; pattern `^(0\|[1-9][0-9]{0,19})$` |
| `spec.feeMicroSTX` | string | | FeeMicroSTX is transfer fee. | length ≤ 20; pattern `^(0\|[1-9][0-9]{0,19})$` |
| `spec.interval` | string | | Interval is the offered cadence. | length 2–32 |
| `spec.recipient` | object | | Recipient receives baseline transfers. | |
| `spec.recipient.accountRef` | ref → StacksAccount | | AccountRef selects a resolved public identity. | |
| `spec.recipient.address` | string | | Address is an explicit destination. | length ≤ 64; pattern `^ST[0-9A-HJKMNP-TV-Z]{26,39}$` |
| `spec.targetNodeRef` | ref → StacksNode | | TargetNodeRef selects native ingress. | |
| `spec.workerPlacement` | object | | WorkerPlacement applies to scoped support Pods. | |
| `spec.workerPlacement.nodeSelector` | map[string]string | | NodeSelector constrains eligible nodes. | properties ≤ 32 |
| `spec.workerPlacement.spreadAcrossNodes` | boolean | | SpreadAcrossNodes requests preferred same-kind anti-affinity. | |
| `spec.workerPlacement.tolerations` | []core/v1 Toleration | | Tolerations allow explicit support-node placement. | items ≤ 32 |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec.amountMicroSTX` | amount must fit uint64 | `uint(self) <= 18446744073709551615u` |
| `spec.feeMicroSTX` | amount must fit uint64 | `uint(self) <= 18446744073709551615u` |
| `spec.recipient` | select an account or address | `has(self.accountRef) != has(self.address)` |

<!-- END GENERATED: crd-reference -->

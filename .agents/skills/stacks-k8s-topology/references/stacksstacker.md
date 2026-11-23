# StacksStacker

A PoX maintenance worker definition, `stacks.stacks.org/v1alpha2`. It keeps one holder
stacked for one signer, across PoX-4 and PoX-5.

## Usage

- `holderAccountRef` holds the STX and must be funded in genesis with at least the
  stake plus fees. `administratorAccountRef` deploys and drives the PoX-5 direct-signer
  manager contract and needs a small balance.
- `signerRef` names a `StacksSigner` participant; `targetNodeRef` names the
  `StacksNode` participant used for reads and submissions.
- `amountMicroSTX` is the stake. `lockCycles` is 2–12; `renewWhenRemainingCycles` is
  1 to `lockCycles` − 1.
- Missing funds report `InsufficientFunds`; nothing is funded implicitly.

## Updates

Amount, `lockCycles`, renewal threshold and `control.paused` can change, subject to
unfinished bootstrap gates. A new amount applies at the next enrollment after unlock.
Reference changes need a new participant. Removing the participant does not unstake;
the lock expires through the protocol.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`stacks.stacks.org/v1alpha2`, kind `StacksStacker`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.administratorAccountRef` | ref → StacksAccount | | AdministratorAccountRef controls the direct manager. | |
| `spec.amountMicroSTX` | string | | AmountMicroSTX is desired stake. | length ≤ 20; pattern `^(0\|[1-9][0-9]{0,19})$` |
| `spec.holderAccountRef` | ref → StacksAccount | | HolderAccountRef funds stake. | |
| `spec.lockCycles` | integer (int32) | | LockCycles controls enrollment coverage. | 2–12 |
| `spec.renewWhenRemainingCycles` | integer (int32) | | RenewWhenRemainingCycles triggers maintenance. | 1–11 |
| `spec.signerRef` | ref → StacksSigner | | SignerRef identifies a consensus signer participant. | |
| `spec.targetNodeRef` | ref → StacksNode | | TargetNodeRef selects verified mutation ingress. | |
| `spec.workerPlacement` | object | | WorkerPlacement applies to scoped support Pods. | |
| `spec.workerPlacement.nodeSelector` | map[string]string | | NodeSelector constrains eligible nodes. | properties ≤ 32 |
| `spec.workerPlacement.spreadAcrossNodes` | boolean | | SpreadAcrossNodes requests preferred same-kind anti-affinity. | |
| `spec.workerPlacement.tolerations` | []core/v1 Toleration | | Tolerations allow explicit support-node placement. | items ≤ 32 |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec` | renewal threshold must be below lock cycles | `!has(self.lockCycles) \|\| !has(self.renewWhenRemainingCycles) \|\| self.renewWhenRemainingCycles < self.lockCycles` |
| `spec.amountMicroSTX` | amount must fit uint64 | `uint(self) <= 18446744073709551615u` |

<!-- END GENERATED: crd-reference -->

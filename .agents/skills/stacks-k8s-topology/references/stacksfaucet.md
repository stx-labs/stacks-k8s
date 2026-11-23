# StacksFaucet

An optional funding worker, `stacks.stacks.org/v1alpha2`. At most one per network. It
serves `StacksFaucetRequest`s.

## Usage

- `targetNodeRef` names the `StacksNode` participant it submits to; required.
- `accountRef` is optional. When omitted, a referenced definition owns a reusable
  default account, and an inline entry owns one that is deleted with it.
- `genesisBalanceMicroSTX` (default 1,000,000 STX) funds the faucet in genesis; zero
  means no allocation. `maxRequestMicroSTX` caps each request, inclusive.

## Updates

Request limits can change. Account and target changes need a new participant, and a
replacement cannot add genesis funds. There is no participant pause; root
`operation: Paused` holds sends.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`stacks.stacks.org/v1alpha2`, kind `StacksFaucet`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.accountRef` | ref → StacksAccount | | AccountRef selects funding identity; omission generates one. | |
| `spec.genesisBalanceMicroSTX` | string | | GenesisBalanceMicroSTX contributes one immutable allocation. | length ≤ 20; pattern `^(0\|[1-9][0-9]{0,19})$` |
| `spec.maxRequestMicroSTX` | string | | MaxRequestMicroSTX bounds each transfer. | length ≤ 20; pattern `^(0\|[1-9][0-9]{0,19})$` |
| `spec.targetNodeRef` | ref → StacksNode | | TargetNodeRef selects native ingress. | |
| `spec.workerPlacement` | object | | WorkerPlacement applies to scoped support Pods. | |
| `spec.workerPlacement.nodeSelector` | map[string]string | | NodeSelector constrains eligible nodes. | properties ≤ 32 |
| `spec.workerPlacement.spreadAcrossNodes` | boolean | | SpreadAcrossNodes requests preferred same-kind anti-affinity. | |
| `spec.workerPlacement.tolerations` | []core/v1 Toleration | | Tolerations allow explicit support-node placement. | items ≤ 32 |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec.genesisBalanceMicroSTX` | amount must fit uint64 | `uint(self) <= 18446744073709551615u` |
| `spec.maxRequestMicroSTX` | amount must fit uint64 | `uint(self) <= 18446744073709551615u` |

<!-- END GENERATED: crd-reference -->

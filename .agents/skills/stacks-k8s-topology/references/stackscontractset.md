# StacksContractSet

Required contract deployment and registry initialization, `stacks.stacks.org/v1alpha2`.
Exactly one per network.

## Usage

- `bundle` selects a closed catalog entry; `sbtc-regtest-v1` deploys the real sBTC
  prerequisite contracts.
- `deployerAccountRef` signs deployments and must be funded in genesis.
- `targetNodeRef` names the `StacksNode` participant used for submissions.
- `initialization.mode: ExplicitTestRegistry` registers `signerAccountRefs`,
  `aggregateKeyAccountRef` and `threshold`. These bridge keys are public inputs only;
  they need no funds.
- Contract principals are published from the deployer address and frozen into genesis.

## Updates

Only `control.paused` changes the active worker; other changes need a new participant
that agrees with the frozen contract identities. Deployed contracts are never
overwritten; a differing source reports `Conflict`.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`stacks.stacks.org/v1alpha2`, kind `StacksContractSet`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.bundle` | string | | Bundle selects the immutable source manifest. | one of `sbtc-regtest-v1` |
| `spec.deployerAccountRef` | ref → StacksAccount | | DeployerAccountRef supplies deployment identity. | |
| `spec.initialization` | object | | Initialization specifies registry postconditions. | |
| `spec.initialization.aggregateKeyAccountRef` | ref → StacksAccount | yes | AggregateKeyAccountRef supplies the explicit aggregate key. | |
| `spec.initialization.mode` | string | yes | Mode is the supported initialization authority. | one of `ExplicitTestRegistry` |
| `spec.initialization.signerAccountRefs` | []ref → StacksAccount | yes | SignerAccountRefs supplies public bridge identities. | items 2–100; keyed by `name` |
| `spec.initialization.threshold` | integer (int32) | yes | Threshold bounds registry authorization. | 1–100 |
| `spec.targetNodeRef` | ref → StacksNode | | TargetNodeRef selects mutation ingress. | |
| `spec.workerPlacement` | object | | WorkerPlacement applies to scoped support Pods. | |
| `spec.workerPlacement.nodeSelector` | map[string]string | | NodeSelector constrains eligible nodes. | properties ≤ 32 |
| `spec.workerPlacement.spreadAcrossNodes` | boolean | | SpreadAcrossNodes requests preferred same-kind anti-affinity. | |
| `spec.workerPlacement.tolerations` | []core/v1 Toleration | | Tolerations allow explicit support-node placement. | items ≤ 32 |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec.initialization` | threshold must not exceed signer count | `self.threshold <= size(self.signerAccountRefs)` |
| `spec.initialization` | threshold must be a strict signer majority | `self.threshold > size(self.signerAccountRefs) / 2` |

<!-- END GENERATED: crd-reference -->

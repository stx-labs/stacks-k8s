# BitcoinReorganization

Replaces the last `depth` (1–6) blocks on one `BitcoinNode` participant with depth+1
new blocks, `actions.stacks.org/v1alpha2`. Install, request and semantics:
[bitcoin-actions.md](bitcoin-actions.md).

## Usage

- The reorganization is local to that node; other nodes follow only through Bitcoin
  peering. Partition the node first to hold a competing fork.
- All three `boundaryPolicy` flags must be `true`: the action cannot check epoch, cycle
  or prepare-phase boundaries itself. Check them with `stacks-inspect` beforehand.
- Needs `bitcoinActions.reorganizationEnabled` on the network release and
  `bitcoinReorganization.enabled` on the action release.

## Updates

Immutable. Deleting it does not restore the replaced chain.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`actions.stacks.org/v1alpha2`, kind `BitcoinReorganization`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.address` | string | yes | Address receives replacement coinbase outputs. | length 14–128 |
| `spec.bitcoinNodeRef` | ref → BitcoinNode | yes | BitcoinNodeRef names the logical BitcoinNode participant. | |
| `spec.boundaryPolicy` | object | yes | BoundaryPolicy explicitly accepts unknown protocol schedule boundaries. | |
| `spec.boundaryPolicy.allowEpochBoundaryCrossing` | boolean | yes | AllowEpochBoundaryCrossing permits an epoch boundary in the affected range. | |
| `spec.boundaryPolicy.allowPreparePhaseBoundaryCrossing` | boolean | yes | AllowPreparePhaseBoundaryCrossing permits a PoX prepare-phase boundary. | |
| `spec.boundaryPolicy.allowRewardCycleBoundaryCrossing` | boolean | yes | AllowRewardCycleBoundaryCrossing permits a reward-cycle boundary. | |
| `spec.depth` | integer (int32) | yes | Depth is the suffix length; exactly depth+1 replacement blocks are requested. | 1–6 |
| `spec.networkUID` | string | yes | NetworkUID pins the canonical network incarnation. | length 1–128 |
| `spec.timeout` | string | yes | Timeout bounds new invalidation and generation from object creation. | length ≤ 32 |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec` | action spec is immutable | `self == oldSelf` |
| `spec.boundaryPolicy.allowEpochBoundaryCrossing` | unknown schedule requires explicit epoch opt-in | `self == true` |
| `spec.boundaryPolicy.allowPreparePhaseBoundaryCrossing` | unknown schedule requires explicit prepare-phase opt-in | `self == true` |
| `spec.boundaryPolicy.allowRewardCycleBoundaryCrossing` | unknown schedule requires explicit reward-cycle opt-in | `self == true` |
| `spec.timeout` | timeout must be positive | `duration(self) > duration('0s')` |
| `spec.timeout` | timeout cannot exceed 10m | `duration(self) <= duration('10m')` |

<!-- END GENERATED: crd-reference -->

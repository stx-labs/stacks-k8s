# BitcoinBlockGeneration

Mines 1–100 Bitcoin blocks on one `BitcoinNode` participant, one acknowledged RPC per
block, `actions.stacks.org/v1alpha2`. Install, request and semantics:
[bitcoin-actions.md](bitcoin-actions.md).

## Usage

- `cadence.mode`: `Immediate` (no wait), `Fixed` (`intervalSeconds`), `Uniform`
  (`minSeconds`–`maxSeconds`) or `Explicit` (`delaysSeconds`, count minus one entries).
  Delays start from each receipt.
- `timeout` covers pending time too; a busy target can consume it before the first block.
- Blocks pay `address`; use a wallet's `status.bitcoinAddress`.

## Updates

Immutable. Delete to cancel; blocks already acknowledged remain.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`actions.stacks.org/v1alpha2`, kind `BitcoinBlockGeneration`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.address` | string | yes | Address receives the regtest coinbase outputs. | length 14–128 |
| `spec.bitcoinNodeRef` | ref → BitcoinNode | yes | BitcoinNodeRef names one logical BitcoinNode participant in the pinned network. | |
| `spec.cadence` | object | yes | Cadence controls delays between receipts and subsequent single-block requests. | |
| `spec.cadence.delaysSeconds` | []integer (int32) | | DelaysSeconds contains count minus one ordered delays; zero adds no intentional wait. | items ≤ 99 |
| `spec.cadence.intervalSeconds` | integer (int32) | | IntervalSeconds is the fixed receipt-relative delay. | 1–60 |
| `spec.cadence.maxSeconds` | integer (int32) | | MaxSeconds is the inclusive upper uniform bound. | 1–60 |
| `spec.cadence.minSeconds` | integer (int32) | | MinSeconds is the inclusive lower uniform bound. | 1–60 |
| `spec.cadence.mode` | string | yes | Mode selects immediate, fixed, inclusive uniform, or explicit inter-block delays. | one of `Immediate`, `Fixed`, `Uniform`, `Explicit`; length ≤ 9 |
| `spec.count` | integer (int32) | yes | Count bounds acknowledged single-block RPCs. | 1–100 |
| `spec.networkUID` | string | yes | NetworkUID pins the canonical network incarnation. | length 1–128 |
| `spec.timeout` | string | yes | Timeout includes all pending time from API-server creation. | length ≤ 32 |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec` | action spec is immutable | `self == oldSelf` |
| `spec` | explicit cadence requires count minus one delays | `self.cadence.mode != 'Explicit' \|\| (has(self.cadence.delaysSeconds) ? size(self.cadence.delaysSeconds) : 0) == self.count - 1` |
| `spec.cadence` | intervalSeconds is required only for Fixed | `has(self.intervalSeconds) == (self.mode == 'Fixed')` |
| `spec.cadence` | minSeconds and maxSeconds are required only for Uniform | `has(self.minSeconds) == (self.mode == 'Uniform') && has(self.maxSeconds) == (self.mode == 'Uniform')` |
| `spec.cadence` | uniform bounds must be ordered | `self.mode != 'Uniform' \|\| self.minSeconds <= self.maxSeconds` |
| `spec.cadence` | delaysSeconds is allowed only for Explicit | `!has(self.delaysSeconds) \|\| self.mode == 'Explicit'` |
| `spec.timeout` | timeout must be positive | `duration(self) > duration('0s')` |
| `spec.timeout` | timeout cannot exceed 10m | `duration(self) <= duration('10m')` |

<!-- END GENERATED: crd-reference -->

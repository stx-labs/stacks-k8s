# StacksFaucetRequest

One bounded funding request against a running network's faucet,
`stacks.stacks.org/v1alpha2`. It is not a participant.

## Usage

- `networkUID` must equal the current root's `metadata.uid`; `faucetRef` names the
  faucet participant.
- `destination` is exactly one `accountRef` or testnet address.
- `amountMicroSTX` must not exceed the faucet's `maxRequestMicroSTX`. `timeout` is at
  most 1h and counts from creation.
- Phases: `Pending`, `Submitted`, `Completed`, `Rejected`, `Inconclusive`, `Expired`.
  `Completed` means the exact transfer was observed included.

## Updates

The spec is immutable and terminal requests are never retried. Deleting a request
cancels it only if the worker has not dispatched it yet.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`stacks.stacks.org/v1alpha2`, kind `StacksFaucetRequest`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.amountMicroSTX` | string | yes | AmountMicroSTX is a positive uint64 transfer amount. | length ≤ 20; pattern `^(0\|[1-9][0-9]{0,19})$` |
| `spec.destination` | object | yes | Destination is resolved once to a concrete public address. | |
| `spec.destination.accountRef` | ref → StacksAccount | | AccountRef selects a resolved public identity. | |
| `spec.destination.address` | string | | Address is an explicit destination. | length ≤ 64; pattern `^ST[0-9A-HJKMNP-TV-Z]{26,39}$` |
| `spec.faucetRef` | ref → StacksFaucet | yes | FaucetRef selects one logical faucet participant in this namespace. | |
| `spec.networkUID` | string | yes | NetworkUID prevents a request from following a replacement environment. | length 1–128 |
| `spec.timeout` | string | yes | Timeout starts at creationTimestamp; the controller never extends it. | length 2–32 |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec` | request spec is immutable | `self == oldSelf` |
| `spec.amountMicroSTX` | amount must be positive | `self != '0'` |
| `spec.amountMicroSTX` | amount must fit uint64 | `uint(self) <= 18446744073709551615u` |
| `spec.destination` | select an account or address | `has(self.accountRef) != has(self.address)` |
| `spec.timeout` | timeout must be positive and at most one hour | `duration(self) > duration('0s') && duration(self) <= duration('1h')` |

<!-- END GENERATED: crd-reference -->

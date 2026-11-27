# BitcoinExecution

Generated, network-owned record of block generation on one Bitcoin target,
`bitcoin.stacks.org/v1alpha2`. Never applied by users.

## Usage

It retains each generation request and its outcome, including uncertain RPCs, so that
participant removal or controller restarts cannot repeat or lose generation. Read it
when Bitcoin production stalls or a target reports closed.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`bitcoin.stacks.org/v1alpha2`, kind `BitcoinExecution`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.networkUID` | string | yes | NetworkUID is the owning network incarnation. | |
| `spec.offer` | object | | Offer is the scheduler's sole bounded generation opportunity. | |
| `spec.offer.address` | string | yes | Address is the exact public coinbase destination. | length ≤ 128 |
| `spec.offer.ceiling` | integer (int64) | yes | Ceiling is the frozen gate's maximum permitted resulting height. | ≥ 1 |
| `spec.offer.count` | integer (int32) | | Count is the number of blocks this opportunity generates in one request; omission is one. Only first-gate bootstrap opportunities of a batching network may exceed one. | 1–500 |
| `spec.offer.expectedHeight` | integer (int64) | yes | ExpectedHeight is the converged pre-dispatch height. | ≥ 0 |
| `spec.offer.expectedTip` | string | yes | ExpectedTip is the converged pre-dispatch block hash. | pattern `^[0-9a-f]{64}$` |
| `spec.offer.expiresAt` | string (date-time) | yes | ExpiresAt prevents a stale opportunity from initiating a new request. | |
| `spec.offer.initialization` | object | yes | Initialization binds the retained network scheduler record. | |
| `spec.offer.initialization.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.offer.initialization.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.offer.initialization.name` | string | yes | Name is the public resource name. | |
| `spec.offer.initialization.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.offer.mode` | string | | Mode distinguishes frozen bootstrap advancement from completed-initialization baseline authority. Omission preserves existing bootstrap records. | one of `Bootstrap`, `Baseline` |
| `spec.offer.number` | integer (int64) | yes | Number is monotonic within the initialization record. | ≥ 1 |
| `spec.offer.override` | object | | Override pins the active timing request, when this opportunity used one. | |
| `spec.offer.override.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.offer.override.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.offer.override.name` | string | yes | Name is the public resource name. | |
| `spec.offer.override.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.offer.policyDigest` | string | yes | PolicyDigest identifies the admitted production policy. | |
| `spec.offer.production` | object | yes | Production identifies the admitted production participant. | |
| `spec.offer.production.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.offer.production.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.offer.production.name` | string | yes | Name is the public resource name. | |
| `spec.offer.production.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.offer.target` | object | | Target binds the sole selected participant for a baseline opportunity. | |
| `spec.offer.target.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.offer.target.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.offer.target.name` | string | yes | Name is the public resource name. | |
| `spec.offer.target.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.offer.wallet` | object | yes | Wallet pins the payout identity used for accounting. | |
| `spec.offer.wallet.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.offer.wallet.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.offer.wallet.name` | string | yes | Name is the public resource name. | |
| `spec.offer.wallet.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |
| `spec.participant` | object | yes | Participant is the exact BitcoinNode participant. | |
| `spec.participant.fingerprint` | string | | Fingerprint identifies public content when relevant. | |
| `spec.participant.kind` | string | yes | Kind identifies the API kind or participant kind. | |
| `spec.participant.name` | string | yes | Name is the public resource name. | |
| `spec.participant.uid` | string | yes | UID prevents same-name replacement from inheriting authority. | |

<!-- END GENERATED: crd-reference -->

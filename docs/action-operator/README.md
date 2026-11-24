# Action operator

The optional action operator serves `actions.stacks.org/v1alpha2` and runs one
lifecycle controller per enabled kind: `BitcoinBlockGeneration` (enabled by default)
and `BitcoinReorganization`. Each action resource is one bounded mechanism against
one logical target. The agent chooses ordering, overlap and interpretation; action
specs have no lists, stages, dependencies or other orchestration fields.

[Bitcoin actions](bitcoin-actions.md) describes both kinds. This page covers the
common lifecycle, RBAC and operation.

## Two writers

| Component | Writes | Does not |
| --- | --- | --- |
| Bitcoin control worker (network operator, one per Bitcoin node) | Admission, the reservation on its `BitcoinExecution`, Armed RPCs and receipts | Write action status or finalizers |
| Lifecycle controller (action operator) | Action status and the `actions.stacks.org/action-cleanup` finalizer | Send RPCs, read Secrets or write execution records |

The worker sends only after the lifecycle controller has copied the admission into
the action's status. The lifecycle controller projects status from the execution
record by uncached reads and polls every second until the action settles. It emits
no Kubernetes Events.

## Install

Install the action chart before enabling the network operator's action values. The
action chart installs the action CRDs. When `bitcoinActions.generationEnabled` or
`bitcoinActions.reorganizationEnabled` is set, every Bitcoin control worker lists
the enabled kinds on each pass and fails that pass before serving the baseline
offer if the list fails, so enabling a value while its CRD is absent stops baseline
Bitcoin production.

1. Install the [action chart](../../charts/stacks-action-operator/README.md) in the
   network namespace. Add `--set bitcoinReorganization.enabled=true` for
   reorganization.

   ```bash
   helm upgrade --install actions charts/stacks-action-operator \
     --namespace lab --set image.tag=dev
   ```

2. Enable the matching values on the cluster-wide network operator release. Both
   default to false.

   ```bash
   helm upgrade network charts/stacks-network-operator -n stacks-network-system \
     --reuse-values --set bitcoinActions.generationEnabled=true
   ```

A kind must be enabled on both sides. The worker admits only requests that already
carry the cleanup finalizer, so requests in a namespace without the lifecycle
controller are never admitted. Keep the action CRDs installed while either network
value is enabled. Helm does not upgrade CRDs; see
[installation compatibility](../network-operator/migration.md).

## Requests

Both kinds share these spec fields:

| Field | Contract |
| --- | --- |
| `networkUID` | Exact UID of the namespace's `StacksNetwork` (always named `network`). |
| `bitcoinNodeRef.name` | Logical `BitcoinNode` participant name, not a Pod or StatefulSet. |
| `timeout` | Positive duration of at most `10m`, counted from `metadata.creationTimestamp`. Pending time counts. |

The whole spec is immutable (CEL `self == oldSelf`); create a new object for another
attempt. Deletion is the cancellation signal. A namespace `ResourceQuota` allows 64
objects per kind.

`actions.stacks.org/correlation-id` is an optional label for searching evidence; it
is also used on native Chaos Mesh faults. It is snapshotted into
`status.correlationID` at admission. It is not an attribution, authorization or
idempotency key; the object UID is authoritative.

## Admission

On each pass without a held reservation, the worker for a Bitcoin node:

1. Lists every enabled kind in the namespace (at most 64 each; an incomplete list
   fails the pass).
2. Orders requests by creation time, then UID.
3. Skips requests for another network UID or node, deleted, terminal, already
   admitted, without the cleanup finalizer, expired, failing the payout-address
   check, or beyond the current initialization ceiling.
4. Re-reads the first remaining request uncached and writes the reservation to its
   `BitcoinExecution` with an optimistic-lock update.

Ineligible requests do not block baseline production; they stay `Pending` until
they expire. Status then records the admitted network (`name`, `uid`,
`observedGeneration`), the exact target (participant, configuration, credentials,
StatefulSet, revision, Pod, container and image identity) and
`admittedPolicy`, which identifies the execution record and its RPC policy digest.
Identity changes after admission stop further sends.

## Lifecycle

```text
Pending -> Admitted -> Active -> Completed
                         |
                         +-----> Recovering -> Failed | Inconclusive

Any nonterminal phase -> Failed | Inconclusive
```

| Phase | Meaning |
| --- | --- |
| `Pending` | Not admitted; no side effect. |
| `Admitted` | Reservation held and acknowledged; nothing sent yet. |
| `Active` | At least one RPC authorized (`status.startedAt`). |
| `Recovering` | A reorganization is running its compensating `reconsiderblock`. |
| `Completed` | Every requested RPC acknowledged; see each kind for its endpoint. |
| `Failed` | A definite stop with no unknown effect or cleanup. Not a protocol verdict. |
| `Inconclusive` | Effect, identity or cleanup cannot be established. |

`Completed`, `Failed` and `Inconclusive` are terminal and frozen: the outcome is
never revised, while later receipts and cleanup facts are still copied into status.
The API also defines `Recovered` for reversible actions; neither current kind uses
it.

| Condition | `True` when | Reason |
| --- | --- | --- |
| `Admitted` | `status.admittedAt` is set | `AdmissionObserved` |
| `Progressing` | The phase is not terminal | The current phase while running; the terminal reason afterwards |
| `EffectObserved` | The phase is `Completed` | `ReceiptEvidence` |
| `CleanupComplete` | No Armed RPC, no unacknowledged reorganization cleanup and no unsafe cleanup | `CleanupEvidence` |

Terminal reasons written by the lifecycle controller:

| Reason | Outcome |
| --- | --- |
| `ReceiptsComplete` | `Completed`. |
| `NoDispatch` | `Failed`: deleted or expired before admission, with the execution inventory readable. |
| `AdmissionUnavailable` | `Inconclusive`: deleted or expired before admission, but the execution inventory could not be read. |
| `ExecutionUnavailable` | `Inconclusive`: admitted, but the bound execution record is gone or replaced. |
| `EnvironmentDisposed` | `Inconclusive`: the network was deleted, is deleting or was replaced. |
| `EffectUncertain` | `Inconclusive`: an Armed RPC has no receipt, or cleanup became unsafe. |
| `CleanupDeadlineExceeded` | `Inconclusive`: reorganization cleanup was not acknowledged by the cleanup horizon. |

Otherwise the outcome is `Failed` with the worker's stop reason, for example
`ActionCancelled`, `DeadlineExceeded`, `NetworkStopped`, `NetworkDeleting`,
`ParticipantRemoved`, `NetworkPaused` or a reorganization
verification reason. `IdentityDiverged` is `Inconclusive` once a send was
authorized. The API package also declares `ActionDeadlineExceeded` and
`MechanismFailed`; neither is currently emitted.

### Finalizer and release

The lifecycle controller adds `actions.stacks.org/action-cleanup` before admission.
The worker releases its reservation only after terminal status acknowledges every
receipt and cleanup fact. The lifecycle controller then removes the finalizer. An
Armed RPC without a receipt, or unacknowledged reorganization cleanup, keeps the
reservation, the finalizer and the node's exclusion even if the action is deleted.
Deleting the network releases the finalizer as `EnvironmentDisposed` without
claiming cleanup or server quiescence. Unknown delivery is never replayed.

## Time and clocks

`status.expiresAt` is `creationTimestamp + timeout`. The deadline bounds new
admission and new sends. Reorganization compensation has a separate fixed horizon of
`expiresAt + 30s`, never reset by restart.

The API server sets the creation time, while the Bitcoin control worker and the
lifecycle controller each compare the deadline and the cleanup horizon against their
own clocks. Keep node clocks synchronized. If the lifecycle controller's clock runs
ahead, it can freeze `Inconclusive`/`CleanupDeadlineExceeded` while the worker still
completes compensation; `status.cleanupAcknowledged` then becomes `true` under the
frozen outcome. Cadence delays are minimums with no upper latency bound.

## RBAC

The chart's namespaced Role grants:

- `get` on `stacksnetworks`, `stacksnetworkparticipants` and `bitcoinexecutions`;
- `get`, `list`, `watch` and `patch` on each enabled action kind, and `patch` on
  its `status`; `patch` on the primary resource is required for the finalizer;
- `create` and `patch` on `events`, and `get`, `list`, `watch`, `create`, `update`
  and `patch` on `leases`, for leader election.

It grants no Secret, workload or execution-record writes. Each Bitcoin control
worker's Role gets `get` and `list` on the action kinds enabled on the network
operator. No user-facing Roles are shipped. `chart_test.go` in
[`operators/action`](../../operators/action/README.md) checks the exact rendered
permissions.

## Operation

```bash
kubectl -n lab get bitcoinblockgenerations,bitcoinreorganizations
kubectl -n lab get bitcoinblockgeneration three-blocks -o yaml
```

Fill `networkUID` and `address` in the [examples](../../examples/actions/) from the
running network. Read `phase` together with the conditions and mechanism fields.
Completion reports acknowledged RPCs on one node; it is not a consensus or recovery
verdict.

- **Cancel**: delete the action. A sent RPC cannot be retracted.
- **Pause**: pausing the network holds new sends. A held generation resumes on
  unpause or expires. A reorganization that already invalidated its boundary stops
  with `NetworkPaused` and runs its compensation.
- **Disable a kind**: change installation values only after reservations settle or
  the environment is disposed. Disabling removes the worker's read permission for
  that kind; it is not cancellation and can strand retained uncertainty or cleanup.
- **Teardown**: let requests reach a terminal phase, or delete the network
  following [network cleanup](../network-operator/operations.md). Uninstall the
  action release only after its requests have released their finalizers.

## Limits

- CRD bounds are the only admission policy. There is no administrator-owned safety
  policy, no aggregate accounting across kinds or with native faults, and no
  fair-share arbitration with baseline production (see
  [Bitcoin actions](bitcoin-actions.md)).
- A missing action CRD for an enabled value appears only as a per-pass list error in
  the worker, not as a named condition.
- Clock skew between nodes can make the cleanup outcome disagree with later cleanup
  evidence, as described above.

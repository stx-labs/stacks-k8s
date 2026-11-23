# NetworkObservation

A one-shot, read-only snapshot of every Bitcoin node, Stacks node and signer's
identity: participant, workload, Pod, container, requested and resolved image,
rollout revision, Service routing and configuration, `observation.stacks.org/v1alpha1`.
It works against `v1alpha2` networks. The namespace must be enrolled in, or be the
installation namespace of, an observability release.

## Usage

```yaml
apiVersion: observation.stacks.org/v1alpha1
kind: NetworkObservation
metadata: {name: before-fault, namespace: lab-x}
spec:
  networkRef: {name: network}
```

- `Ready` means every selected actor's identity was directly verified. `Pending` waits
  up to `pendingTimeoutSeconds` for unfinished actors; read-time drift or replacement
  gives `Inconclusive`.
- `status.binding.snapshotDigest` identifies the snapshot. Set
  `expectedSnapshotDigest` on a later observation to check nothing changed.
- Private configuration content is controller-reported evidence; Secrets are never read.
  A snapshot is not proof of protocol health.

## Updates

One observation per generation. Create a new resource for another point in time.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`observation.stacks.org/v1alpha1`, kind `NetworkObservation`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.expectedInventoryDigest` | string | | ExpectedInventoryDigest binds only a legacy v1alpha1 network inventory. | pattern `^sha256:[0-9a-f]{64}$` |
| `spec.expectedSnapshotDigest` | string | | ExpectedSnapshotDigest binds the independently observed v1alpha2 actor snapshot. | pattern `^sha256:[0-9a-f]{64}$` |
| `spec.networkRef` | ref → StacksNetwork | yes | NetworkRef selects a StacksNetwork in this namespace. | |
| `spec.pendingTimeoutSeconds` | integer (int32) | | PendingTimeoutSeconds bounds how long the observation waits for a ready topology. | default `300`; 10–86400 |
| `spec.timeoutSeconds` | integer (int32) | | TimeoutSeconds bounds the complete direct-read observation. | default `10`; 1–30 |

<!-- END GENERATED: crd-reference -->

# NetworkTelemetry

A continuous recording of one network into the telemetry backend,
`observation.stacks.org/v1alpha2`. The namespace must be enrolled in the telemetry
release's `watchNamespaces`.

## Usage

- `networkUID` must be the live root UID at creation. The recording has no owner
  reference: it outlives the root, and a same-name replacement is not recorded.
- `storageSecretRef` names a same-namespace Secret with `endpoint` and `authorization`
  keys (ingest credential). Use an immutable Secret.
- `objects` records Kubernetes objects and attributed faults; `logs` and `metrics` add a
  node collector DaemonSet. Container CPU and memory are always recorded and need
  Metrics Server.
- Status: `WorkloadsReady`, `status.recording.heartbeatAt` (must be recent),
  per-source `observedAt`, and `status.tablePrefix`.

## Updates

`sources` is mutable; a change rolls the recording workloads and leaves a capture
boundary. Network identity, storage Secret and retention are immutable: create a new
recording to change them. Deleting it keeps backend tables until their TTL.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`observation.stacks.org/v1alpha2`, kind `NetworkTelemetry`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.networkName` | string | yes | NetworkName selects the source root; its UID must match before collector creation. | length ≤ 63; pattern `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` |
| `spec.networkUID` | string | yes | NetworkUID prevents same-name replacement from inheriting collection. | pattern `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$` |
| `spec.retention` | object | yes | Retention sets immutable table TTL. | |
| `spec.retention.window` | string | yes | Window is the log/object TTL; native metrics use the backend profile's fixed 24h TTL. TTL is not a hard byte quota or immediate physical erasure guarantee. | one of `1h`, `6h`, `24h` |
| `spec.sources` | object | yes | Sources controls future capture; changes can restart collectors and create explicit gaps. | |
| `spec.sources.logs` | boolean | yes | Logs captures redacted workload stdout/stderr. | |
| `spec.sources.metrics` | boolean | yes | Metrics scrapes named native metrics ports; actor container CPU/memory does not depend on this field. | |
| `spec.sources.objects` | boolean | yes | Objects captures allowlisted public Kubernetes observations. | |
| `spec.storageSecretRef` | string | yes | StorageSecretRef names administrator-provisioned endpoint and ingestion credentials. | length ≤ 63; pattern `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec` | network identity is immutable | `self.networkName == oldSelf.networkName && self.networkUID == oldSelf.networkUID` |
| `spec` | storage and retention require a new recording | `self.retention == oldSelf.retention && self.storageSecretRef == oldSelf.storageSecretRef` |
| `spec` | select at least one source | `self.sources.objects \|\| self.sources.logs \|\| self.sources.metrics` |

<!-- END GENERATED: crd-reference -->

# Releases

## Products

The three operators are independently installed and versioned products in one
repository. Each chart owns only its CRDs and RBAC.

| Product | Responsibility | Dependency direction |
| --- | --- | --- |
| `apis/network` module | Versioned Kubernetes API types for the `network`, `bitcoin`, `stacks` and `actions` groups | Kubernetes API machinery only |
| `stacks-network-operator` | Topology, baseline Bitcoin production, Stacks operation and their scoped workers; network, Bitcoin and Stacks CRDs, including `BitcoinBlockScheduleOverride` | `apis/network`, `libs/stacks`, `libs/bitcoin` |
| `stacks-action-operator` | Bounded action lifecycles; `BitcoinBlockGeneration` and `BitcoinReorganization` CRDs | `apis/network`; requires the network operator, whose worker executes the RPC |
| `stacks-observability-operator` | Passive identity observation and telemetry recording; `NetworkObservation` and `NetworkTelemetry` CRDs; optional bundled GreptimeDB | `apis/network`; reads network, action and Chaos Mesh APIs without importing their runtime code |
| Chaos Mesh | Native infrastructure faults | External optional upstream chart |

The network chart is useful alone: baseline operation needs no action, Chaos Mesh or
observability installation. The observability operator starts
without optional action or Chaos Mesh CRDs and reports their absence as unavailable
sources. Investigation tools under `tools/stacks-*` are independent Go modules, not
operator runtime dependencies.

## CRDs

- CRDs are generated from API types and committed into their owning chart.
- CRDs are cluster-wide even for namespaced objects. Namespace isolation does not
  permit incompatible versions of one CRD in a cluster.
- Each API serves one alpha version. The network chart refuses to install over
  existing network CRDs that serve another version.
- Helm installs files under `crds/` only on first installation and performs no API
  conversion. Apply changed CRDs explicitly before upgrading; see
  [installation compatibility](network-operator/migration.md).
- Chaos Mesh CRDs come from its upstream chart and are never copied.

No external version-skew compatibility is claimed. Document the network API and
contract versions accepted by each observability and action release.

## Image and Git-source boundary

Operators accept immutable OCI image references. They never clone repositories or
run build scripts. An external agent or CI system:

1. checks out the chosen repository and revision;
2. builds and scans the image outside operator trust;
3. pushes it to an accessible registry, or loads it into the local kind cluster;
4. records its content identity (pinning published images by digest); and
5. patches the relevant actor image and configuration reference.

See the [local actor image workflow](network-operator/actor-images.md) for revision
selection, unique local tags and mixed-version updates. Recorded image metadata is
not an attestation that the image matches a source revision.

## Versioning

Each release updates its chart `version`, chart `appVersion`, image tag and release
notes together. Use component-prefixed product tags:

```text
stacks-network-operator/v0.1.0
stacks-observability-operator/v0.1.0
stacks-action-operator/v0.1.0
```

If a Go module is published for external import, its semantic-version tag must
match its repository subdirectory instead:

```text
apis/network/v0.1.0
libs/stacks/v0.1.0
operators/network/v0.1.0
```

Publish module tags in dependency order: `apis/network`, `libs/bitcoin` and
`libs/stacks` before the operator and tool modules that require them. Repository
builds use local `replace` directives, but published consumers ignore them and must
resolve the declared versions. The API module never includes controller
implementations.

Operator binaries are distributed as container images through their Helm charts.
Version-qualified `go install` is not supported because repository builds rely on
the local replacements.

Before tagging:

1. run `make verify`, `make vuln`, `make docker-check`, and `make docker-build`;
2. build and publish an immutable multi-architecture image;
3. set the chart image tag to the released image version;
4. install the packaged chart in a clean cluster and exercise its examples;
5. publish each chart independently as an OCI artifact.

No repository workflow publishes images or charts, or produces signatures, SBOMs or
provenance; CI runs verification, security and dependency-review workflows only.

## Current versions

The network and observability charts are at `0.2.0`; the action chart is at `0.1.0`.
Chart default controller image tags are `dev` for network and action and `0.1.0` for
observability. The Bitcoin observer image is pinned once in the network chart
[values.yaml](../charts/stacks-network-operator/values.yaml), independently of the
controller tag; local builds must load that image or override `bitcoinObserverImage`.
Unpublished incompatible chart or schema changes require fresh test installations.

## Worker artifacts

The network image contains its operator/resolver entrypoint, the scoped Go worker
binary and the Bitcoin observer. Test controller and worker images with the same
API/chart version and selected actor profile. Bitcoin control and Stacks management
have different restart contracts; do not infer a safe management-worker restart from
an operator rollout.

`operators/network/transactions` is an offline SDK oracle only. Include its pinned
test dependencies in repository auditing, but do not package Node.js in runtime
images.

# Shared contracts

Wire-contract fixtures shared by independent consumers. A fixture is not a shared
implementation: each consumer decodes the same bytes through its own production code.

| Fixture | Contract | Consumers |
| --- | --- | --- |
| [`image-id-v1.json`](image-id-v1.json) | Immutable image-digest extraction from kubelet container-status values | Observability operator topology boundary tests |
| [`investigation-preflight-v1.json`](investigation-preflight-v1.json) | Telemetry fields, observer scope and Chaos namespace enrollment read by `stacks-preflight` | `tools/stacks-preflight`, observability operator, chart-policy telemetry tests |
| [`stacks-identity/v1/`](stacks-identity/v1/README.md) | Public key, address and identity encodings | `libs/stacks/identity` |
| [`stacks-protocol-v1/`](stacks-protocol-v1/README.md) | Clarity values, signed transactions, structured signatures and PoX-4 transactions from the pinned Stacks.js oracle | `libs/stacks` |

`investigation-preflight-v1.json` is a partial read-contract vector, not a complete
admissible `NetworkTelemetry` manifest; chart tests independently verify the served
resource, emitted arguments and enrollment predicate.

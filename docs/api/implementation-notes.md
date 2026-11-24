# Implementation notes

Implementation contracts behind the public API: status field managers, generated names
and native fault selection. The [network runtime](../network-operator/runtime.md) describes
visible workloads, status and permissions.

## Shared status apply contract

Aggregate participant status writes use server-side apply alongside domain writers. Every writer
of a shared status object, including faucet requests,
uses the status subresource and constructs a minimal apply object from its assigned
fields. Include apiVersion, kind, metadata.name/namespace and current UID/resourceVersion
preconditions; do not apply a fetched object's whole status or metadata. A failed
identity/conflict check requires a fresh read and authorization check, never adoption
or creation of a replacement object.

Field managers are fixed across reconciles, process restarts and workload replacements:

| Manager | Allowed status payload |
| --- | --- |
| stacks-network-aggregate | Participant admission and conditions Resolved, PolicyDeferred and AdmissionReady. The aggregate separately owns complete root status with optimistic concurrency. |
| stacks-network-domain-\<kind\> | Participant runtime and WorkloadReady, PlacementReady and ConfigVerified conditions. kind is the lowercase API kind, including bitcoinblockproduction; no admission or worker execution. |
| stacks-network-domain-bitcoinblockproduction-scheduling | Only participant scheduling, projected from the durable BitcoinInitialization scheduler record. |
| stacks-network-domain-bitcoincontrol | Only participant bitcoinControl lifecycle evidence. |
| stacks-network-worker-execution | Only execution on the exact bound Stacks management participant; shared fixed manager name across roles. |
| stacks-network-faucet-request | Faucet request admission, phase and controller conditions. |
| stacks-network-faucet-execution | Only faucet request execution from the bound faucet worker. |
| stacks-bitcoin-schedule-override | Only BitcoinBlockScheduleOverride lifecycle status. |

Use typed, bounded status fields. Shared status objects and independently owned
subtrees use structural granular maps, never a shared atomic parent or an untyped
preserve-unknown-fields bucket. Every conditions array is a map-list with
`x-kubernetes-list-type: map` and `x-kubernetes-list-map-keys: [type]` (Go markers
`+listType=map`, `+listMapKey=type`). The entire condition entry belongs to one writer;
do not split its status, reason, message, observedGeneration or lastTransitionTime
between managers. Workers put execution observations in execution, not controller
condition entries.

For example, a domain readiness apply contains only identity/preconditions and
status.runtime plus its own conditions, with no status.admission, execution, Resolved
or PolicyDeferred. Omitting one of its previously applied fields/condition entries
removes that writer's field when it has no other owner. Never send null/empty values
for another writer's subtree. ForceOwnership affects every submitted field, so its
use for migration or an explicit condition handoff is restricted to this minimal
assigned payload. RBAC cannot enforce field-level ownership.

The aggregate converts a verified legacy status Update manager to its apply manager
with client-go's csaupgrade helper, using the status subresource and UID/resourceVersion
guards and preserving unrelated managers. Force apply alone can leave legacy ownership
of unchanged fields and prevent later removal.

The aggregate submits a WorkloadReady fallback only for kinds without a registered domain
controller; the operator registers one for every participant kind. The domain controller
owns the entire WorkloadReady entry and publishes actual readiness, including
False/Unknown until proven ready. Manager identity alone does not authorize a new worker;
exact current bindings remain mandatory.

## Go protocol library and runtime boundary

Operators and workers use the independent Go modules [libs/stacks](../../libs/stacks/README.md)
(native node RPC, Clarity codecs, address/key encoding, transaction construction/signing,
SIP-018 and PoX signing) and `libs/bitcoin` (Core RPC). Each has no Kubernetes or
operator dependency; consumers require a version and use a relative local `replace`.
Enrollment/renewal decisions, nonce ownership and sBTC/signer-manager administration stay
in workers, outside the libraries. Stacks.js 7.6.0 is a pinned test-only oracle; operator,
resolver and worker images contain no Node/JavaScript runtime.

## Generated names

Runtime root names have a **52-character maximum**, leaving room for generated
children. Start with `n-<network-token>-<kind>-<participant-name>-<purpose>`, where the token
is the first 12 lowercase hex characters of SHA-256 of the full network UID. Keep
at most its first 39 characters (trim trailing hyphens), then append `-` and 12 hex
characters of SHA-256 over the compact JSON string array
`[networkUID, participantUID, kind, participantName, purpose]`. This suffix is always present;
full UIDs remain in labels/status and are verified on every ownership check. Kind
tokens are lowercase API kind names; purpose uses lowercase DNS-label characters.
Config artifact purposes include the hexadecimal content digest without a colon. A collision is
an error, never adoption.

Actor StatefulSets use ordinal 0, so their Pod hostname/name is at most 54 characters.
The fixed claim-template name data gives `data-<root>-0`, at most 59. Deployment names
are also capped at 52, leaving room for ReplicaSet/Pod suffixes; Job roots use the same
limit. Services are at most 52. No user-supplied ordinal or claim-template prefix is
supported.

Status publishes actual endpoints; clients never invent DNS or follow a stable
definition-name alias into the next network. Selectors include full network UID. Reusable
identity names use `<faucet>-account` or `<node>-identity`, with no network prefix;
if over 63 characters, use the first 50 (trim trailing hyphens) plus `-` and 12 hex
characters of SHA-256 of that full candidate. These are CR/Secret names, not
StatefulSet root names; resolver workload roots still follow the 52-character limit
using the identity source UID/name in place of participant UID/name and an empty
networkUID when resolved independently. Generated participant CR names use
prefix `n-<network-token>-participant-<participant-name>`, truncated/trimmed to 39
characters, followed by `-` and 12 hex characters of SHA-256 over the compact JSON
array [networkUID, participantName, "participant"]. No participant UID is used, because
it is not known until creation. Root status records that created
UID before any runtime activation; same-name reconstruction after loss is forbidden.

Inline default accounts are named `<generated participant name>-identity` (Stacks nodes)
or `<generated participant name>-account` (faucets), with the same 63-character rule, and
are owned by that participant. An existing same-name account owned by another object is
never adopted.

Full reusable source names remain in the `network.stacks.org/source-name` annotation and
status; only source kind/UID enter provenance labels. Source names are never truncated into
an ambiguous label, and reusable definitions are not held to the participant-name limit.

## Native Chaos Mesh selection

Agents create native Chaos Mesh faults with their granted Kubernetes authority.
stacks-k8s supplies no admission profile or fault quota. For actor-specific faults,
prefer selectors with exact network and participant UIDs and `role=actor` on each
intended endpoint. This convention avoids unrelated workloads but does not prevent
an agent from intentionally targeting workers, collectors or nodes. Put the
network UID and correlation ID on the fault object's metadata labels for journal
attribution. Names are display hints. Observe selected Pod UIDs: a label selector
may also match a replacement Pod, and an empty selection proves no effect. See
[upstream selector semantics](https://chaos-mesh.org/docs/define-chaos-experiment-scope/)
and [Chaos Mesh operations](../chaos/operations.md).

StatefulSet process replacement remains part of actor behavior. Removing a
participant withdraws its workload; a fault does not retain membership or
recreate the actor. Native fault status alone is not evidence of a live effect.

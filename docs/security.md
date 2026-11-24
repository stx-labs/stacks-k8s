# Security model

stacks-k8s runs disposable regtest networks for trusted investigators. Each operator
installation is a trusted-administrator installation, not a tenant isolation boundary:
anyone who can author topology, enroll namespaces or read operator credentials is
trusted with the authority those inputs reach. stacks-k8s does not protect an
experiment against cluster-administrator interference.

## Trust boundaries

| Principal or component | Trusted for | Not trusted or permitted for |
| --- | --- | --- |
| Cluster administrator | Installing charts and CRDs, enrolling namespaces, provisioning backend credentials | Nothing is enforced against this role |
| External agent | Choosing and interpreting an investigation with the Kubernetes identity it is given | stacks-k8s ships no agent Role; its authority is whatever the administrator grants |
| Network operator controller | Compiling topology, provisioning workloads and scoped worker identities, publishing admitted identity | Bitcoin RPC, transaction signing, reading Secret contents |
| Resolver Jobs | Generating credentials and rendering configuration into name-scoped Secrets | Anything beyond their named inputs and outputs |
| Bitcoin control worker | Executing Bitcoin control RPC for one execution record, including production and action requests | Topology mutation, other targets' credentials |
| Stacks workers | Bounded Stacks operation using mounted keys for their declared role | Secret API access, workload writes, keys of other roles |
| Action operator | Action admission and lifecycle status | Bitcoin RPC, Secrets, sequencing actions |
| Observability operator, recorders and collectors | Collecting and labeling configured facts | Mutating observed resources, reading Secrets through the API, diagnosis |
| Chaos Mesh | Its upstream injection and recovery contract | Stacks protocol correctness |
| Actors | Their own process behavior and self-reported telemetry | Proving their own identity or correctness |
| Kubernetes API | Admitted object state within its consistency guarantees | Complete application behavior or globally ordered time |

## Identity and confused-deputy prevention

- References are same-namespace and typed.
- Network, participant, workload, Pod, image and policy identity are recorded before
  mutation and pinned by UID.
- Uncached reads precede identity-sensitive effects; when identity cannot be
  established, the operation fails closed.
- Replacement, deletion and recreation, stale generations and ambiguous targets are
  refused rather than silently retargeted.
- Deletion is UID-preconditioned and owner-checked; foreign same-named objects are
  never adopted.
- Actor-reported identity or completion is untrusted until corroborated by the control
  plane or external observation.

Content digests protect immutable requests, configuration, images and evidence. They
do not promise repeatable distributed behavior.

## Admission

1. OpenAPI schemas and CEL enforce static type, enum, bound and immutability rules.
   Schema bounds are absolute; there is no administrator policy that raises them.
2. Kubernetes RBAC limits each identity to intended resources and subresources.
3. Controllers re-check identity and dynamic preconditions before every material
   side effect.

No admission webhook or ValidatingAdmissionPolicy is installed. The network chart
refuses to install over existing network CRDs that serve a different version. The
action chart installs a `ResourceQuota` of 64 objects per action kind in its
namespace. Native Chaos Mesh resources keep upstream admission and the permissions of
the requester; stacks-k8s does not cap them. Admission success is not proof of effect.

## Shipped RBAC

Rendered RBAC for every chart is checked against exact allowlists (`make rbac-verify`).

**Network operator.** A ClusterRole bound cluster-wide. It covers network, Bitcoin and
Stacks resources and their status, plus StatefulSets, Deployments, Jobs, Pods,
Services, ConfigMaps, ServiceAccounts, Roles and RoleBindings it provisions. It also
holds `get`, `list`, `watch`, `create` and `patch` on Secrets. The controller itself
reads Secrets only as uncached metadata and creates empty owned Secrets; contents are
written and read by scoped workloads. The operator must hold every permission it
delegates to worker Roles, because it has no `bind` or `escalate` grant.

**Delegated worker Roles.** The operator creates a ServiceAccount, Role and
RoleBinding per workload, scoped by resource name where Kubernetes allows:

- resolver Jobs get or patch only their named input and output Secrets;
- the Bitcoin control worker reads its target's RPC and configuration Secrets, Pod,
  StatefulSet, Service and wallets by name, and updates only its execution record's
  status;
- Stacks workers read the public resources they need and patch assigned status, with
  no Secret API access.

**Action operator.** A namespaced Role in its installation namespace: `get` on networks,
participants and Bitcoin executions, and management of the enabled action kinds and
their status. No Secret, workload or RPC access.

**Observability operator.** Namespaced Roles in the installation namespace and each
enrolled namespace; no ClusterRole. It manages only recording workloads and their
RBAC, reads observed resources, and has no Secret API access. See the
[observability guide](observability/README.md).

Every operator keeps `create` and `patch` on Events. No controller emits Events
directly; controller-runtime leader election records lease transitions as Events.

RBAC cannot restrict finalizer patches to metadata, and labels or field selectors
route work rather than authorize it. Ownership checks and status-field ownership
between writers are trusted-writer protocols, tested in code.

## Credentials

- Bitcoin Core uses separate `actor`, `control` and, when the observer is enabled,
  observer principals rendered as `rpcauth` entries with per-principal
  `rpcwhitelist` method sets; `rpcwhitelistdefault=1` denies unlisted principals.
  Actor clients receive only the actor credential; block generation, wallet
  management, peer control and invalidation methods are reserved for `control`.
- Generated credential and configuration Secrets are owned by the resource that
  declares them, and resolved Bitcoin Secrets are marked immutable. Resolved keys are
  never regenerated.
- Stacks keys are mounted only into the worker for the declared role, never all
  network accounts.
- Never copy credentials into status, logs or Events. The observability recorder
  redacts recognized credential patterns before export, within documented limits.
- Credentials are static per environment. Rotation is not supported.
- Armed Bitcoin work is pinned to its admitted credential and endpoint and cannot
  retarget after uncertainty.

## Irreversible Bitcoin actions

Bitcoin block production and reorganization are irreversible at the harness level.
Each RPC request is authorized by a durable `Armed` record before it is sent. RPC
timeout, context cancellation, Lease expiry or chain-state absence does not establish
server-side quiescence, so uncertain requests are not retried and leave the target
blocked. There is no reset or readmission: use a fresh isolated environment after such
ambiguity. Deletion stops new authorization; it cannot recall Armed work or undo chain
history. Reorganization records the invalidated block and acknowledges cleanup
separately; cleanup does not undo history. See
[Bitcoin production](network-operator/bitcoin-production.md) and
[Bitcoin actions](action-operator/bitcoin-actions.md).

## Workload hardening

- Actor Pods run as non-root with RuntimeDefault seccomp, no privilege escalation,
  all capabilities dropped, a read-only root filesystem and no ServiceAccount token.
- Operator Deployments run as non-root with the same container restrictions.
- Log-collecting observability collectors run as root with read-only host log mounts,
  dropped capabilities and no privileged mode or host networking. Metrics-only
  collectors run non-root without host mounts.

Topology authors hold delegated workload authority: choosing images, commands,
configuration or Secret references influences code that runs with mounted credentials.
Denying direct workload or Secret writes does not contain that authority, so
topology authors are trusted for the actor authority they can reach.

## Observability integrity

Records carry source identity and explicit capture gaps; missing telemetry is never
treated as healthy. Redaction occurs before export and is best-effort. An observer
compromise grants no workload or action mutation privilege.

## Controls not provided

- No NetworkPolicy is rendered; actor, worker and backend egress is unrestricted.
- No aggregate user ClusterRoles (viewer, editor, action user, observer) are shipped.
- No query proxy: investigation clients connect to the telemetry backend directly with
  backend credentials, bypassing Kubernetes authentication and RBAC.
- No administrator policy to raise or tighten action bounds beyond the schema.
- No credential rotation or recovery of a target after ambiguous execution.
- Controller metrics endpoints are unauthenticated where enabled.

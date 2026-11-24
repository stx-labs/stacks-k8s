# Architecture

stacks-k8s runs disposable, reasonably realistic Stacks regtest networks on Kubernetes.
It supports ordinary development and testing, reliability, liveness, resilience,
performance and efficiency investigations, defensive security testing, and verification
of behavior described in responsibly reported vulnerability reports.

A primary user is a trusted security agent working in a disposable network. The agent
combines topology changes, bounded actions and native infrastructure faults within its
granted permissions; operators expose small controls and passive evidence. Success means
evidence-backed conclusions and, where feasible, a semantically verified reduced
reproducer. An unsuccessful reproduction does not prove an issue is absent, and
verification never requires identical distributed execution.

## Components

Each operator is an independently deployable product with its own Go module, image,
chart and version. Running a `StacksNetwork` requires none of the optional components.

| Component | Responsibility |
| --- | --- |
| [Network operator](network-operator/runtime.md) | Resolves `StacksNetwork` composition, admits genesis and participants, provisions actors and scoped management workers, and maintains declared operation |
| Scoped workers | Run in each network namespace; perform Bitcoin production, Stacks transaction demand, stacking and contract initialization, and publish protocol facts |
| [Action operator](action-operator/README.md), optional | Projects the lifecycle of one bounded Bitcoin generation or reorganization; the network's Bitcoin control worker remains the only RPC sender |
| [Observability operator](observability/README.md), optional | Read-only identity snapshots (`NetworkObservation`) and continuous GreptimeDB recording (`NetworkTelemetry`) |
| [Chaos Mesh](chaos/operations.md), optional | Native Pod, network and other infrastructure faults, created directly by the user or agent |
| User or agent | Chooses experiments, applies changes, collects evidence and draws conclusions |

Shared Kubernetes types live in `apis/network`. Portable protocol code (RPC, Clarity,
transaction signing) lives in `libs/stacks` and `libs/bitcoin`. Neither contains
controller code. Wire fixtures under [contracts/](../contracts/README.md) are verified
through each consumer's own decoding.

## Boundaries

- The user or agent is the only orchestration and reasoning layer. No operator plans
  scenarios, sequences experiments, replays journals, reduces failures or diagnoses root
  causes.
- One action resource represents one bounded action. There is no scenario or playbook
  resource.
- Long-lived desired operation, such as `BitcoinBlockProduction`, is mutable and has no
  terminal completion. Temporary overrides resume the latest baseline; they never restore
  an old spec.
- The observability operator never mutates the environment it observes.
- Distributed execution is not deterministic. Content digests identify configuration,
  images and evidence; they do not promise identical runtime behavior. Repeatability
  claims name their scope, prerequisites and limits.
- Source checkout and image builds happen outside the operators, which consume OCI
  image references. See [custom actor images](network-operator/actor-images.md).
- Generic infrastructure faults use native Chaos Mesh resources.
- Public APIs say Bitcoin rather than burnchain, except when quoting a Stacks
  configuration or protocol field.
- Controllers are idempotent and level-based, with one logical writer per field and
  external side effect.
- Go owns configuration rendering, RPC, signing and execution. The pinned Stacks.js
  package is an offline test oracle only.

## Steady-state operation

`StacksNetwork` declares a functioning network: topology plus ongoing behavior. Reusable
definitions are inert until selected as participants. The aggregate resolves and admits;
capability controllers and workers converge initialization and then maintain operation.
No external bootstrap command is needed.

- **Genesis.** Account resolution is independent of networks. Genesis freezes balances,
  epoch and PoX inputs, public identities, contract sources and initial requirements
  before any actor starts. Changing genesis requires a new network. Funding an account
  does not enroll it in any capability.
- **Bitcoin production.** Bitcoin nodes have no mining role. `BitcoinBlockProduction`
  selects timing and weighted targets; per-target `BitcoinExecution` reservations
  serialize baseline production and bounded actions. A client timeout never clears an
  ambiguous dispatch.
- **Stacks.** Stacks nodes keep their mining role. Independent workers supply
  transaction demand and enroll and renew stackers; consensus signers run separately. A
  transfer interval influences demand; it does not promise block cadence.
- **Interventions.** Bounded actions and native faults are separate from baseline
  policy. Reorganization cleanup removes its local invalidity marker, not the resulting
  best chain.
- **Readiness.** Kubernetes readiness, completed initialization (`Initialized`), recent
  protocol progress (`Operational`) and action completion are distinct facts. Optional
  actors, faucets and telemetry never become prerequisites for unrelated capabilities.
- **Faults and control.** Losing RPC control does not prove an actor stopped mining, and
  fault cleanup does not prove consensus recovered. Client deadlines and Lease expiry do
  not establish server-side quiescence.

Stacks management workers keep process-local nonce and submission state. Losing a bound
worker ends managed operation for that network; no replacement guesses a nonce or
resubmits uncertain work. Shared accounts are legal experiment inputs without nonce
coordination between workers.

## Capabilities and limits

| Area | Current behavior | Limits |
| --- | --- | --- |
| Composition | Heterogeneous participants from reusable definitions or inline entries, with typed overrides | Participant names are single-use within a root |
| Accounts and wallets | Scoped generation or import; public identity resolution | The operator never reads private key material; shared accounts are allowed |
| Actors | Bitcoin nodes, Stacks miners and followers, consensus signers as StatefulSets with Services | A Ready Pod does not establish protocol health |
| Bitcoin production | Fixed or uniform timing, weighted targets, initialization ceilings, per-target execution records, temporary schedule overrides | An ambiguous dispatch closes that target; recovery needs a fresh network |
| Stacks operation | STX-transfer demand, direct PoX-4/PoX-5 enrollment and renewal, pinned sBTC contract deployment with test registry initialization | Single-account transfer profile only; no real sBTC signers or delegated pools; workers do not recover after process loss |
| Faucet | Bounded request resources with native submission and inclusion | No cross-worker nonce coordination |
| Lifecycle | Cooperative pause, terminal stop, per-actor image rolls, participant and root removal, configured PVC retention | `Stopped` is terminal; no recovery after kind node loss during initialization |
| Actions | Finite Bitcoin generation (immediate, fixed, uniform, explicit delays) and bounded local reorganization | No protocol-level actions (signer, miner or input injection) |
| Observation | Identity snapshots; continuous recording of objects, logs, native metrics, resource usage and capture gaps; offline evidence bundles | No in-cluster export resource or query proxy; no tracing |

Live checks run only on a local three-node kind cluster with specific actor images.
They do not establish compatibility with arbitrary images, every cadence and resource
combination, or recovery from every fault. Unit and envtest results do not prove kubelet,
storage, garbage-collection or protocol behavior.

## Agent interaction

An agent, or any client, may independently:

1. create or patch a `StacksNetwork` and wait for its observed generation;
2. change Bitcoin production and transaction demand through `StacksNetwork`;
3. inspect admitted participants and current protocol facts;
4. create one native Chaos Mesh fault or one bounded action resource;
5. watch that resource and surrounding telemetry;
6. create, overlap, update or delete further resources within its permissions; and
7. export evidence for a time window and use it to attempt reproduction and reduction.

These are client choices, not an in-cluster workflow. Discovery uses structural CRD
schemas, `kubectl explain`, printer columns, stable condition reasons and the
[API reference](api/README.md).

Clients watch generation-aware status and conditions instead of sleeping. Actions use the
`Pending`, `Admitted`, `Active`, `Recovering`, `Completed`, `Recovered`, `Failed` and
`Inconclusive` phases; `Inconclusive` is a first-class result, never success or failure.
The `actions.stacks.org/correlation-id` label is a search hint; the object UID is the
authoritative identity. Protocol correctness is a conclusion the agent draws from trusted
and self-reported evidence.

The [agent skills](../.agents/skills/stacks-k8s/SKILL.md) package these workflows for
coding agents.

## Dependency boundaries

Operators consume versioned APIs and portable libraries through independent module
requirements and local `replace` directives. The observability operator does not import
the network operator's runtime. Generated schemas and deepcopies come from isolated
tooling modules, and no committed root `go.work` unifies the dependency graphs. See
[development](development.md) and [security](security.md).

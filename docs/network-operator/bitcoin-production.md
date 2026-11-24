# Bitcoin production

Select a `BitcoinBlockProduction` participant in `StacksNetwork.spec.participants`.
Its reusable definition references a `BitcoinBlockSchedule`, selected Bitcoin-node
participants and a payout wallet. The [example](../api/examples/30-actors.yaml)
shows the complete wiring and initial miner-wallet funding requirements.

## Cadence and targets

Omitting both `schedule` and `scheduleRef` uses a fixed 60-second Bitcoin cadence.
Explicit schedules take precedence. `Fixed` uses one interval; `Uniform` samples between
a minimum and maximum; every interval is between 1 second and 1 hour.

Initialization time scales with the last gate height and the cadence. The 30-actor
example's first PoX-5 waterfall is at Bitcoin height 300: at a fixed 5-second cadence it
initializes in roughly 25-30 minutes; at the 60-second default, expect about five hours.
Full initialization is qualified at a 5-second fixed cadence. Faster cadences are not: a
1-second schedule completes `PrepareBitcoin`, but full initialization at 1 second can fail
waiting for the prepared signer set (`PoXAnchorBlockRequired`). Use 5 seconds or slower.

Timing and target selection are separate. Fixed or uniform cadence creates one
opportunity at a time; up to 100 weighted targets determine where it is offered.
Weights distribute generation opportunities, not hash power or canonical progress.
Capacity, reservations, expiration and unavailable targets may skip opportunities. There
is no catch-up burst, redraw or replay of skipped slots. Removing a target stops new
baseline offers to it; its execution record and unresolved state remain.

The scoped Bitcoin control workload holds mutation credentials and writes durable
per-target `BitcoinExecution` records. It independently enforces the aggregate's
initialization ceilings. Actor RPC credentials are method restricted. Retained receipts
are accounted independently of later opportunity or producer selection; new sends wait
for that accounting.

A `BitcoinBlockScheduleOverride` temporarily replaces timing for an exact network
and production instance. Expiry or cancellation resumes the latest admitted baseline,
not a saved old policy. Finite actions use the same target reservation and executor.
See the [resource reference](../api/resources.md) and
[Bitcoin actions](../action-operator/bitcoin-actions.md).

Production observes frozen initialization ceilings and native gate evidence.
Crossing a height is not permission to skip enrollment or contract confirmation.
After initialization, it maintains the declared ongoing schedule.

Use participant/root cooperative controls to pause new production. Receipt
collection and native observations continue. Follow [network cleanup](operations.md#removal-and-cleanup)
before deleting a namespace; genesis and execution records stay protected until
participant consumers finish disposal.

## Bootstrap admission timing

Before initialization completes, an unconsumed generation offer remains valid for
the greater of the sampled cadence interval and 30 seconds. Cadence controls when
a new offer may be selected; it is not the bootstrap worker admission deadline.
A missed tick does not replace an unexpired offer or accumulate catch-up work.
Fresh converged height/tip changes or current production identity/policy changes
replace unsent stale offers subject to cadence, without waiting for their expiry.
Workers still check current policy, pause, exact target and chain identity, and
the frozen gate ceiling before sending. Expiry does not cancel an Armed RPC.
Completed-initialization baseline opportunities retain their next-tick expiry.

Each first-gate opportunity waits for a fresh post-receipt observation, so funding and
maturing miners takes roughly one observation round trip per block. Set
`initialization.batchGeneration: true` to fund each miner and mature the outputs in as few
requests as possible: one per miner and one for the remaining height, each up to 500 blocks.
A `minimumHeight` of 103 with one miner needing two outputs takes two requests (2 and 101
blocks). Only the first gate batches. Enrollment, contract and waterfall gates still advance
one block per opportunity.

```yaml
initialization:
  targetNodeRef: {name: btc-01}
  minimumHeight: 103
  minerWalletRefs:
    - {name: miner-wallet-01}
  matureOutputsPerMiner: 2
  batchGeneration: true
```

## Execution records

Each Bitcoin node has one network-owned `BitcoinExecution`; one `BitcoinInitialization`
holds scheduler state. The aggregate pins both UIDs before workers activate. A missing or
recreated record fails closed and is never treated as a fresh target. Users do not create
or edit these records.

| Status | Meaning |
| --- | --- |
| `phase: Idle`, no `armed` | No outstanding request. The current `reservation` owner may arm its next admitted request. |
| `armed`, `phase: Armed` | Written by compare-and-swap before sending and bound to one worker `processNonce`. Only the process whose write succeeded may send, once. No restarted or replacement process resends or supersedes it. |
| `lastReceipt` | A successful response, accounted once (with `blocksGenerated` and `completedOffer`); `armed` clears and `reservation` is retained. |
| `drain` | Terminal-control acknowledgement. `Uncertain` with `phase: Blocked` means shutdown ended with a request outstanding. |

- An Armed request without a known receipt closes its target. If the arm write's
  response is lost, the record stays Armed even though nothing was sent; the live process
  may withdraw only an arm it knows it never sent.
- Client timeouts do not terminate Core execution. Leader-election Leases and uncached
  reads do not fence the server. Absence from observed tips does not prove an outstanding
  request cannot execute later. A fixed grace interval does not establish quiescence.
- The RPC client never retries, follows redirects or reuses connections, and responses
  are limited to 64 KiB. An Armed request pins its target process and credential
  identity. A server error proves the handler returned, not that it had no effect.
- On SIGTERM the worker stops new arms and has up to 25 seconds (within the Pod's
  45-second grace) to persist receipts. Crashes, lost responses, API-write failures and
  exhausted grace can still strand a target; a rollout is not guaranteed to drain.
- Action deadlines prevent new arms but cannot recall an Armed request.

There is no in-place reset of a closed target. Preserve evidence and continue in a fresh
isolated environment: a new namespace and root with new UIDs, freshly generated RPC
credentials and no reused data or configuration. A new namespace does not fence a request
the old Core process already accepted. Stopping or deleting the network abandons its
records (`BitcoinInitialization` phase `Abandoned`) without waiting for RPC quiescence or
an evidence sink; abandonment never claims successful cleanup or authorizes resuming the
old target. Export evidence first when it matters.

# Bitcoin actions

`BitcoinBlockGeneration` and `BitcoinReorganization` are finite requests against one
`BitcoinNode` participant. The network's Bitcoin control worker for that node
admits and executes them; the [action operator](README.md) projects their lifecycle.
Install and enable both sides as described there. Examples are in
[`examples/actions`](../../examples/actions/).

## Shared execution

Actions use the worker's existing `BitcoinExecution` record, the same reservation,
single Armed slot and receipt accounting that baseline production uses. A lost Armed
acknowledgement or unknown delivery is never replayed. Client deadlines, timeouts,
Lease expiry and Pod deletion are not treated as proof that Bitcoin Core stopped
executing a request.

Until initialization completes, admission and every invalidation or generation send
also obey the network's current initialization gate: an action cannot take the chain
past the gate's Bitcoin ceiling.

### Priority over baseline

- Before serving a baseline offer, the worker selects any eligible action. The
  oldest eligible request (creation time, then UID) wins.
- An admitted action excludes baseline production on that node from admission until
  release, including its cadence delays and retained uncertainty or cleanup.
- Continuously submitted eligible actions can therefore starve baseline production
  on that node. Ineligible `Pending` requests are skipped and do not stall it.
- There is no fair-share arbitration. Baseline opportunities on a busy target follow
  the [production](../network-operator/bitcoin-production.md) rules.

## BitcoinBlockGeneration

Requests `count` acknowledged single-block `generatetoaddress` calls to `address`.

| Field | Bounds |
| --- | --- |
| `count` | 1–100. |
| `cadence.mode` | `Immediate`, `Fixed`, `Uniform` or `Explicit`. |
| `cadence.intervalSeconds` | `Fixed` only; 1–60. |
| `cadence.minSeconds`, `cadence.maxSeconds` | `Uniform` only; inclusive, ordered, 1–60. |
| `cadence.delaysSeconds` | `Explicit` only; exactly `count - 1` entries of 0–60. |
| `address` | Regtest payout address, 14–128 characters; checked against the node before admission and each send. |

The first block is eligible immediately. Each later delay starts at the previous
receipt and is rounded up to a whole second. `Uniform` samples one delay per receipt.
Delays are minimums: late dispatch is not caught up and has no upper latency bound.

`Completed` requires all `count` receipts, the last received no later than
`expiresAt` and before any deletion. `status.blocksGenerated`, `lastBlockHash` and
`lastDispatchID` report acknowledged RPC receipts, not canonical chain membership.

## BitcoinReorganization

Replaces a local suffix of `depth` blocks on one node with `depth + 1` blocks mined to
`address`.

| Field | Bounds |
| --- | --- |
| `depth` | 1–6. |
| `address` | Regtest payout address, 14–128 characters. |
| `boundaryPolicy` | `allowEpochBoundaryCrossing`, `allowRewardCycleBoundaryCrossing` and `allowPreparePhaseBoundaryCrossing` must all be `true`. |

The worker has no trusted Stacks schedule, so all three boundary opt-ins are required;
selective boundary admission is not supported.

### Admission capture

Before reserving, the worker reads at most 64 chain tips and refuses any existing
`invalid` tip or more than one active tip. It records the original tip, walks
`depth` headers to the retained ancestor (each with increasing chainwork), records
the first removed hash, and confirms the tip did not move during capture. The spec
accepts no arbitrary hash, endpoint, RPC, wallet or command.

### Mechanism

1. Confirm the tip still equals the captured original, then arm and account
   `invalidateblock` for the first removed hash.
2. Generate replacements one at a time; after the first, each waits at least one
   second after the previous receipt. Before each send, the tip must equal the last
   verified block. Each receipt must be the next height, extend the previous block
   and add chainwork.
3. Arm and account `reconsiderblock` for the same hash.
4. Verify every replacement is canonical at its height and the final tip's chainwork
   exceeds the original.

An unexpected tip before a send stops the action with `ExternalChainMovement`, and
a replacement receipt that fails verification with `ReplacementVerificationFailed`.
A failed final check (`ReplacementWorkInsufficient`, `ReplacementNotCanonical`)
marks cleanup unsafe and reports `Inconclusive`/`EffectUncertain`.

Status adds `originalChain`, `forkParent`, `invalidatedHash`,
`replacementBlockHashes` (at most seven), `finalChain`, `invalidationAcknowledged`
and `cleanupAcknowledged`. `Completed` requires all mutation receipts, acknowledged
cleanup and the final local check. It does not assert peer or Stacks convergence;
other nodes follow only through normal Bitcoin peering. Cleanup removes this
action's invalidity marker; it does not restore the original chain. Generated
history is irreversible.

`getchaintips`, `getblockheader`, `getblockhash`, `invalidateblock` and
`reconsiderblock` are in the node's control RPC allowlist; the network operator's
`bitcoinActions.reorganizationEnabled` value controls whether the worker uses them.
Bitcoin Core 31.1 defines
[invalidation and reconsideration](https://github.com/bitcoin/bitcoin/blob/v31.1/src/rpc/blockchain.cpp).

### Cleanup

The action deadline bounds new invalidation and generation. Compensation has a
separate fixed horizon of `expiresAt + 30s`; see the
[clock assumption](README.md#time-and-clocks).

| Situation | Behavior |
| --- | --- |
| Stopped before invalidation is acknowledged | No cleanup needed. `Failed` with the stop reason. |
| Stopped after acknowledged invalidation (deletion, deadline, network pause, verification failure) | No further generation. Only `reconsiderblock` for the captured hash is armed, on the same admitted runtime. `Recovering`, then `Failed` with the stop reason once cleanup is acknowledged. |
| Armed RPC without a receipt | `Inconclusive`/`EffectUncertain`. Exclusion and the finalizer are retained; no further mutation, including cleanup. |
| Target identity changed after invalidation | Cleanup is marked unsafe; no compensation on the replacement process. `Inconclusive`. |
| Cleanup not acknowledged by `expiresAt + 30s` | No new compensation is armed. `Inconclusive`. An already armed `reconsiderblock` keeps `Recovering` until its receipt or the horizon. |
| Receipt arrives after a terminal outcome | Copied into status; the outcome is not revised. |
| Worker restart between accounted calls | Continues from the execution record; never repeats an acknowledged step. |
| Network deleted or replaced | `EnvironmentDisposed`; the finalizer is released without claiming cleanup or server quiescence. |

Unresolved cleanup keeps the node excluded. Recover by creating a fresh environment.

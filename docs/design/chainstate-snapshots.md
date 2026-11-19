# Chainstate snapshots

Status: implemented initial slice of the roadmap's
[reusable chainstate snapshots](roadmap.md#reusable-chainstate-snapshots); names
and schema remain subject to API review. Qualified on three-node kind only.

## Purpose

A network that initializes from genesis spends most of its boot generating
Bitcoin blocks and observing initialization gates (about 10 minutes for the
minimal cohort on kind). A restored network starts its actors on chain data
captured from an earlier `Initialized` network with the same genesis and initial
cohort, and reaches `Initialized` in about a minute.

Restore is opt-in. Without `spec.genesis.restore` a network bootstraps exactly as
before; restored and bootstrapped networks run side by side under one operator.

## Model

| Piece | Role |
| --- | --- |
| `StacksChainstateSnapshot` | Immutable, namespaced. Public facts only: snapshot image, `genesisDigest`, the source initial cohort's projection (below), per-actor archive path, sha256 and writing image, Bitcoin and Stacks tips. No status. |
| Snapshot image | OCI image with one gzip tar per actor under `/snapshot`, on a BusyBox-compatible base. |
| `StacksNetwork.spec.genesis.restore` | `snapshotRef` and `stacksImagePolicy` (`Exact` default, or `AllowDifferent`). Creation-only, like the rest of genesis input. |
| Frozen genesis | `spec.source.restored` records the image, Bitcoin tip and accepted image changes; each restored actor's `BootstrapRequirement.restoredArchive` records its archive. `Dependencies` pins the snapshot. |

The network spec still declares every genesis input; the snapshot supplies none of
them. The operator compiles the network as usual and then verifies it against the
snapshot. After freeze, nothing reads the snapshot resource again: deleting it does
not affect a running restored network. The image must stay pullable, since every
restored actor start runs it.

A [capture tool](../../tools/stacks-snapshot/README.md) produces the archives,
the manifest and a Dockerfile from a source network; the
[operations guide](../network-operator/chainstate-snapshots.md) covers the workflow. It uses the caller's
credentials; the operator gains no permissions.

## Restore sequence

| Step | Bootstrap from genesis | Restore |
| --- | --- | --- |
| Freeze | Compile, freeze | Compile, then validate against the snapshot; any mismatch fails resolution with `SnapshotMismatch` before any workload exists. |
| Actor volumes | Empty | `restore-data` init container from the snapshot image: verify sha256, extract into empty `/data`, drop peer and mempool files, sync, then write and sync a marker holding the checksum. A matching marker skips the step on restart. |
| PrepareBitcoin | Generate to the first gate | Prove the restored chain (snapshot height and tip hash, ready wallets, one mature output per miner), then mine exactly one refresh block and record `preparedAt`. |
| Gates | Observed one by one under ceilings | Completed at `preparedAt` with `initialization.restoredFrom`; the final ceiling is retained as gate completion leaves it. |
| Stacking | Initial cohort targets the bootstrap cycle | Restored stackers run as a later cohort: keep or renew from the current cycle. The PoX-5 captured-administrator check still applies to them. |
| Actors, workers, `Initialized` | Unchanged | Unchanged |

**Refresh block.** stacks-node refuses a burnchain tip older than two hours, and
a snapshot usually is. The refresh block is always mined, so restore does not
depend on the snapshot's age. The scheduler recognizes it by its accounted
receipt, not by height, so a retried preparation never mines twice; two
unanswered offers report `RestoreRefreshUnacknowledged`.

**Fail-closed reads.** Restored preparation reads the restore facts from the
exact frozen genesis binding. If the genesis cannot be established it blocks,
rather than falling back to the bootstrap path on a restored chain.

## What must match

The snapshot fixes everything its chain data encodes.

| Input | Rule |
| --- | --- |
| `spec.chain` (profile, epochs, PoX, allocations, contract bindings) | Equal `genesisDigest`. |
| Initial cohort | Same participant names and kinds; no member missing, none extra. |
| Accounts | Same public identities per role (`Identity`, `Signer`, `Holder`, `Administrator`, `Deployer`, `RegistrySigner`, `AggregateKey`). |
| Links and roles | Same node's Bitcoin node, signer's node, stacker's signer; mining role; stake and lock cycles; contract set registry threshold and signer order (the on-chain signer set and its principal depend on it). |
| Bitcoin node images | Equal to the image that wrote the data. |
| Stacks node and signer images | Equal under `Exact`; under `AllowDifferent`, recorded per actor in `restored.imageChanges`. Schema migrations only go forward, so use the same or a newer release. |
| Snapshot height | At least the final initialization ceiling: only `Initialized` networks can be captured. |

Everything else is rendered fresh from the restoring spec and may differ:
resources, storage, `configOverrides`, Bitcoin production schedule, transaction
and contract workers, worker accounts, namespace, names and UIDs. The projection
deliberately excludes per-network identities and whole-policy digests for this
reason. The miner payout wallet is not compared either: it only receives new
coinbase, so a restored network may pay a different address.

Settings that shape what a node stores (an event observer, an index) take effect
from the restore point only; earlier history is not backfilled.

## Adding participants

A snapshot fixes the initial cohort; extra participants join through the existing
late-participant path once the network is `Initialized`. They start with empty
volumes.

| Late participant | Outcome |
| --- | --- |
| Node that neither mines nor signs | Syncs from the restored nodes (seconds to a minute). |
| Signer and stacker | Signs from the next reward cycle and changes signer weights and threshold. |
| Miner | Needs Bitcoin funding, 100-block coinbase maturity and VRF registration. |

In practice one snapshot serves one consensus shape (miners and signers); plain
nodes can be added to any of them.

## Keys and artifact contents

- The snapshot resource and image carry public identities, chain data and
  checksums only. Private keys stay in the restoring network's own Secrets.
- Capture refuses a network whose cohort or genesis-funded accounts use generated
  keys: a restore must reproduce their public identities, which the genesis digest
  covers, so their keys must be importable.
- Bitcoin Core wallets in the data are watch-only descriptors. Peer files (the
  Stacks peer database, which holds the node's p2p key and source peers, and Bitcoin
  Core's `peers.dat` and `anchors.dat`) and mempools (`mempool.sqlite`,
  `mempool.dat`) are dropped at capture and again at restore.
- An archive captured from a restored network never carries a valid restore
  marker: a marker holds its own archive's checksum, which no archive can contain.
- Files are dropped by name. The list was checked against stacks-core 4.0.5 (a key
  scan of the archives found none); recheck it when a supported release adds data
  files.

## Capture

Capture requires an `Initialized` network and stops it, since `Stopped` is
terminal: pause, wait until every Bitcoin and Stacks tip is still, stop (recording
the tips in an annotation for a resumed export), then export each actor volume
through a read-only, unprivileged Pod. Only the initial cohort is captured. The
export rejects links, devices, paths outside the data root and streams that end
before tar's end-of-archive marker. See the
[tool README](../../tools/stacks-snapshot/README.md) for the request, steps and RBAC.

## Failure reasons

| Reason | Where | Meaning |
| --- | --- | --- |
| `SnapshotMismatch` | Network resolution | The network differs from the snapshot; the message lists every difference. |
| `RestoredChainDiffers` | `BitcoinInitialization` | The restored Bitcoin node does not report the snapshot's height and tip. |
| `RestoredWalletsImmature` | `BitcoinInitialization` | A miner has no mature output to commit with. |
| `RestoreRefreshUnacknowledged` | `BitcoinInitialization` | The refresh block was offered twice without a receipt. |
| `RestoreFailed` | Participant | The restore step failed or its image cannot be pulled. |

## Qualification

On kind, with the minimal cohort (one Bitcoin node, mining node and signer):

- One snapshot restored into two namespaces, one with `Exact` images (4.0.3) and
  one with `AllowDifferent` (4.0.5 on 4.0.3 data). Each was `Initialized` in under
  a minute and produced blocks; a from-scratch network ran alongside.
- A late follower node synced to the tip of a restored network in about two minutes.
- Capture took about 100 seconds; compressed archives total about 8 MB.
- A benchmark network (400 genesis-funded senders) reached ready in 2.7 minutes
  restored versus 10.8 minutes from genesis, and sustained the same load.

## Limits

- Restore always creates a new network; it never rewinds one in place.
- Removing, replacing or re-linking an initial participant needs a new snapshot.
- Capture refuses a source whose signer set may depend on participants outside the
  initial cohort: signers or stackers added after initialization, or participants
  removed since, whose locked stake can still count.
- Capture refuses a source whose stackers changed `amountMicroSTX` or `lockCycles`
  after initialization: the snapshot records the stake frozen in the genesis, which
  neither the source's current policy nor its locked stake may match. The check
  compares the admitted policy, not the on-chain lock, so a stake changed and then
  reverted passes while the changed amount stays locked.
- Actor volumes must be persistent: capture refuses ephemeral storage before it
  stops anything.
- Kind with locally loaded images is the only qualified delivery; registries and
  managed clusters are untested.
- Determinism is not a goal: restored networks share a starting chain, not
  subsequent results.

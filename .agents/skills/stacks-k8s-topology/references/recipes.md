# Topology recipes

## Examples

Complete manifests live in the stacks-k8s repository under
`docs/api/examples/`. Envtest strictly admits `minimal.yaml`,
`separate-roles.yaml` and `30-actors.yaml`; the first two also resolve while paused.

| Example | Participants | Shape | Timing |
| --- | --- | --- | --- |
| `minimal.yaml` | 7 | One Bitcoin node; one mining Stacks node that also hosts the only signer | Epochs from height 103, 5s blocks, batched first gate |
| `separate-roles.yaml` | 15 | Miner on its own Stacks and Bitcoin nodes; two dedicated signer nodes with one stacker each; one follower; faucet | Same as minimal |
| `30-actors.yaml` | 30 actors | Four miners, six signer nodes, followers, ten Bitcoin nodes, faucet | Epochs from height 203, 60s blocks |
| `late-participant.yaml` | — | Faucet requests and actors added after initialization (design example) | — |
| `optional.yaml` | — | Bitcoin actions, telemetry, schedule override | — |

Use `separate-roles.yaml` when miner and signer roles must run in separate stacks-node
processes, for example to verify a reported vulnerability. Its participant names match
the `minimal14` variant of the live harness in
`operators/network/internal/publicintegration`, which was qualified with 30-actor
epoch heights and a 5s cadence. With its own faster epochs and the stock 4.0.5 images,
one live run reached `Operational=True` about 10 minutes after `Running` (2026-11-23).

Initialization time scales with the last gate height and the Bitcoin block interval.
Moving every epoch 100 blocks earlier and using `batchGeneration` is what makes the
minimal timing faster than 30-actors.

Root `genesis.pox` defaults to 20-block reward cycles with a 5-block prepare phase: 25s
at a 5s cadence. A fault covering a whole prepare phase can halt the chain on a missing
PoX anchor, so lengthen both, or slow the cadence, when experiments need longer fault
windows. Both are frozen at creation and lengthen initialization.

Nakamoto signing requires 70% of signer weight. With two equal signers, losing either
one stalls block signing; use three or more stackers if the experiment must tolerate a
signer loss.

## Dedicated signer node

A signer attaches to exactly one Stacks node, and each node serves at most one signer.
A non-mining node needs only its Bitcoin binding; its identity account is generated.

```yaml
kind: StacksNode
metadata: {name: signer-node-01}
spec:
  bitcoinNodeRef: {name: btc-02}        # participant
  peers: {discovery: Network}
---
kind: StacksSigner
metadata: {name: signer-01}
spec:
  nodeRef: {name: signer-node-01}       # participant
  accountRef: {name: consensus-01}      # StacksAccount, signing-capable
---
kind: StacksStacker
metadata: {name: stacker-01}
spec:
  holderAccountRef: {name: holder-01}          # funded in genesis
  administratorAccountRef: {name: admin-01}    # funded in genesis
  signerRef: {name: signer-01}                 # participant
  targetNodeRef: {name: signer-node-01}        # participant used for submissions
  amountMicroSTX: "1000000000000000"
  lockCycles: 6
  renewWhenRemainingCycles: 3
```

Add `holder-01`, `admin-01` and `consensus-01` accounts, list all three new
participants in the root, and allocate the holder at least the stake plus fees (the
examples use twice the stake) and the administrator a small balance.

To host the signer on the miner instead, point `nodeRef` and `targetNodeRef` at the
mining node, as `minimal.yaml` does.

## Miner

```yaml
kind: BitcoinWallet
metadata: {name: miner-wallet-01}
spec:
  keySource:
    stacksMinerAccountRef: {name: miner-key-01}   # wallet derives from the miner key
---
kind: BitcoinNode
metadata: {name: btc-07}
spec:
  walletRefs: [{name: miner-wallet-01}]           # node that loads the miner wallet
---
kind: StacksNode
metadata: {name: miner-01}
spec:
  bitcoinNodeRef: {name: btc-07}                  # must load the wallet below
  identityAccountRef: {name: miner-key-01}        # required for miners
  mining:
    enabled: true
    bitcoinWalletRef: {name: miner-wallet-01}
    feeRateSatsPerVByte: 2
```

For an initial miner, also add the wallet to `BitcoinBlockProduction`
`initialization.minerWalletRefs` and allocate `miner-key-01` STX in genesis. A miner
added after initialization is not funded by initialization: generate mature Bitcoin to
its wallet address with a bounded Bitcoin action before enabling mining.

## Follower or observer

```yaml
kind: StacksNode
metadata: {name: follower-01}
spec:
  bitcoinNodeRef: {name: btc-02}
  peers: {discovery: Network}
```

`peers.discovery: Network` seeds from the other selected participants of the same kind.
Use `peers.nodeRefs` to control seeds explicitly, for example to build a chain or
partition-prone topology.

## Separate failure domains

- Separate processes: give the miner and each signer their own `StacksNode`.
- Separate Bitcoin backends: bind the miner and the signer nodes to different
  `BitcoinNode` participants, and list both as production `targets`.
- Separate Kubernetes nodes: set `placement.nodeSelector` per definition, or
  `placement.spreadAcrossNodes: true` for preferred same-kind anti-affinity.
  `defaults.workerPlacement` places support Pods only.

## Reuse and inline definitions

One definition can back several participants; each gets its own Pod, storage and status.
Give each a distinct identity through an override:

```yaml
participants:
  - name: follower-a
    kind: StacksNode
    definition: {ref: {name: shared-follower}}
  - name: follower-b
    kind: StacksNode
    definition: {ref: {name: shared-follower}}
    overrides:
      stacksNode:
        identityAccountRef: {name: follower-b-key}
```

An entry may instead carry `definition.inline.<branch>` with the same fields as the
kind's `spec`. The branch name is the kind in lower camel case (`stacksNode`,
`bitcoinBlockProduction`). Inline instances own their generated default accounts, which
are deleted with the instance.

## Mixed versions

Set root `defaults.images` for the common build, then override one participant:

```yaml
- name: miner-01
  kind: StacksNode
  definition: {ref: {name: miner-01}}
  overrides:
    stacksNode:
      image: stacks-core:patched-01
```

Load every tag into kind before applying. Image edits roll only that actor; readiness
does not prove a successful protocol upgrade.

## Custom actor configuration

`config.overrides` merges native settings after the managed ones: a TOML tree for Stacks
actors, or option names and one section level for Bitcoin. Protected paths (genesis,
epochs, PoX, identity, credentials, managed endpoints, signer event subscriptions) are
rejected. `config.secretRef` instead selects a complete document; it excludes
`overrides`. `config.compatibility: Unverified` allows deliberate divergence, but that
actor no longer satisfies initial protocol prerequisites.

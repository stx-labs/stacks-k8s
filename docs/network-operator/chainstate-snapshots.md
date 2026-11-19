# Chainstate snapshots

Capture an `Initialized` network once, then start new networks from its chain data
instead of bootstrapping from genesis. On kind, the minimal cohort reaches `Initialized`
in about a minute instead of about ten. The [design](../design/chainstate-snapshots.md)
explains the rules; this guide covers the workflow.

A snapshot serves networks with the same genesis inputs, initial cohort (participants,
public keys, roles, links, stake) and Bitcoin image. Resources, placement, overrides,
production cadence and workers may differ. Each consensus shape needs its own snapshot.

## Select the cluster

The commands below use the same variables as [custom actor images](actor-images.md):

```bash
export STACKS_KIND_CLUSTER="${STACKS_KIND_CLUSTER:-stacks-k8s}"
export STACKS_KUBECONFIG="${STACKS_KUBECONFIG:-$PWD/tools/local-cluster/kubeconfig}"
export STACKS_CONTEXT="${STACKS_CONTEXT:-kind-$STACKS_KIND_CLUSTER}"
k() { kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" "$@"; }
```

## 1. Run a source network with imported keys

Capture refuses accounts with generated keys, including accounts funded only by
`spec.genesis.allocations`: a restoring network must reproduce the same public
identities, so the keys must be importable. Create one Secret entry per
account (a hex secp256k1 private key with the `01` compressed suffix), and point each
account at it instead of `generate: true`:

```bash
NS=lab-source
k create namespace "$NS"
args=()
for a in holder-01 admin-01 consensus-01 miner-key-01 sbtc-deployer traffic-account \
         bridge-key-01 bridge-key-02 bridge-aggregate; do
  args+=(--from-literal="$a=$(openssl rand -hex 32)01")
done
k -n "$NS" create secret generic source-keys "${args[@]}"
```

```yaml
spec:
  key:
    secretRef: {name: source-keys, key: holder-01}
```

Keep these keys with the snapshot: every restoring namespace needs the same Secret.
They are lab keys; never put them in a snapshot, an image or a repository.

Apply the network (for example [minimal.yaml](../design/public-api/examples/minimal.yaml)
with the accounts changed as above), request `Running` and wait for `Initialized`.
Keep actor storage persistent, and keep every signer and stacker in the initial
manifest: capture refuses ephemeral actor storage, and signers, stackers or removed
participants outside the initial cohort.

## 2. Capture

Capture **stops the source network**; `Stopped` is terminal. Write the request and run
the [stacks-snapshot](../../tools/stacks-snapshot/README.md) tool:

```bash
cat > capture.json <<EOF
{
  "kubeconfig": "$STACKS_KUBECONFIG",
  "context": "$STACKS_CONTEXT",
  "namespace": "$NS",
  "network": "network",
  "networkUID": "$(k -n "$NS" get stacksnetwork network -o jsonpath='{.metadata.uid}')",
  "snapshot": "minimal-seed",
  "image": "localhost/stacks-snapshots/minimal-seed:v1",
  "output": "$PWD/minimal-seed",
  "timeoutSeconds": 1800
}
EOF
GOWORK=off go -C tools/stacks-snapshot run . capture < capture.json > capture-report.json
```

The output directory receives one archive per Bitcoin node, Stacks node and signer,
`snapshot.yaml` and a `Dockerfile`. If the export fails after the stop, rerun with
`"resume": true` and an empty output directory. Capture takes about 100 seconds for the
minimal cohort; the archives total about 8 MB. The tool README lists the request fields
and the RBAC for a non-admin caller.

## 3. Build and load the image

```bash
docker build -t localhost/stacks-snapshots/minimal-seed:v1 "$PWD/minimal-seed"
kind load docker-image localhost/stacks-snapshots/minimal-seed:v1 --name "$STACKS_KIND_CLUSTER"
```

Load it again after recreating the cluster. To share a snapshot, push the image to a
registry and hand out `snapshot.yaml` together with the keys. Restore verifies each
archive's sha256, so a tag is enough.

## 4. Restore

In each restoring namespace, apply the snapshot resource and the same keys Secret, then
a network that declares the source's genesis inputs and cohort plus `genesis.restore`.
[restore.yaml](../design/public-api/examples/restore.yaml) is the restoring counterpart of
`minimal.yaml`:

```bash
NS=lab-restore
k create namespace "$NS"
k -n "$NS" apply -f minimal-seed/snapshot.yaml
k -n "$NS" create secret generic source-keys --from-literal=... # the source's keys
k apply -f restore.yaml                                         # namespace lab-restore
k -n "$NS" patch stacksnetwork network --type=merge -p '{"spec":{"operation":"Running"}}'
```

`stacksImagePolicy: Exact` (default) requires the Stacks node and signer images that
wrote the data. `AllowDifferent` runs others, for example a newer release on the same
chain, and records each change in the frozen genesis. Use the same or a newer release:
database migrations only go forward. The Bitcoin image must always match.

Check the result:

```bash
k -n "$NS" get stacksnetwork network \
  -o jsonpath='{.status.phase} {.status.initialization.restoredFrom.name}{"\n"}'
g=$(k -n "$NS" get stacksnetwork network -o jsonpath='{.status.genesisRef.name}')
k -n "$NS" get stacksgenesis "$g" -o jsonpath='{.spec.source.restored}{"\n"}'
```

One snapshot restores into any number of namespaces at once; each gets fresh volumes
and identities. Add participants beyond the snapshot's cohort only after `Initialized`:
plain nodes sync in seconds to a minute, signers sign from the next reward cycle, and
miners need funding and coinbase maturity.

## Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| `ResolutionError`, reason `SnapshotMismatch` | The message lists every difference: genesis inputs, missing or extra participants, keys, links, stake or images. Fix the network to match, or capture a new snapshot. It shows while `Paused`, so a check needs no workload. |
| `GenesisInputsUnavailable`, "chainstate snapshot unavailable" | `snapshot.yaml` is not applied in this namespace, or is being deleted. |
| Participant `RestoreFailed` | The snapshot image cannot be pulled on the actor's node (not loaded into kind, or not in the registry), or an archive checksum failed. |
| `BitcoinInitialization` `RestoredChainDiffers` | The restored Bitcoin node does not report the snapshot's tip: the archive and snapshot resource come from different captures. |
| `BitcoinInitialization` `RestoredWalletsImmature` | A miner wallet has no mature output in the captured chain. Capture a network that mined longer. |
| `BitcoinInitialization` `RestoreRefreshUnacknowledged` | The refresh block offer went unanswered twice; check the Bitcoin production worker. |

## Lifetime

- The snapshot resource is needed only until the network freezes; delete it afterwards
  if you like. The image must stay pullable while restored networks exist, since every
  restored actor start runs its restore step (a no-op once the data is in place).
- Tear restored networks down like any other: `Stopped`, delete the network, then the
  namespace ([operations](operations.md)).

## Limits

- Restore always creates a new network; it does not rewind or restart one in place.
- A snapshot fixes the consensus shape; removing, replacing or re-linking an initial
  participant needs a new capture. Signers and stackers added after initialization
  cannot be captured: a restored network would miss their signing weight.
- A stacker's `amountMicroSTX` and `lockCycles` must keep their initial values until
  capture: the snapshot records the initial stake, so capture refuses a changed one.
  Change stake on the restored network instead, once it is `Initialized`.
- Node settings that shape stored data (event observers, indexes) apply from the restore
  point only.
- Qualified on kind with locally loaded images; registries and managed clusters are
  untested.

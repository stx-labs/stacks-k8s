# stacks-snapshot

Capture an initialized network's chain data as a restorable chainstate snapshot. The
result is one gzip tar archive per Bitcoin node, Stacks node and signer, a
ready-to-apply `StacksChainstateSnapshot` manifest and a Dockerfile for the snapshot
image that carries the archives. A network that sets
`spec.genesis.restore.snapshotRef` to that resource starts from the captured chain
instead of initializing from genesis. The
[operations guide](../../docs/network-operator/chainstate-snapshots.md) walks through
the whole workflow, from a source network with imported keys to a restore.

Capture **stops the source network**. `Stopped` is terminal, so capture a network you
no longer need running. If the export fails after the stop, rerun the same request
with `"resume": true` and an empty output directory: it exports again from the tips
the first run recorded.

```bash
GOWORK=off go -C tools/stacks-snapshot run . capture < capture.json > report.json
```

```json
{
  "kubeconfig": "/path/to/kubeconfig",
  "context": "kind-stacks-k8s",
  "namespace": "lab",
  "network": "network",
  "networkUID": "12345678-1234-1234-1234-123456789abc",
  "snapshot": "epoch40-minimal",
  "image": "localhost/stacks-snapshots/epoch40-minimal:v1",
  "output": "/abs/path/epoch40-minimal",
  "bitcoinHeight": 260,
  "timeoutSeconds": 1800
}
```

| Field | Meaning |
| --- | --- |
| `kubeconfig`, `context` | Cluster selection. Static certificate or token credentials only; exec and auth-provider plugins are rejected. |
| `namespace`, `network`, `networkUID` | The source network. The UID guards against acting on a recreated network with the same name. |
| `snapshot` | Name of the `StacksChainstateSnapshot` the manifest declares. |
| `image` | Reference of the snapshot image that will hold the archives under `/snapshot`; recorded as `spec.image`. Capture does not build or push it. |
| `exportImage` | Optional. BusyBox-compatible image: it runs the export Pod and is the snapshot image's base, so it must provide `sh`, `sleep`, `tar` with gzip, `sha256sum`, `cut`, `cat`, `find` and `sync`. Defaults to BusyBox pinned by digest. |
| `output` | Absolute path of a new or empty directory for the archives, `snapshot.yaml` and `Dockerfile`. It is created only after the network identity is confirmed. |
| `bitcoinHeight` | Optional. Pause once every Bitcoin node reports at least this height. Not allowed with `resume`. |
| `timeoutSeconds` | Bound for the whole capture, 60 to 86400. |
| `resume` | Optional. Export a network an earlier capture already stopped, from the tips it recorded on the network. |

## What capture does

1. Requires an `Initialized` network that is not stopped, and reads its frozen initial
   cohort. It refuses accounts with generated keys, including accounts funded only by
   `spec.genesis.allocations`: a restoring network must import the same keys to reproduce
   the cohort's public identities and the genesis balances. It also refuses, before
   anything stops, ephemeral actor storage (stopping deletes it) and signers, stackers
   or removed participants outside the initial cohort (the restored signer set could
   miss their weight), and stackers whose stake changed after initialization (the
   snapshot records the initial stake).
2. Optionally waits for `bitcoinHeight`, then sets `spec.operation: Paused` and waits
   for phase `Paused`. Bitcoin production stops; nodes keep running.
3. Waits until the chain is still: the observation of every selected Bitcoin node and
   Stacks node is refreshed twice in a row with the same tips, and the nodes of each
   chain agree. Records of removed Bitcoin nodes are skipped.
   It records the Bitcoin and Stacks tips from these status observations.
4. Sets `spec.operation: Stopped` and waits for phase `Stopped`. The same patch records
   the tips in the network's `snapshot.stacks.org/capture` annotation, for a resume.
   Actor volumes stay. It then re-reads the actor images and refuses to export if one
   changed during the capture: the manifest would name the wrong writer of the data.
5. For each actor volume, creates a Pod with the actor's resource bounds and tolerations
   that mounts it read-only and streams `tar` out of it over `exec`. Locally, it gzips
   the stream, computes its sha256 and drops:
   - the root entry;
   - the peer and mempool files (Stacks `peer.sqlite*` and `mempool.sqlite*`, Bitcoin
     Core `peers.dat`, `anchors.dat` and `mempool.dat`), which hold the source
     network's peers (and the node's p2p key) and pending transactions;
   - the restore marker of a network that was itself restored.

   It refuses links, devices and members outside the data root, and a stream that ends
   without tar's end-of-archive marker. A failed tar ends the stream at once with its
   error; a partial archive is deleted.
6. Writes `snapshot.yaml` and `Dockerfile`, and prints a report with the tips, archive
   checksums and step times. None of them contains keys.

With `resume`, capture requires a `Stopped` network carrying the recorded tips, skips
steps 2 to 4, and exports into the new output directory. A failure before the stop
leaves the network `Paused`; set it back to `Running` to keep using it.

Only the initial cohort is captured. Participants that joined later are not part of the
snapshot; add them to the restored network after it initializes.

The export Pod runs as the actor user (1000) without privileges, a service account
token or a writable root filesystem. It expires on its own after `timeoutSeconds`, and
a resumed export deletes the ones an interrupted run left behind.

## Build, load and restore

The Dockerfile copies the archives onto the base image under `/snapshot`, readable by
the restore user. It needs BuildKit, Docker's default builder since version 23:

```bash
docker build -t localhost/stacks-snapshots/epoch40-minimal:v1 /abs/path/epoch40-minimal
kind load docker-image localhost/stacks-snapshots/epoch40-minimal:v1 --name stacks-k8s
```

Load the image again after recreating the cluster; it stays in the local Docker. To
share a snapshot, push the image to a registry instead and hand out `snapshot.yaml`
with the scenario's keys. Restore checks every archive's sha256, so a tag is enough.

To restore, apply `snapshot.yaml` in the restoring namespace and select it from a
network whose initial cohort imports the same keys:

```yaml
spec:
  genesis:
    restore:
      snapshotRef: {name: epoch40-minimal}
      stacksImagePolicy: Exact   # AllowDifferent runs other Stacks node and signer images
```

The network copies what it uses from the resource into its frozen genesis, so the
resource is needed only until the network freezes. Keep the image available while a
restored network exists: every actor Pod starts with the image's restore step (a no-op
once its volume holds the data).

## Permissions

Capture uses the caller's credentials; the operator gains no permissions. Outside a
cluster-admin context, bind this Role in the source namespace:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: stacks-snapshot-capture
rules:
  - apiGroups: [network.stacks.org]
    resources: [stacksnetworks]
    verbs: [get, patch]
  - apiGroups: [network.stacks.org]
    resources: [stacksnetworkparticipants, stacksgeneses]
    verbs: [get]
  - apiGroups: [stacks.stacks.org]
    resources: [stacksaccounts]
    verbs: [get]
  - apiGroups: [bitcoin.stacks.org]
    resources: [bitcoinexecutions]
    verbs: [get]
  - apiGroups: [""]
    resources: [persistentvolumeclaims]
    verbs: [list]
  - apiGroups: [""]
    resources: [pods]
    verbs: [get, list, create, delete]
  - apiGroups: [""]
    resources: [pods/exec]
    verbs: [get, create]
```

No Secret, Node or PersistentVolume access is needed: the scheduler places the export
Pod on its volume's node. Both verbs on `pods/exec` are listed because clusters differ
in how they authorize the exec stream: some check `get` for the WebSocket upgrade,
others `create`.

---
name: stacks-k8s-images
description: Build stacks-core actor images from a branch, commit or patched worktree, load them into the stacks-k8s kind cluster, select them for a network or one participant, and roll or verify a running actor's image. Use for instrumented or patched builds, mixed-version networks, image pull or load failures, and builds that starve the cluster.
---

# Build and select stacks-k8s actor images

Actors run the images a network selects; the operators never build or check out
source. Without `spec.defaults.images`, the operator's built-in defaults apply
(currently `ghcr.io/stacks-network/stacks-core:4.0.5`,
`ghcr.io/stacks-network/stacks-signer:4.0.5` and `bitcoin/bitcoin:31.1`). Operator
images are covered by the `stacks-k8s-cluster` skill.

```bash
export STACKS_KIND_CLUSTER=stacks-k8s
export KUBECONFIG="$PWD/tools/local-cluster/kubeconfig"   # from the stacks-k8s root
ARCH=$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')
```

## Budget the Docker VM first

kind and image builds share the Docker VM's CPU and memory. A stacks-core build next
to a running network can stall the API server (`TLS handshake timeout`) or get the
kind nodes OOM-killed, which loses initializing networks.

- Build and load every image **before** creating the network or setting `Running`.
- For a full `release` build (fat LTO, `codegen-units = 1`), stop the cluster first
  with `make cluster-stop`. One observed run exceeded 45 minutes and then ran out of
  memory in a 16 GB VM.
- For local verification, prefer the thin-LTO recipe in
  [release-lite.Dockerfile](assets/release-lite.Dockerfile), which builds only
  `stacks-node` and `stacks-signer`; a comparable build took about 3 minutes. Its
  codegen and timing differ from release images; say so in any finding.

## Build a revision

Use a separate stacks-core worktree per revision or patch; image labels and build
arguments record metadata but never select source.

```bash
STACKS_SOURCE=/absolute/path/to/stacks-core
STACKS_REVISION=branch-or-commit
STACKS_BUILD=$(mktemp -d)/stacks-core
git -C "$STACKS_SOURCE" worktree add --detach "$STACKS_BUILD" "$STACKS_REVISION"
# Apply any patch in $STACKS_BUILD now.
STACKS_COMMIT=$(git -C "$STACKS_BUILD" rev-parse HEAD)
export STACKS_IMAGE="stacks-core:${STACKS_COMMIT:0:12}-patch-01"   # new tag per build
docker build --platform "linux/$ARCH" \
  -f .agents/skills/stacks-k8s-images/assets/release-lite.Dockerfile \
  --build-arg "STACKS_NODE_VERSION=local-$STACKS_COMMIT" \
  --build-arg "GIT_BRANCH=$STACKS_REVISION" --build-arg "GIT_COMMIT=$STACKS_COMMIT" \
  --label "org.opencontainers.image.revision=$STACKS_COMMIT" \
  -t "$STACKS_IMAGE" "$STACKS_BUILD"
```

Drop the `-f` line to use the worktree's own root Dockerfile (full `release`). The
image must put `stacks-node` and `stacks-signer` on `PATH`; one image can serve both
node and signer participants. Older revisions may need another recipe or toolchain.

Record the commit, the full local patch (including new files), build options and the
content ID: `docker image inspect "$STACKS_IMAGE" --format '{{.Id}}'`.

## Load into kind

Locally built images load directly:

```bash
kind load docker-image "$STACKS_IMAGE" --name "$STACKS_KIND_CLUSTER"
```

Images pulled from a registry are multi-platform and fail with `content digest … not
found`. Pull the node platform and load an archive instead, including for the stock
defaults:

```bash
IMAGE=ghcr.io/stacks-network/stacks-core:4.0.5
docker pull --platform "linux/$ARCH" "$IMAGE"
docker image save --platform "linux/$ARCH" -o /tmp/actor-image.tar "$IMAGE"
kind load image-archive /tmp/actor-image.tar --name "$STACKS_KIND_CLUSTER"
```

Confirm with `docker exec stacks-k8s-worker crictl images | grep stacks-core`. The
network default pull policy, `IfNotPresent`, uses loaded images; never reuse a tag for
different content or set `Always` for local-only images.

## Select images

| Field | Scope | Mutable |
| --- | --- | --- |
| `StacksNetwork.spec.defaults.images.{bitcoin,stacksNode,stacksSigner}` | Every actor of that kind | No, frozen at root creation |
| Reusable definition `spec.image` | Participants using that definition | Yes |
| Participant entry `overrides.<branch>.image` | One participant | Yes |

```yaml
- name: miner-01
  kind: StacksNode
  definition: {ref: {name: miner-01}}
  overrides:
    stacksNode:
      image: stacks-core:0123456789ab-patch-01
```

Decide defaults before applying a new network; afterwards change images per
participant. Keep the entry's name, kind and definition unchanged. Never edit generated
StatefulSets or Pods directly.

## Roll and verify

An image change rolls only that actor's StatefulSet. There is no network-wide upgrade
sequencer; change one actor, verify it, then the next:

```bash
kubectl -n lab-x get stacksnetworkparticipant -o \
  custom-columns=NAME:.spec.participantName,IMAGE:.status.runtime.imageID,POD:.status.runtime.podRef.name
```

Check the new `imageID`, native RPC and protocol progress before moving on; a Ready Pod
does not prove a successful upgrade. The PVC keeps chain data across the roll; whether
the new binary can open it is the binary's concern, and selecting an older image is not
a database rollback. Changing images never needs new genesis.

Do not change management-worker images or placement as an upgrade experiment: losing a
bound stacker, contract or traffic worker ends managed operation.

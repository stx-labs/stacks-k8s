---
name: stacks-k8s-cluster
description: Create, start, stop or destroy the local stacks-k8s kind cluster; build and install or upgrade the network operator and its CRDs; install Chaos Mesh and dashboard add-ons; tear down experiments in order; recover when kubectl cannot reach the cluster. Use before the first network, after changing the stacks-k8s checkout, or when the cluster or an operator misbehaves.
---

# Run the stacks-k8s cluster

Run commands from the root of a stacks-k8s checkout at current `main` or a pinned
release, in its own worktree. The operator renders actor configuration, so an old
checkout can produce configuration that a newer stacks-core image rejects, which shows
up only as an opaque `Init:Error`.

## Select the cluster

Reuse the dedicated `stacks-k8s` kind cluster and leave it running. Never point
commands at another cluster or the global kubeconfig context.

```bash
export STACKS_KIND_CLUSTER=stacks-k8s
export KUBECONFIG="$PWD/tools/local-cluster/kubeconfig"
export STACKS_CONTEXT=kind-stacks-k8s
kubectl --context "$STACKS_CONTEXT" get nodes
```

The cluster has one control plane and two workers (`stacks-k8s-worker`,
`stacks-k8s-worker2`), Kubernetes 1.37.0. Host `127.0.0.1:14000` maps to NodePort
30400 for the optional telemetry dashboard.

## Lifecycle

| Command | Effect |
| --- | --- |
| `make cluster-create` | Create the cluster with Headlamp and Metrics Server; writes `tools/local-cluster/kubeconfig` |
| `make cluster-start` | Restart stopped node containers and wait for Ready |
| `make cluster-stop` | Stop node containers, keeping data; frees the Docker VM for image builds |
| `make cluster-destroy` | Delete the cluster and all its data; only when the user asks |
| `make cluster-chaos-install` | Install or reconcile the pinned Chaos Mesh 2.8.4 chart; idempotent |
| `make cluster-headlamp` | Port-forward Headlamp to `127.0.0.1:8080`; token from `make cluster-headlamp-token` |

`HEADLAMP=false` and `METRICS_SERVER=false` skip add-ons at creation. Telemetry needs
Metrics Server. Stopping and starting preserves disk data, not uninterrupted
execution: check every network's conditions after a restart.

## Install the network operator

Install once per cluster; it watches every namespace. The operator image also runs
resolver Jobs and management workers, and pins the Bitcoin observer sidecar image.
Use a fresh tag for every build: reusing a tag neither restarts the Deployment nor
guarantees the new image is used.

```bash
TAG="dev-$(git rev-parse --short HEAD)-$(date +%s)"
docker build -f operators/network/Dockerfile -t "stacks-network-operator:$TAG" .
kind load docker-image "stacks-network-operator:$TAG" --name "$STACKS_KIND_CLUSTER"
helm upgrade --install network charts/stacks-network-operator \
  --kube-context "$STACKS_CONTEXT" -n stacks-network-system --create-namespace \
  --set image.tag="$TAG" --set bitcoinObserverImage="stacks-network-operator:$TAG"
kubectl --context "$STACKS_CONTEXT" -n stacks-network-system rollout status deploy/network
```

Add `--set bitcoinActions.generationEnabled=true` (and `reorganizationEnabled=true`)
only when bounded Bitcoin actions are needed; see the `stacks-k8s-faults` skill. The
observability release is covered by the `stacks-k8s-observability` skill.

**Helm never upgrades CRDs.** After pulling a checkout that changed `charts/*/crds/`,
apply them before upgrading the release, and review conflicts instead of forcing:

```bash
kubectl --context "$STACKS_CONTEXT" apply --server-side -f charts/stacks-network-operator/crds/
```

The operators serve `v1alpha2` only and never convert older stored resources. See
`docs/network-operator/migration.md` before replacing an older installation.

## Check health

| Check | Command |
| --- | --- |
| Nodes Ready | `kubectl get nodes` |
| Operator rolled out | `kubectl -n stacks-network-system rollout status deploy/network` |
| Operator logs | `kubectl -n stacks-network-system logs deploy/network --since=10m` |
| Served API | `kubectl api-resources --api-group=network.stacks.org` |
| Node pressure | `kubectl top nodes`; `docker stats --no-stream` |

## Troubleshoot

| Symptom | Cause and action |
| --- | --- |
| `connection refused` to the API server | Node containers are stopped: `make cluster-start` |
| `TLS handshake timeout`, very slow `kubectl` | The Docker VM is saturated, usually by an image build. Wait or stop the build; build images with the cluster stopped |
| Node containers exited 137 | The Docker VM ran out of memory and killed kind. `make cluster-start`, then abandon any network that was initializing |
| Actor Pods `Init:Error` | The `config-check` init container discards stderr, so its logs are empty. Check that the operator checkout is current for the actor image version |
| `ErrImageNeverPull`, `ErrImagePull` | The tag was not loaded into kind; load it (see the `stacks-k8s-images` skill) |

After nodes are killed mid-initialization, actor Pods can fail in init and stay held by
the `network.stacks.org/actor-termination` finalizer, so `Stopped` and deletion never
finish. There is no supported recovery yet. Never remove finalizers by hand: record
the namespace as abandoned and continue in a fresh one.

## Tear down an experiment

Order matters, because each step needs the controllers of the next:

1. Delete native faults and wait for them to finish deleting.
2. Delete or let bounded action requests settle.
3. Stop and delete the network root:

   ```bash
   kubectl -n lab-x patch stacksnetwork network --type=merge -p '{"spec":{"operation":"Stopped"}}'
   kubectl -n lab-x wait stacksnetwork/network --for=jsonpath='{.status.phase}'=Stopped --timeout=5m
   kubectl -n lab-x delete stacksnetwork network --wait=true --timeout=5m
   ```

4. Delete the namespace's `NetworkTelemetry`; it keeps recording after the root is gone.
   Note its UID first, then remove its collector checkpoints from every node:
   `docker exec <node> rm -rf /var/lib/stacks-telemetry/<telemetry-uid>`. Recorded data
   stays in the backend until its retention expires.
5. Uninstall the namespace's action release (`helm uninstall actions -n lab-x`); its
   Helm record lives in that namespace.
6. Review retained PVCs (garbage collection can take a few seconds), then delete the
   namespace and remove it from the telemetry `watchNamespaces`.

Leave the cluster, the network operator, Chaos Mesh and the telemetry backend running.
Helm uninstall keeps CRDs; deleting CRDs discards every network in the cluster.

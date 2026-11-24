# Network operator module

The Go module for the network operator: the aggregate and actor controllers,
capability workload controllers and the scoped execution workers, built into one
image. Entrypoints live under `cmd/`. See the [runtime reference](../../docs/network-operator/runtime.md)
for controllers, workers and status ownership.

Development requires Go 1.27.4, and Node.js 24 or newer with npm for the offline
Stacks.js oracle under `transactions/`. API types and their generator toolchain live
in [apis/network](../../apis/network/README.md).

Run development commands from the repository root:

```bash
make generate
make verify-network
```

Generated deepcopy code and CRDs are committed; do not edit them by hand.

The opt-in live suite in [internal/publicintegration](internal/publicintegration/README.md)
creates a disposable network from the [API examples](../../docs/api/examples/) on an
explicitly selected cluster and checks initialization, lifecycle, optional actions and
native faults (`make -C operators/network test-live`).

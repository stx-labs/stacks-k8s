# Network declarations

Use the [complete composition example](../../docs/design/public-api/examples/30-actors.yaml).
It includes generated account keys, reusable definitions and a paused network.
Set images and placement for your cluster before applying it, then request Running.
See [operations](../../docs/network-operator/operations.md).

For a shorter first run, the [minimal example](../../docs/design/public-api/examples/minimal.yaml)
declares the smallest accepted cohort with earlier epochs and batched first-gate generation.

To skip bootstrap on repeated runs, the [restore example](../../docs/design/public-api/examples/restore.yaml)
starts the same cohort from a chainstate snapshot captured with
[stacks-snapshot](../../tools/stacks-snapshot/README.md); see
[chainstate snapshots](../../docs/design/chainstate-snapshots.md).

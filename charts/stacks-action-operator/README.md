# Stacks action operator

Optional, independently installed `actions.stacks.org/v1alpha2` lifecycle controllers
for finite Bitcoin generation and bounded local reorganization. The controllers read
public network and execution records and write action status and finalizers. The
network's Bitcoin control worker remains the only RPC sender. See the
[action operator guide](../../docs/action-operator/README.md).

## Install

Install this chart in the network namespace **before** enabling the network chart's
`bitcoinActions.generationEnabled` or `bitcoinActions.reorganizationEnabled`. This
chart ships the action CRDs. With either network value set, each Bitcoin control
worker lists the enabled action kinds before serving baseline offers, so a missing
CRD stops baseline Bitcoin production.

```bash
helm upgrade --install actions charts/stacks-action-operator \
  --namespace lab --set image.tag=dev
helm upgrade network charts/stacks-network-operator -n stacks-network-system \
  --reuse-values --set bitcoinActions.generationEnabled=true
```

| Value | Default | Effect |
| --- | --- | --- |
| `bitcoinGeneration.enabled` | `true` | `BitcoinBlockGeneration` lifecycle controller and its RBAC. |
| `bitcoinReorganization.enabled` | `false` | `BitcoinReorganization` lifecycle controller and its RBAC. |

At least one must be enabled. Both network-side values default to false; a kind
needs both sides. Installing lifecycle controllers alone grants no Bitcoin mutation
authority.

The chart rejects existing action CRDs that serve a version other than `v1alpha2`; it
never deletes or converts old resources. Helm does not upgrade CRDs; apply schema
updates explicitly as described in
[installation compatibility](../../docs/network-operator/migration.md). Keep the CRDs
installed while either network value is enabled.

## Resources

- One Deployment with leader election in the release namespace, watching only that
  namespace.
- A `ResourceQuota` allowing 64 objects of each action kind.
- A namespaced Role. Lifecycle controllers `get` networks, participants and
  `BitcoinExecution` records, and `get`/`list`/`watch`/`patch` enabled action kinds
  and `patch` their status. `events` and `leases` permissions serve leader election.
  They receive no Secret access and cannot write execution records, actor workloads
  or network status. `operators/action` tests the exact rendered permissions.

## Changing enablement

Change enablement only after action reservations settle or the environment is
disposed. Disabling a kind on the network chart removes the worker's read permission;
it is not cancellation and can strand retained uncertainty or cleanup. While the
controller and worker retain access, delete an action to cancel it or pause the
network to hold sends. Uninstall this release only after its requests have released their
finalizers.

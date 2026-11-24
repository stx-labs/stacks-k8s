# Action operator

Independent Go runtime for bounded Bitcoin action lifecycles. The default
`BitcoinBlockGeneration` controller and optional `BitcoinReorganization`
controller project the network's `BitcoinExecution` records into action status
and cleanup finalizers. They do not issue RPCs or write execution records; the
network operator's Bitcoin control worker admits and sends.

- [Operating guide](../../docs/action-operator/README.md)
- [Bitcoin actions](../../docs/action-operator/bitcoin-actions.md)
- [Helm chart](../../charts/stacks-action-operator/README.md)

The manager serves only `--api-version=v1alpha2` (the default). Flags
`--bitcoin-generation-enabled` (default `true`) and
`--bitcoin-reorganization-enabled` (default `false`) select the controllers; at least
one is required. It watches one namespace (`--watch-namespace`, defaulting to
`WATCH_NAMESPACE` or the ServiceAccount namespace) and uses leader election.

The module depends only on the versioned `apis/network` module; it imports no other
operator's implementation.

```bash
make -C operators/action verify
```

`verify` runs formatting, vet, unit and race tests, and the envtest integration
suite (`-tags=integration`). `chart_test.go` checks the chart's exact rendered RBAC.

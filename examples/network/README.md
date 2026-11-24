# Network declarations

| Example | Use |
| --- | --- |
| [minimal](../../docs/api/examples/minimal.yaml) | Shortest first run: the smallest accepted cohort, with the only signer on the mining node, earlier epochs and batched first-gate generation. |
| [separate roles](../../docs/api/examples/separate-roles.yaml) | Miner and two signers in separate stacks-node processes on separate kind nodes, one stacker per signer, plus a follower and faucet. Default for verifying reported vulnerabilities. |
| [30 actors](../../docs/api/examples/30-actors.yaml) | Complete composition: four miners, six signers, followers, reusable definitions. Uses the 60-second default cadence and built-in epochs, so initialization is slow. |

Each example creates its own namespace and a paused network. Set images and placement
for your cluster before applying it, then request Running. See
[operations](../../docs/network-operator/operations.md). Agents can use the
[topology skill](../../.agents/skills/stacks-k8s-topology/SKILL.md) for wiring recipes
and per-kind field references.

Server-side dry-run does not create the manifest's `Namespace`, so create it first:

```bash
kubectl create namespace lab-minimal
kubectl apply --dry-run=server -f docs/api/examples/minimal.yaml
```

Dry-run checks schema and admission rules only; reference resolution is reported by
the paused network's `Resolved` condition.

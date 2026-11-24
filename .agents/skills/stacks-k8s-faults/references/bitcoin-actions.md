# Bounded Bitcoin actions

`BitcoinBlockGeneration` and `BitcoinReorganization` are finite requests executed by
the network's existing Bitcoin control worker, the only Bitcoin RPC sender. The
optional action operator only tracks their lifecycle.

## Install

Both sides must be enabled. Install the action operator **in the network namespace**
first, since its chart ships the action CRDs:

```bash
TAG="dev-$(git rev-parse --short HEAD)-$(date +%s)"
docker build -f operators/action/Dockerfile -t "stacks-action-operator:$TAG" .
kind load docker-image "stacks-action-operator:$TAG" --name stacks-k8s
helm upgrade --install actions charts/stacks-action-operator -n lab-x \
  --set image.repository=stacks-action-operator --set image.tag="$TAG" \
  --set bitcoinReorganization.enabled=true
```

Then add the executor permissions on the cluster-wide network operator release (see the
`stacks-k8s-cluster` skill):

```bash
helm upgrade network charts/stacks-network-operator -n stacks-network-system --reuse-values \
  --set bitcoinActions.generationEnabled=true \
  --set bitcoinActions.reorganizationEnabled=true
```

Enabling the network values while the action CRDs are absent stops baseline Bitcoin
production: the control worker's action list fails before it serves the baseline offer.

Omit the two reorganization flags when only generation is needed; the action chart
enables generation by default. A request whose kind is not enabled on both sides stays
unadmitted and expires. Change the flags only after requests settle:
disabling one is not cancellation.

## Fill a request

- `networkUID`: the current root UID.
- `bitcoinNodeRef.name`: a `BitcoinNode` participant name.
- `address`: a regtest payout address, for example a wallet's
  `kubectl get bitcoinwallet production-wallet -o jsonpath='{.status.bitcoinAddress}'`.
- `timeout`: up to 10m, counted from creation, including time spent pending.

Specs are immutable. Create a new request for another attempt.

```yaml
apiVersion: actions.stacks.org/v1alpha2
kind: BitcoinBlockGeneration
metadata:
  name: three-blocks
  namespace: lab-x
  labels:
    actions.stacks.org/correlation-id: extra-blocks
spec:
  networkUID: "${NETWORK_UID}"
  bitcoinNodeRef: {name: btc-01}
  count: 3
  cadence: {mode: Fixed, intervalSeconds: 2}
  address: "${PAYOUT_ADDRESS}"
  timeout: 2m
---
apiVersion: actions.stacks.org/v1alpha2
kind: BitcoinReorganization
metadata:
  name: replace-two
  namespace: lab-x
spec:
  networkUID: "${NETWORK_UID}"
  bitcoinNodeRef: {name: btc-01}
  depth: 2                         # invalidates 2 blocks, mines 3 replacements
  address: "${PAYOUT_ADDRESS}"
  boundaryPolicy:                  # all three must be true
    allowEpochBoundaryCrossing: true
    allowRewardCycleBoundaryCrossing: true
    allowPreparePhaseBoundaryCrossing: true
  timeout: 2m
```

## Semantics

- Generation sends one block per RPC and shares the production worker's execution
  reservation with baseline production; eligible requests are served in creation-time
  and UID order. Every dispatch obeys the network's initialization ceiling.
- A reorganization is local to the selected node: it invalidates the suffix and mines
  depth+1 replacements there. Other nodes converge only through normal Bitcoin peering.
  Cleanup compensates only its own captured invalidity marker; it does not restore the
  old chain.
- A namespace quota allows 64 requests per kind.
- Watch `status.phase` with the admission, receipt and cleanup conditions. Completion
  reports acknowledged RPCs, not consensus or recovery.
- Deleting a request cancels pending work but cannot retract an RPC already sent. An
  ambiguous execution keeps the target excluded; it is never replayed.

Field details: [bitcoinblockgeneration.md](bitcoinblockgeneration.md),
[bitcoinreorganization.md](bitcoinreorganization.md).

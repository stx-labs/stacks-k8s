# BitcoinNode

A Bitcoin Core regtest actor definition, `bitcoin.stacks.org/v1alpha2`. It has no mining
role: `BitcoinBlockProduction` generates blocks through each node's control worker.

## Usage

- `walletRefs` lists the `BitcoinWallet`s this node loads. A miner's Bitcoin node must
  load that miner's wallet. The examples load production's payout wallet on the
  initialization target.
- `peers` is `discovery: Network` (default: seed from the other selected Bitcoin
  participants) or explicit `nodeRefs`. Seeds are startup hints, fixed for a
  configuration generation.
- `config.overrides` accepts native option names; one nested object selects a chain
  section such as `regtest`. `config.secretRef` selects a complete `bitcoin.conf`
  instead. Chain selection, RPC, listeners, seeds, indexes and wallets are protected.
- `observer.enabled: true` adds a read-only RPC metrics sidecar.
- Common actor fields: `image`, `imagePullPolicy`, `resources`, `placement`, `storage`.
  Storage defaults to 2Gi, not retained; `ephemeral: true` uses `emptyDir`.

## Updates

Peers, wallets, image and resources can change; a peer or configuration change may roll
the actor. Storage class, mode and retention changes need a new participant; size can
grow but not shrink. `control.suspended: true` on the entry scales the actor to zero
and keeps its PVC.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`bitcoin.stacks.org/v1alpha2`, kind `BitcoinNode`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.config` | object | | Config selects configuration customization. | |
| `spec.config.compatibility` | string | | Compatibility permits intentionally divergent experimental actors. | one of `Managed`, `Unverified` |
| `spec.config.overrides` | object (free-form) | | Overrides contains Stacks TOML values or Bitcoin scalar/repeated-option/section values. | |
| `spec.config.secretRef` | ref → Secret key | | SecretRef selects a complete configuration version. | |
| `spec.config.serviceRefs` | []object | | ServiceRefs resolves explicit placeholders. | items ≤ 100; keyed by `alias` |
| `spec.config.serviceRefs[].alias` | string | yes | Alias identifies the placeholder. | length ≤ 63 |
| `spec.config.serviceRefs[].endpoint` | string | yes | Endpoint selects the advertised port role. | one of `rpc`, `p2p`, `events` |
| `spec.config.serviceRefs[].kind` | string | yes | Kind restricts the target API kind. | one of `BitcoinNode`, `StacksNode`, `StacksSigner` |
| `spec.config.serviceRefs[].name` | string | yes | Name identifies a selected participant. | length ≤ 63 |
| `spec.image` | string | | Image selects the actor binary. | length ≤ 512 |
| `spec.imagePullPolicy` | string | | ImagePullPolicy controls image retrieval. | one of `Always`, `IfNotPresent`, `Never` |
| `spec.observer` | object | | Observer enables the optional network-owned read-only RPC exporter; omission disables it. | |
| `spec.observer.enabled` | boolean | | Enabled starts the metrics and structured-log sidecar; omission is false. | |
| `spec.observer.intervalSeconds` | integer (int32) | | IntervalSeconds separates completed polls; omission uses 10 seconds. | 5–300 |
| `spec.peers` | object | | Peers supplies startup seed hints. | |
| `spec.peers.discovery` | string | | Discovery resolves the selected network graph. | one of `Network` |
| `spec.peers.nodeRefs` | []ref → BitcoinNode | | NodeRefs identifies explicit participant seeds. | items ≤ 100; keyed by `name` |
| `spec.placement` | object | | Placement applies only to actor Pods. | |
| `spec.placement.nodeSelector` | map[string]string | | NodeSelector constrains eligible nodes. | properties ≤ 32 |
| `spec.placement.spreadAcrossNodes` | boolean | | SpreadAcrossNodes requests preferred same-kind anti-affinity. | |
| `spec.placement.tolerations` | []core/v1 Toleration | | Tolerations allow explicit support-node placement. | items ≤ 32 |
| `spec.resources` | core/v1 ResourceRequirements | | Resources are workload requests/limits. | |
| `spec.storage` | object | | Storage configures instance-local persistence. | |
| `spec.storage.class` | string | | Class preserves explicit empty storageClassName. | length ≤ 253 |
| `spec.storage.ephemeral` | boolean | | Ephemeral selects emptyDir. | |
| `spec.storage.retainOnDelete` | boolean | | RetainOnDelete preserves a PVC after instance removal. | |
| `spec.storage.size` | string | | Size is a Kubernetes capacity quantity. | length ≤ 32 |
| `spec.walletRefs` | []ref → BitcoinWallet | | WalletRefs selects reusable wallet identities loaded by this node. | items ≤ 100; keyed by `name` |
| `spec.workerPlacement` | object | | WorkerPlacement applies to scoped support Pods. | |
| `spec.workerPlacement.nodeSelector` | map[string]string | | NodeSelector constrains eligible nodes. | properties ≤ 32 |
| `spec.workerPlacement.spreadAcrossNodes` | boolean | | SpreadAcrossNodes requests preferred same-kind anti-affinity. | |
| `spec.workerPlacement.tolerations` | []core/v1 Toleration | | Tolerations allow explicit support-node placement. | items ≤ 32 |

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec.config` | config overrides and secretRef are exclusive | `!(has(self.overrides) && has(self.secretRef))` |
| `spec.peers` | select nodeRefs or discovery | `has(self.nodeRefs) != has(self.discovery)` |
| `spec.storage` | ephemeral storage excludes PVC settings | `!has(self.ephemeral) \|\| !self.ephemeral \|\| (!has(self.size) && !has(self.class) && !has(self.retainOnDelete))` |

<!-- END GENERATED: crd-reference -->

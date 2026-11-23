# StacksNode

A Stacks peer actor definition, `stacks.stacks.org/v1alpha2`: miner, signer host or
follower.

## Usage

- `bitcoinNodeRef` (a `BitcoinNode` participant) is required after composition.
- Miners need `identityAccountRef` and `mining.bitcoinWalletRef`. The wallet must derive
  from the same account and be loaded by the `bitcoinNodeRef` node. Other nodes may omit
  `identityAccountRef` and get a generated, unfunded identity.
- A node hosts a signer when a `StacksSigner.nodeRef` names it; the operator configures
  the event subscription. A node hosts at most one signer. A miner may host one too.
- `peers` is `discovery: Network` (default) or explicit `nodeRefs` to other
  `StacksNode` participants. Nodes start without waiting for peers.
- `config.overrides` is a TOML tree merged after managed settings; arrays replace whole.
  Protected paths (genesis, epochs, PoX, identity, credentials, endpoints, signer events)
  are rejected with their path. `config.secretRef` selects a complete `config.toml`;
  `serviceRefs` resolve `${SERVICE:<alias>}` placeholders to Service DNS names.
- `config.compatibility: Unverified` permits divergence, reports
  `ConfigVerified=False`, and stops the node counting toward initial prerequisites.
- Services: RPC 20443, P2P 20444, metrics 9153.

## Updates

`mining.enabled` and `feeRateSatsPerVByte` can change live; toggling mining rolls the
node and waits for a loaded, mature wallet (`MiningReady`). Identity, Bitcoin node and
wallet bindings are fixed once admitted; changing them needs a new participant. Image,
resources and configuration changes roll only this actor.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`stacks.stacks.org/v1alpha2`, kind `StacksNode`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.bitcoinNodeRef` | ref → BitcoinNode | | BitcoinNodeRef is a mandatory participant binding after composition. | |
| `spec.config` | object | | Config selects configuration customization. | |
| `spec.config.compatibility` | string | | Compatibility permits intentionally divergent experimental actors. | one of `Managed`, `Unverified` |
| `spec.config.overrides` | object (free-form) | | Overrides contains Stacks TOML values or Bitcoin scalar/repeated-option/section values. | |
| `spec.config.secretRef` | ref → Secret key | | SecretRef selects a complete configuration version. | |
| `spec.config.serviceRefs` | []object | | ServiceRefs resolves explicit placeholders. | items ≤ 100; keyed by `alias` |
| `spec.config.serviceRefs[].alias` | string | yes | Alias identifies the placeholder. | length ≤ 63 |
| `spec.config.serviceRefs[].endpoint` | string | yes | Endpoint selects the advertised port role. | one of `rpc`, `p2p`, `events` |
| `spec.config.serviceRefs[].kind` | string | yes | Kind restricts the target API kind. | one of `BitcoinNode`, `StacksNode`, `StacksSigner` |
| `spec.config.serviceRefs[].name` | string | yes | Name identifies a selected participant. | length ≤ 63 |
| `spec.identityAccountRef` | ref → StacksAccount | | IdentityAccountRef supplies P2P/miner identity. | |
| `spec.image` | string | | Image selects the actor binary. | length ≤ 512 |
| `spec.imagePullPolicy` | string | | ImagePullPolicy controls image retrieval. | one of `Always`, `IfNotPresent`, `Never` |
| `spec.mining` | object | | Mining selects the optional miner role. | |
| `spec.mining.bitcoinWalletRef` | ref → BitcoinWallet | | BitcoinWalletRef supplies miner funding identity, including while disabled. | |
| `spec.mining.enabled` | boolean | | Enabled controls mining; omission is false. | |
| `spec.mining.feeRateSatsPerVByte` | integer (int32) | | FeeRateSatsPerVByte controls Bitcoin commit fees. | ≥ 1 |
| `spec.peers` | object | | Peers supplies startup hints. | |
| `spec.peers.discovery` | string | | Discovery resolves the selected network graph. | one of `Network` |
| `spec.peers.nodeRefs` | []ref → StacksNode | | NodeRefs identifies explicit participant seeds. | items ≤ 100; keyed by `name` |
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

Admission rules (CEL):

| Path | Message | Rules |
| --- | --- | --- |
| `spec.config` | config overrides and secretRef are exclusive | `!(has(self.overrides) && has(self.secretRef))` |
| `spec.peers` | select nodeRefs or discovery | `has(self.nodeRefs) != has(self.discovery)` |
| `spec.storage` | ephemeral storage excludes PVC settings | `!has(self.ephemeral) \|\| !self.ephemeral \|\| (!has(self.size) && !has(self.class) && !has(self.retainOnDelete))` |

<!-- END GENERATED: crd-reference -->

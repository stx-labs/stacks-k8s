# StacksSigner

A consensus signer actor definition, `stacks.stacks.org/v1alpha2`.

## Usage

- `nodeRef` names the `StacksNode` participant this signer attaches to; that node
  receives its event subscription.
- `accountRef` must be a signing-capable `StacksAccount`. Keep it separate from the
  stacker's holder and administrator accounts.
- Stake and registration come from a `StacksStacker` whose `signerRef` names this
  signer. `Resolved` does not wait for registration; `Registered` and reward-set
  participation are separate observations.
- Services: events 30000, metrics 31000.
- Image, resources, placement, storage and `config` follow the `StacksNode` rules,
  with signer-specific protected fields.

## Updates

Node and account bindings need a new participant. `control.suspended` on the entry stops
the signer process, which is an actor fault from the protocol's point of view.

## Fields

<!-- BEGIN GENERATED: crd-reference -->

`stacks.stacks.org/v1alpha2`, kind `StacksSigner`.
Generated from the chart CRD; do not edit.

| Field | Type | Required | Description | Constraints |
| --- | --- | --- | --- | --- |
| `spec.accountRef` | ref → StacksAccount | | AccountRef supplies consensus signing identity. | |
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
| `spec.nodeRef` | ref → StacksNode | | NodeRef selects the associated StacksNode participant. | |
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
| `spec.storage` | ephemeral storage excludes PVC settings | `!has(self.ephemeral) \|\| !self.ephemeral \|\| (!has(self.size) && !has(self.class) && !has(self.retainOnDelete))` |

<!-- END GENERATED: crd-reference -->

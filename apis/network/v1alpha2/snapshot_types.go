package v1alpha2

import (
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StacksChainstateSnapshot describes one captured chainstate artifact that fresh networks may restore.
// It records public facts only; private keys stay in the restoring network's own Secrets. A
// restoring network copies what it uses into its frozen genesis, so this resource is needed only
// until the network freezes; its image must stay available while the network exists.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:validation:XValidation:rule="self.spec == oldSelf.spec",message="chainstate snapshots are immutable"
type StacksChainstateSnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec identifies the artifact and the public inputs a restoring network must reproduce.
	Spec StacksChainstateSnapshotSpec `json:"spec"`
}

// StacksChainstateSnapshotSpec identifies an artifact and its source network's initial cohort.
// A restoring network pins the JSON digest of this spec, so every field added later must be
// optional and omitempty: otherwise existing snapshots change fingerprint on an operator upgrade.
// +kubebuilder:validation:XValidation:rule="self.participants.exists(p, has(p.archive))",message="a snapshot carries at least one actor archive"
type StacksChainstateSnapshotSpec struct {
	// Image carries the actor archives under /snapshot and provides a POSIX shell with tar (gzip),
	// sha256sum, cut, cat, find and sync, as BusyBox does. Every actor start runs it, so it must
	// stay available while a restored network exists. Restore verifies every archive checksum, so a
	// tag is accepted; a digest reference additionally pins delivery.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	Image string `json:"image"`
	// GenesisDigest identifies the source network's public chain configuration.
	// +kubebuilder:validation:MaxLength=71
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	GenesisDigest string `json:"genesisDigest"`
	// Participants projects the source initial cohort onto the values a restore depends on.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=1000
	// +listType=map
	// +listMapKey=name
	Participants []SnapshotParticipant `json:"participants"`
	// Bitcoin records the captured burnchain tip.
	Bitcoin SnapshotBitcoinTip `json:"bitcoin"`
	// Stacks records the captured Stacks tip.
	Stacks SnapshotStacksTip `json:"stacks"`
}

// SnapshotParticipant is the restore-relevant projection of one initial cohort member: the public
// identities by role, the links between participants, mining role, stake and registry threshold
// the captured chain depends on. Per-network identities (UIDs) and whole-policy digests are
// deliberately absent: resources, overrides and placement may differ in a restoring network.
// Worker accounts (transaction production, faucets) are not projected: they may change, and any
// they fund at genesis is already covered by the genesis digest.
// +kubebuilder:validation:XValidation:rule="has(self.archive) == (self.kind in ['BitcoinNode','StacksNode','StacksSigner'])",message="exactly the actor kinds carry an archive"
// +kubebuilder:validation:XValidation:rule="has(self.miningEnabled) == (self.kind == 'StacksNode')",message="miningEnabled is required on, and only on, a StacksNode"
// +kubebuilder:validation:XValidation:rule="has(self.bitcoinNode) == (self.kind == 'StacksNode')",message="bitcoinNode is required on, and only on, a StacksNode"
// +kubebuilder:validation:XValidation:rule="has(self.node) == (self.kind == 'StacksSigner')",message="node is required on, and only on, a StacksSigner"
// +kubebuilder:validation:XValidation:rule="has(self.signer) == (self.kind == 'StacksStacker')",message="signer is required on, and only on, a StacksStacker"
// +kubebuilder:validation:XValidation:rule="self.kind == 'StacksStacker' ? (has(self.amountMicroSTX) && has(self.lockCycles)) : (!has(self.amountMicroSTX) && !has(self.lockCycles))",message="stake is required on, and only on, a StacksStacker"
// +kubebuilder:validation:XValidation:rule="!has(self.registryThreshold) || self.kind == 'StacksContractSet'",message="registryThreshold applies to StacksContractSet only"
// +kubebuilder:validation:XValidation:rule="!has(self.registrySigners) || self.kind == 'StacksContractSet'",message="registrySigners applies to StacksContractSet only"
type SnapshotParticipant struct {
	// Name is the source network-relative instance name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`
	// Kind is the participant domain.
	Kind ParticipantKind `json:"kind"`
	// Accounts lists the public identities the participant uses, by the role that selects them.
	// +kubebuilder:validation:MaxItems=1000
	// +listType=map
	// +listMapKey=role
	// +listMapKey=address
	Accounts []SnapshotAccount `json:"accounts,omitempty"`
	// MiningEnabled preserves a Stacks node's mining role.
	MiningEnabled *bool `json:"miningEnabled,omitempty"`
	// BitcoinNode preserves a Stacks node's Bitcoin participant.
	// +kubebuilder:validation:MaxLength=63
	BitcoinNode string `json:"bitcoinNode,omitempty"`
	// Node preserves a signer's Stacks node participant.
	// +kubebuilder:validation:MaxLength=63
	Node string `json:"node,omitempty"`
	// Signer preserves a stacker's signer participant.
	// +kubebuilder:validation:MaxLength=63
	Signer string `json:"signer,omitempty"`
	// AmountMicroSTX preserves a stacker's stake.
	AmountMicroSTX *common.Amount `json:"amountMicroSTX,omitempty"`
	// LockCycles preserves a stacker's lock coverage.
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=12
	LockCycles *int32 `json:"lockCycles,omitempty"`
	// RegistryThreshold preserves a contract set's initial bridge registry threshold.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	RegistryThreshold *int32 `json:"registryThreshold,omitempty"`
	// RegistrySigners preserves a contract set's registry signer addresses in registry order: the
	// on-chain signer set and the principal derived from it depend on the order, which accounts
	// do not carry.
	// +kubebuilder:validation:MaxItems=100
	// +kubebuilder:validation:items:MaxLength=64
	// +kubebuilder:validation:items:Pattern=`^ST[0-9A-HJKMNP-TV-Z]{26,39}$`
	// +listType=atomic
	RegistrySigners []string `json:"registrySigners,omitempty"`
	// Archive locates an actor's captured data.
	Archive *SnapshotArchive `json:"archive,omitempty"`
}

// SnapshotAccount is one public identity a participant uses, with the role that selects it.
type SnapshotAccount struct {
	// Role is the participant field that selects the account.
	// +kubebuilder:validation:Enum=Identity;Signer;Holder;Administrator;Deployer;RegistrySigner;AggregateKey
	Role SnapshotAccountRole `json:"role"`
	// PublicIdentity is the account's address and public key; the key is compared in lower case.
	common.PublicIdentity `json:",inline"`
}

// SnapshotArchive locates one actor's captured data in the snapshot image.
type SnapshotArchive struct {
	// Path names a gzip tar archive directly under /snapshot, with members relative to the actor's
	// data root.
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[a-z0-9][-a-z0-9.]*\.tar\.gz$`
	Path string `json:"path"`
	// SHA256 is the archive's hex checksum, verified before extraction.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	SHA256 string `json:"sha256"`
	// Image is the actor image that last wrote the data.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	Image string `json:"image"`
}

// SnapshotBitcoinTip identifies the captured best Bitcoin block.
type SnapshotBitcoinTip struct {
	// Height is the captured block height.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4294967295
	Height int64 `json:"height"`
	// BlockHash is the captured block hash as Bitcoin RPC reports it.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	BlockHash string `json:"blockHash"`
}

// SnapshotStacksTip identifies the captured canonical Stacks tip.
type SnapshotStacksTip struct {
	// Height is the captured Stacks block height.
	// +kubebuilder:validation:Minimum=0
	Height int64 `json:"height"`
	// BlockHash is the captured tip block hash.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	BlockHash string `json:"blockHash"`
	// IndexBlockID is the captured tip's index block ID, SHA512/256(block hash || consensus hash).
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	IndexBlockID string `json:"indexBlockID"`
	// RewardCycle is the PoX reward cycle at capture.
	// +kubebuilder:validation:Minimum=0
	RewardCycle int64 `json:"rewardCycle"`
}

// StacksChainstateSnapshotList contains API objects.
// +kubebuilder:object:root=true
type StacksChainstateSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains objects.
	Items []StacksChainstateSnapshot `json:"items"`
}

//go:build integration

package foundationintegration

import (
	"context"
	"strings"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyChainstateSnapshotSchema checks snapshot admission rules and the creation-only restore selection.
func verifyChainstateSnapshotSchema(t *testing.T, ctx context.Context, c client.Client) {
	t.Helper()
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "chainstate-snapshot"}}
	if err := c.Create(ctx, namespace); err != nil {
		t.Fatal(err)
	}
	hex := func(n int) string { return strings.Repeat("a", n) }
	identity := common.PublicIdentity{
		Address:   "ST1PQHQKV0RJXZFY1DGX8MNSNYVE3VGZJSRTPGZGM",
		PublicKey: "02" + hex(64),
	}
	archive := func(name string) *api.SnapshotArchive {
		return &api.SnapshotArchive{Path: name + ".tar.gz", SHA256: hex(64), Image: "stacks-core:test"}
	}
	as := func(role api.SnapshotAccountRole) []api.SnapshotAccount {
		return []api.SnapshotAccount{{Role: role, PublicIdentity: identity}}
	}
	valid := func(name string) *api.StacksChainstateSnapshot {
		return &api.StacksChainstateSnapshot{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace.Name},
			Spec: api.StacksChainstateSnapshotSpec{
				Image:         "snapshot:test",
				GenesisDigest: "sha256:" + hex(64),
				Participants: []api.SnapshotParticipant{
					{Name: "btc-01", Kind: api.ParticipantBitcoinNode, Archive: archive("btc-01")},
					{
						Name:          "node-01",
						Kind:          api.ParticipantStacksNode,
						Accounts:      as(api.SnapshotAccountRoleIdentity),
						MiningEnabled: ptr.To(true),
						BitcoinNode:   "btc-01",
						Archive:       archive("node-01"),
					},
					{
						Name:     "signer-01",
						Kind:     api.ParticipantStacksSigner,
						Accounts: as(api.SnapshotAccountRoleSigner),
						Node:     "node-01",
						Archive:  archive("signer-01"),
					},
					{
						Name:           "stacker-01",
						Kind:           api.ParticipantStacksStacker,
						AmountMicroSTX: ptr.To(common.Amount("1000")),
						LockCycles:     ptr.To(int32(12)),
						Signer:         "signer-01",
					},
					{Name: "blocks", Kind: api.ParticipantBitcoinBlockProduction},
				},
				Bitcoin: api.SnapshotBitcoinTip{Height: 217, BlockHash: hex(64)},
				Stacks: api.SnapshotStacksTip{
					Height:       126,
					BlockHash:    hex(64),
					IndexBlockID: hex(64),
					RewardCycle:  10,
				},
			},
		}
	}
	snapshot := valid("valid")
	if err := c.Create(ctx, snapshot, client.FieldValidation("Strict")); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
	stored := &api.StacksChainstateSnapshot{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(snapshot), stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Spec.Participants) != 5 || stored.Spec.Participants[1].Archive == nil ||
		stored.Spec.Stacks.IndexBlockID != hex(64) {
		t.Fatalf("snapshot not preserved: %+v", stored.Spec)
	}
	stored.Spec.Bitcoin.Height++
	if err := c.Update(ctx, stored); !apierrors.IsInvalid(err) ||
		!strings.Contains(err.Error(), "chainstate snapshots are immutable") {
		t.Fatalf("snapshot mutation accepted: %v", err)
	}
	for name, mutate := range map[string]func(*api.StacksChainstateSnapshotSpec){
		"actor-without-archive": func(s *api.StacksChainstateSnapshotSpec) { s.Participants[0].Archive = nil },
		"worker-with-archive": func(s *api.StacksChainstateSnapshotSpec) {
			s.Participants[3].Archive = archive("stacker-01")
		},
		"signer-mining": func(s *api.StacksChainstateSnapshotSpec) {
			s.Participants[2].MiningEnabled = ptr.To(false)
		},
		"node-stake": func(s *api.StacksChainstateSnapshotSpec) {
			s.Participants[1].LockCycles = ptr.To(int32(2))
		},
		"no-archives": func(s *api.StacksChainstateSnapshotSpec) {
			s.Participants = s.Participants[3:]
		},
		"archive-outside-root": func(s *api.StacksChainstateSnapshotSpec) {
			s.Participants[0].Archive.Path = "../btc-01.tar.gz"
		},
		"archive-checksum": func(s *api.StacksChainstateSnapshotSpec) { s.Participants[0].Archive.SHA256 = "abc" },
		"genesis-digest":   func(s *api.StacksChainstateSnapshotSpec) { s.GenesisDigest = hex(64) },
		"index-block-id":   func(s *api.StacksChainstateSnapshotSpec) { s.Stacks.IndexBlockID = hex(40) },
		"no-participants":  func(s *api.StacksChainstateSnapshotSpec) { s.Participants = nil },
		"bitcoin-tip-hash": func(s *api.StacksChainstateSnapshotSpec) { s.Bitcoin.BlockHash = "" },
		"lock-cycles-range": func(s *api.StacksChainstateSnapshotSpec) {
			s.Participants[3].LockCycles = ptr.To(int32(13))
		},
		// A projection that omits a compared field could never validate, so the schema requires it.
		"node-without-mining":       func(s *api.StacksChainstateSnapshotSpec) { s.Participants[1].MiningEnabled = nil },
		"node-without-bitcoin-node": func(s *api.StacksChainstateSnapshotSpec) { s.Participants[1].BitcoinNode = "" },
		"signer-without-node":       func(s *api.StacksChainstateSnapshotSpec) { s.Participants[2].Node = "" },
		"stacker-without-signer":    func(s *api.StacksChainstateSnapshotSpec) { s.Participants[3].Signer = "" },
		"stacker-without-stake": func(s *api.StacksChainstateSnapshotSpec) {
			s.Participants[3].AmountMicroSTX = nil
		},
		"node-with-signer-link": func(s *api.StacksChainstateSnapshotSpec) { s.Participants[1].Signer = "signer-01" },
		"node-registry": func(s *api.StacksChainstateSnapshotSpec) {
			s.Participants[1].RegistryThreshold = ptr.To(int32(2))
		},
		"node-registry-signers": func(s *api.StacksChainstateSnapshotSpec) {
			s.Participants[1].RegistrySigners = []string{s.Participants[1].Accounts[0].Address}
		},
		"duplicate-account": func(s *api.StacksChainstateSnapshotSpec) {
			s.Participants[1].Accounts = append(s.Participants[1].Accounts, s.Participants[1].Accounts[0])
		},
		"unknown-role": func(s *api.StacksChainstateSnapshotSpec) { s.Participants[1].Accounts[0].Role = "Miner" },
	} {
		invalid := valid(name)
		mutate(&invalid.Spec)
		if err := c.Create(ctx, invalid, client.FieldValidation("Strict")); !apierrors.IsInvalid(err) {
			t.Fatalf("invalid snapshot %s accepted: %v", name, err)
		}
	}
	// Restore is selected only at creation: genesis is immutable as a whole.
	network := &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: namespace.Name},
		Spec: api.StacksNetworkSpec{
			Operation: "Paused",
			Genesis: &api.GenesisInput{
				Restore: &api.GenesisRestore{SnapshotRef: common.NameRef{Name: snapshot.Name}},
			},
		},
	}
	if err := c.Create(ctx, network, client.FieldValidation("Strict")); err != nil {
		t.Fatalf("restoring network rejected: %v", err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(network), network); err != nil {
		t.Fatal(err)
	}
	if network.Spec.Genesis.Restore == nil || network.Spec.Genesis.Restore.SnapshotRef.Name != snapshot.Name {
		t.Fatalf("restore selection not preserved: %+v", network.Spec.Genesis)
	}
	network.Spec.Genesis.Restore = nil
	if err := c.Update(
		ctx,
		network,
	); !apierrors.IsInvalid(err) ||
		!strings.Contains(err.Error(), "genesis is immutable") {
		t.Fatalf("restore removal accepted: %v", err)
	}
}

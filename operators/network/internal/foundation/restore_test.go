package foundation

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// restoreFinalCeiling is the fixture's final initialization ceiling, below the snapshot's tip.
const restoreFinalCeiling = 199

// account binds a per-network account named for its address, as a compiled requirement does.
func account(address string) api.PublicAccount {
	return api.PublicAccount{
		Binding:  common.Binding{Kind: stacks.KindStacksAccount, Name: "account-" + address, UID: "per-network"},
		Identity: common.PublicIdentity{Address: address, PublicKey: "02" + strings.Repeat("a", 64)},
	}
}

func accountRef(address string) *common.NameRef { return &common.NameRef{Name: "account-" + address} }

// restoreFixture returns a cohort and the snapshot its own projection would produce.
func restoreFixture() (map[string]restoreMember, *api.StacksChainstateSnapshot) {
	image := func(name string) common.ActorFields { return common.ActorFields{Image: ptr.To(name)} }
	cohort := map[string]restoreMember{
		"btc-01": {
			requirement:   api.BootstrapRequirement{Kind: api.ParticipantBitcoinNode},
			configuration: api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{ActorFields: image("bitcoin:1")}},
		},
		"node-01": {
			requirement: api.BootstrapRequirement{
				Kind:          api.ParticipantStacksNode,
				MiningEnabled: ptr.To(true),
				Accounts:      []api.PublicAccount{account("ST1A")},
			},
			configuration: api.Configuration{StacksNode: &stacks.StacksNodeSpec{
				ActorFields:        image("stacks:1"),
				IdentityAccountRef: accountRef("ST1A"),
				BitcoinNodeRef:     &common.NameRef{Name: "btc-01"},
			}},
		},
		"stacker-01": {
			requirement: api.BootstrapRequirement{
				Kind:           api.ParticipantStacksStacker,
				AmountMicroSTX: ptr.To(common.Amount("1000")),
				LockCycles:     ptr.To(int32(12)),
				Accounts:       []api.PublicAccount{account("ST3C"), account("ST4D")},
			},
			configuration: api.Configuration{StacksStacker: &stacks.StacksStackerSpec{
				HolderAccountRef:        accountRef("ST3C"),
				AdministratorAccountRef: accountRef("ST4D"),
				SignerRef:               &common.NameRef{Name: "signer-01"},
			}},
		},
		"sbtc": {
			requirement: api.BootstrapRequirement{
				Kind:     api.ParticipantStacksContractSet,
				Accounts: []api.PublicAccount{account("ST5E"), account("ST6F"), account("ST7G"), account("ST8H")},
			},
			configuration: api.Configuration{StacksContractSet: &stacks.StacksContractSetSpec{
				DeployerAccountRef: accountRef("ST5E"),
				Initialization: &stacks.RegistryInitialization{
					Mode:                   "ExplicitTestRegistry",
					SignerAccountRefs:      []common.NameRef{*accountRef("ST6F"), *accountRef("ST7G")},
					AggregateKeyAccountRef: *accountRef("ST8H"),
					Threshold:              2,
				},
			}},
		},
		"traffic": {
			requirement: api.BootstrapRequirement{
				Kind:     api.ParticipantStacksTransactionProduction,
				Accounts: []api.PublicAccount{account("ST9J")},
			},
			configuration: api.Configuration{StacksTransactionProduction: &stacks.StacksTransactionProductionSpec{
				AccountRef: accountRef("ST9J"),
			}},
		},
	}
	snapshot := &api.StacksChainstateSnapshot{
		ObjectMeta: metav1.ObjectMeta{Name: "seed"},
		Spec: api.StacksChainstateSnapshotSpec{
			Image:         "snapshot:1",
			GenesisDigest: "sha256:chain",
			Bitcoin:       api.SnapshotBitcoinTip{Height: 205, BlockHash: strings.Repeat("b", 64)},
		},
	}
	for _, name := range []string{"btc-01", "node-01", "sbtc", "stacker-01", "traffic"} {
		p := snapshotParticipant(name, cohort[name])
		if image := actorImage(cohort[name].configuration); image != "" {
			p.Archive = &api.SnapshotArchive{Path: name + ".tar.gz", Image: image}
		}
		snapshot.Spec.Participants = append(snapshot.Spec.Participants, p)
	}
	return cohort, snapshot
}

// validate runs validateRestore against the fixture's final ceiling.
func validate(
	snapshot *api.StacksChainstateSnapshot,
	cohort map[string]restoreMember,
	policy api.StacksImagePolicy,
) (*api.RestoredChainstate, map[string]api.SnapshotArchive, error) {
	return validateRestore(snapshot, "sha256:chain", restoreFinalCeiling, cohort, policy)
}

func TestSnapshotParticipantProjectsRolesAndLinks(t *testing.T) {
	cohort, _ := restoreFixture()
	stacker := snapshotParticipant("stacker-01", cohort["stacker-01"])
	holder, admin := account("ST3C").Identity, account("ST4D").Identity
	want := []api.SnapshotAccount{
		{Role: api.SnapshotAccountRoleAdministrator, PublicIdentity: admin},
		{Role: api.SnapshotAccountRoleHolder, PublicIdentity: holder},
	}
	if !reflect.DeepEqual(stacker.Accounts, want) || stacker.Signer != "signer-01" ||
		stacker.AmountMicroSTX == nil || stacker.MiningEnabled != nil || stacker.Archive != nil {
		t.Fatalf("stacker projection: %+v", stacker)
	}
	contracts := snapshotParticipant("sbtc", cohort["sbtc"])
	if len(contracts.Accounts) != 4 || !ptr.Equal(contracts.RegistryThreshold, ptr.To(int32(2))) ||
		!reflect.DeepEqual(contracts.RegistrySigners, []string{"ST6F", "ST7G"}) {
		t.Fatalf("contract set projection: %+v", contracts)
	}
	node := snapshotParticipant("node-01", cohort["node-01"])
	if node.BitcoinNode != "btc-01" || len(node.Accounts) != 1 ||
		node.Accounts[0].Role != api.SnapshotAccountRoleIdentity {
		t.Fatalf("node projection: %+v", node)
	}
	// Worker accounts may change and are covered by the genesis digest when funded.
	if traffic := snapshotParticipant("traffic", cohort["traffic"]); len(traffic.Accounts) != 0 {
		t.Fatalf("worker accounts projected: %+v", traffic)
	}
}

func TestValidateRestoreAcceptsMatchingCohort(t *testing.T) {
	cohort, snapshot := restoreFixture()
	restored, archives, err := validate(snapshot, cohort, "")
	if err != nil {
		t.Fatal(err)
	}
	// The genesis freezes the image, the Bitcoin tip and exactly the actors' archives.
	if restored.Image != "snapshot:1" || restored.Bitcoin != snapshot.Spec.Bitcoin || restored.ImageChanges != nil ||
		len(archives) != 2 || archives["btc-01"].Path != "btc-01.tar.gz" ||
		archives["node-01"].Path != "node-01.tar.gz" {
		t.Fatalf("frozen restore facts: %+v %+v", restored, archives)
	}
	// Per-network bindings, account order, key case, worker policy and worker accounts are outside
	// the comparison.
	member := cohort["stacker-01"]
	member.requirement.Accounts = []api.PublicAccount{member.requirement.Accounts[1], member.requirement.Accounts[0]}
	member.requirement.Accounts[0].Binding.UID = "other-network"
	member.requirement.PolicyDigest = "sha256:other-resources"
	cohort["stacker-01"] = member
	snapshot.Spec.Participants[3].Accounts[0].PublicKey = strings.ToUpper(
		snapshot.Spec.Participants[3].Accounts[0].PublicKey,
	)
	traffic := cohort["traffic"]
	traffic.requirement.Accounts = []api.PublicAccount{account("ST0K")}
	traffic.configuration.StacksTransactionProduction.AccountRef = accountRef("ST0K")
	cohort["traffic"] = traffic
	if _, _, err := validate(snapshot, cohort, ""); err != nil {
		t.Fatalf("non-consensus differences rejected: %v", err)
	}
}

func TestValidateRestoreReportsEveryDifference(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(map[string]restoreMember, *api.StacksChainstateSnapshot)
		want   string
	}{
		"genesis": {
			func(_ map[string]restoreMember, s *api.StacksChainstateSnapshot) {
				s.Spec.GenesisDigest = "sha256:other"
			},
			"genesis digest sha256:chain differs from sha256:other",
		},
		"missing": {
			func(c map[string]restoreMember, _ *api.StacksChainstateSnapshot) { delete(c, "stacker-01") },
			"StacksStacker stacker-01 is missing from the initial cohort",
		},
		"extra": {
			func(c map[string]restoreMember, _ *api.StacksChainstateSnapshot) {
				c["node-02"] = restoreMember{requirement: api.BootstrapRequirement{Kind: api.ParticipantStacksNode}}
			},
			"StacksNode node-02 is not in the snapshot; add it after initialization",
		},
		"kind": {
			func(c map[string]restoreMember, _ *api.StacksChainstateSnapshot) {
				m := c["btc-01"]
				m.requirement.Kind = api.ParticipantStacksNode
				c["btc-01"] = m
			},
			"btc-01 is StacksNode, not BitcoinNode",
		},
		"identity": {
			func(c map[string]restoreMember, _ *api.StacksChainstateSnapshot) {
				m := c["stacker-01"]
				m.requirement.Accounts[0] = account("ST9Z")
				m.configuration.StacksStacker = m.configuration.StacksStacker.DeepCopy()
				m.configuration.StacksStacker.HolderAccountRef = accountRef("ST9Z")
				c["stacker-01"] = m
			},
			"stacker-01 public identities or their roles differ",
		},
		// The same two funded accounts with the holder and administrator swapped keep the genesis
		// digest, but the chain holds the lock under the original holder.
		"swapped roles": {
			func(c map[string]restoreMember, _ *api.StacksChainstateSnapshot) {
				m := c["stacker-01"]
				s := m.configuration.StacksStacker.DeepCopy()
				s.HolderAccountRef, s.AdministratorAccountRef = s.AdministratorAccountRef, s.HolderAccountRef
				m.configuration.StacksStacker = s
				c["stacker-01"] = m
			},
			"stacker-01 public identities or their roles differ",
		},
		"link": {
			func(c map[string]restoreMember, _ *api.StacksChainstateSnapshot) {
				m := c["stacker-01"]
				m.configuration.StacksStacker = m.configuration.StacksStacker.DeepCopy()
				m.configuration.StacksStacker.SignerRef = &common.NameRef{Name: "signer-02"}
				c["stacker-01"] = m
			},
			"stacker-01 links to other participants differ",
		},
		"registry": {
			func(c map[string]restoreMember, _ *api.StacksChainstateSnapshot) {
				m := c["sbtc"]
				m.configuration.StacksContractSet = m.configuration.StacksContractSet.DeepCopy()
				m.configuration.StacksContractSet.Initialization.Threshold = 1
				c["sbtc"] = m
			},
			"sbtc registry threshold differs",
		},
		// The same signers in another order keep the accounts and the genesis digest, but the
		// registry holds the original ordered signer set and the principal derived from it.
		"swapped registry signers": {
			func(c map[string]restoreMember, _ *api.StacksChainstateSnapshot) {
				m := c["sbtc"]
				m.configuration.StacksContractSet = m.configuration.StacksContractSet.DeepCopy()
				refs := m.configuration.StacksContractSet.Initialization.SignerAccountRefs
				refs[0], refs[1] = refs[1], refs[0]
				c["sbtc"] = m
			},
			"sbtc registry signer order differs",
		},
		// A snapshot without the order (captured before it was recorded) is refused, not trusted.
		"registry order not recorded": {
			func(_ map[string]restoreMember, s *api.StacksChainstateSnapshot) {
				s.Spec.Participants[2].RegistrySigners = nil
			},
			"sbtc registry signer order differs",
		},
		// Restore completes every gate, so a snapshot of a network still bootstrapping is refused.
		"before the final ceiling": {
			func(_ map[string]restoreMember, s *api.StacksChainstateSnapshot) {
				s.Spec.Bitcoin.Height = restoreFinalCeiling - 1
			},
			"Bitcoin height 198 is below the final initialization ceiling 199; capture an Initialized network",
		},
		"mining": {
			func(c map[string]restoreMember, _ *api.StacksChainstateSnapshot) {
				m := c["node-01"]
				m.requirement.MiningEnabled = ptr.To(false)
				c["node-01"] = m
			},
			"node-01 mining role differs",
		},
		"stake": {
			func(c map[string]restoreMember, _ *api.StacksChainstateSnapshot) {
				m := c["stacker-01"]
				m.requirement.LockCycles = ptr.To(int32(6))
				c["stacker-01"] = m
			},
			"stacker-01 stake differs",
		},
		"image": {
			func(c map[string]restoreMember, _ *api.StacksChainstateSnapshot) {
				c["node-01"] = withImage(c["node-01"], "stacks:2")
			},
			"node-01 image stacks:2 differs from stacks:1, which wrote the data " +
				"(stacksImagePolicy AllowDifferent accepts another Stacks image)",
		},
	} {
		t.Run(name, func(t *testing.T) {
			cohort, snapshot := restoreFixture()
			tc.mutate(cohort, snapshot)
			_, _, err := validate(snapshot, cohort, api.StacksImagePolicyExact)
			var mismatch *restoreMismatchError
			if !errors.As(err, &mismatch) || !strings.Contains(err.Error(), tc.want) ||
				!strings.HasPrefix(err.Error(), "snapshot seed does not match: ") {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

// withImage changes only an actor member's image, keeping its roles and links.
func withImage(m restoreMember, image string) restoreMember {
	c := m.configuration.DeepCopy()
	switch {
	case c.StacksNode != nil:
		c.StacksNode.Image = ptr.To(image)
	case c.BitcoinNode != nil:
		c.BitcoinNode.Image = ptr.To(image)
	}
	m.configuration = *c
	return m
}

func TestValidateRestoreStacksImagePolicy(t *testing.T) {
	withImages := func(stacksImage, bitcoinImage string) map[string]restoreMember {
		cohort, _ := restoreFixture()
		cohort["node-01"] = withImage(cohort["node-01"], stacksImage)
		cohort["btc-01"] = withImage(cohort["btc-01"], bitcoinImage)
		return cohort
	}
	_, snapshot := restoreFixture()

	restored, _, err := validate(snapshot, withImages("stacks:2", "bitcoin:1"), api.StacksImagePolicyAllowDifferent)
	want := api.RestoredImageChange{Participant: "node-01", SnapshotImage: "stacks:1", Image: "stacks:2"}
	if err != nil || len(restored.ImageChanges) != 1 || restored.ImageChanges[0] != want {
		t.Fatalf("AllowDifferent: %v %+v", err, restored)
	}

	restored, _, err = validate(snapshot, withImages("stacks:1", "bitcoin:1"), api.StacksImagePolicyAllowDifferent)
	if err != nil || restored.ImageChanges != nil {
		t.Fatalf("unchanged images recorded: %v %+v", err, restored)
	}

	// Bitcoin node images must match under every policy.
	_, _, err = validate(snapshot, withImages("stacks:2", "bitcoin:2"), api.StacksImagePolicyAllowDifferent)
	// The Bitcoin problem carries no AllowDifferent hint, and the allowed Stacks change is no problem.
	const bitcoinProblem = "btc-01 image bitcoin:2 differs from bitcoin:1, which wrote the data"
	if err == nil || !strings.HasSuffix(err.Error(), ": "+bitcoinProblem) || strings.Contains(err.Error(), "node-01") {
		t.Fatalf("Bitcoin image change: %v", err)
	}

	// Unknown policy values behave as Exact.
	for _, policy := range []api.StacksImagePolicy{"", api.StacksImagePolicyExact, "Other"} {
		_, _, err = validate(snapshot, withImages("stacks:2", "bitcoin:1"), policy)
		if err == nil {
			t.Fatalf("policy %q accepted a Stacks image change", policy)
		}
	}
}

func TestRestoreMismatchMessageIsBounded(t *testing.T) {
	err := &restoreMismatchError{snapshot: "seed", problems: strings.Split("a b c d e f g h i j", " ")}
	if got := err.Error(); got != "snapshot seed does not match: a; b; c; d; e; f; g; h; and 2 more" {
		t.Fatalf("message: %s", got)
	}
}

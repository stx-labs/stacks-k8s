package main

import (
	"reflect"
	"strings"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func identity(n byte) common.PublicIdentity {
	return common.PublicIdentity{
		Address:   "ST" + strings.Repeat(string('A'+n), 30),
		PublicKey: "02" + strings.Repeat("0", 62) + string('0'+n) + "0",
	}
}

func account(name string, key *common.KeySource) stacks.StacksAccount {
	return stacks.StacksAccount{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name + "-uid")},
		Spec:       stacks.StacksAccountSpec{Key: key},
	}
}

var imported = &common.KeySource{SecretRef: &common.SecretKeyRef{Name: "keys", Key: "k"}}

func participant(name string, kind api.ParticipantKind, config api.Configuration) api.StacksNetworkParticipant {
	return api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{Name: "p-" + name, UID: types.UID(name + "-uid")},
		Spec:       api.StacksNetworkParticipantSpec{NetworkUID: networkUID, ParticipantName: name, Kind: kind},
		Status:     api.ParticipantStatus{Admission: &api.Admission{Configuration: config}},
	}
}

// bound binds one StacksAccount by name to a public identity, as a compiled requirement does.
func bound(name string, n byte) api.PublicAccount {
	return api.PublicAccount{Binding: common.Binding{Kind: stacks.KindStacksAccount, Name: name}, Identity: identity(n)}
}

func ref(name string) *common.NameRef { return &common.NameRef{Name: name} }

func cohortFixture() []member {
	node := participant("node-01", api.ParticipantStacksNode, api.Configuration{
		StacksNode: &stacks.StacksNodeSpec{
			ActorFields:        common.ActorFields{Image: ptr.To("stacks-node:3.1")},
			IdentityAccountRef: ref("miner"),
			BitcoinNodeRef:     ref("btc-01"),
		},
	})
	signer := participant("signer-01", api.ParticipantStacksSigner, api.Configuration{
		StacksSigner: &stacks.StacksSignerSpec{
			ActorFields: common.ActorFields{Image: ptr.To("stacks-signer:3.1")},
			NodeRef:     ref("node-01"),
			AccountRef:  ref("consensus"),
		},
	})
	btc := participant("btc-01", api.ParticipantBitcoinNode, api.Configuration{
		BitcoinNode: &bitcoin.BitcoinNodeSpec{ActorFields: common.ActorFields{Image: ptr.To("bitcoind:28")}},
	})
	stacker := participant("stacker-01", api.ParticipantStacksStacker, api.Configuration{
		StacksStacker: &stacks.StacksStackerSpec{
			HolderAccountRef:        ref("holder"),
			AdministratorAccountRef: ref("admin"),
			SignerRef:               ref("signer-01"),
			AmountMicroSTX:          ptr.To(common.Amount("1000")),
			LockCycles:              ptr.To[int32](12),
		},
	})
	traffic := participant("traffic", api.ParticipantStacksTransactionProduction, api.Configuration{
		StacksTransactionProduction: &stacks.StacksTransactionProductionSpec{AccountRef: ref("sender")},
	})
	return []member{
		{
			requirement: api.BootstrapRequirement{
				Kind:          api.ParticipantStacksNode,
				MiningEnabled: ptr.To(true),
				// An account bound without a role (here "other") is not projected.
				Accounts: []api.PublicAccount{bound("miner", 2), bound("miner", 2), bound("other", 1)},
			},
			participant: node,
			accounts:    []stacks.StacksAccount{account("miner", imported), account("other", imported)},
		},
		{
			requirement: api.BootstrapRequirement{
				Kind:     api.ParticipantStacksSigner,
				Accounts: []api.PublicAccount{bound("consensus", 3)},
			},
			participant: signer,
			accounts:    []stacks.StacksAccount{account("consensus", imported)},
		},
		{requirement: api.BootstrapRequirement{Kind: api.ParticipantBitcoinNode}, participant: btc},
		{
			requirement: api.BootstrapRequirement{
				Kind:           api.ParticipantStacksStacker,
				AmountMicroSTX: ptr.To(common.Amount("1000")),
				LockCycles:     ptr.To[int32](12),
				Accounts:       []api.PublicAccount{bound("holder", 4), bound("admin", 5)},
			},
			participant: stacker,
			accounts:    []stacks.StacksAccount{account("holder", imported), account("admin", imported)},
		},
		{
			requirement: api.BootstrapRequirement{
				Kind:     api.ParticipantStacksTransactionProduction,
				Accounts: []api.PublicAccount{bound("sender", 6)},
			},
			participant: traffic,
			accounts:    []stacks.StacksAccount{account("sender", imported)},
		},
	}
}

func TestProjectRecordsWhatRestoreValidates(t *testing.T) {
	got, err := project(cohortFixture())
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range got {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "btc-01,node-01,signer-01,stacker-01,traffic" {
		t.Fatalf("order %v", names)
	}
	node := got[1]
	if node.Kind != api.ParticipantStacksNode || !ptr.Deref(node.MiningEnabled, false) ||
		node.BitcoinNode != "btc-01" || len(node.Accounts) != 1 ||
		node.Accounts[0] != (api.SnapshotAccount{Role: api.SnapshotAccountRoleIdentity, PublicIdentity: identity(2)}) {
		t.Fatalf("node projection %+v", node)
	}
	if node.Archive == nil || node.Archive.Path != "node-01.tar.gz" || node.Archive.Image != "stacks-node:3.1" {
		t.Fatalf("node archive %+v", node.Archive)
	}
	if got[0].Archive.Image != "bitcoind:28" || got[2].Archive.Image != "stacks-signer:3.1" {
		t.Fatalf("actor images %+v %+v", got[0].Archive, got[2].Archive)
	}
	if signer := got[2]; signer.Node != "node-01" || len(signer.Accounts) != 1 ||
		signer.Accounts[0].Role != api.SnapshotAccountRoleSigner {
		t.Fatalf("signer projection %+v", signer)
	}
	stacker := got[3]
	wantStacker := []api.SnapshotAccount{
		{Role: api.SnapshotAccountRoleAdministrator, PublicIdentity: identity(5)},
		{Role: api.SnapshotAccountRoleHolder, PublicIdentity: identity(4)},
	}
	if stacker.Archive != nil || *stacker.AmountMicroSTX != "1000" || *stacker.LockCycles != 12 ||
		stacker.Signer != "signer-01" || len(stacker.Accounts) != 2 ||
		stacker.Accounts[0] != wantStacker[0] || stacker.Accounts[1] != wantStacker[1] {
		t.Fatalf("stacker projection %+v", stacker)
	}
	// Worker accounts are not compared on restore, so they are not recorded.
	if traffic := got[4]; len(traffic.Accounts) != 0 {
		t.Fatalf("traffic projection %+v", traffic)
	}
}

func TestProjectRecordsTheRegistry(t *testing.T) {
	sbtc := participant("sbtc", api.ParticipantStacksContractSet, api.Configuration{
		StacksContractSet: &stacks.StacksContractSetSpec{
			DeployerAccountRef: ref("deployer"),
			Initialization: &stacks.RegistryInitialization{
				SignerAccountRefs:      []common.NameRef{*ref("bridge-1"), *ref("bridge-2")},
				AggregateKeyAccountRef: *ref("aggregate"),
				Threshold:              2,
			},
		},
	})
	req := api.BootstrapRequirement{
		Kind: api.ParticipantStacksContractSet,
		Accounts: []api.PublicAccount{
			bound("deployer", 1), bound("bridge-1", 2), bound("bridge-2", 3), bound("aggregate", 4),
		},
	}
	got, err := project([]member{{requirement: req, participant: sbtc}})
	if err != nil {
		t.Fatal(err)
	}
	roles := []string{}
	for _, a := range got[0].Accounts {
		roles = append(roles, string(a.Role))
	}
	if strings.Join(roles, ",") != "AggregateKey,Deployer,RegistrySigner,RegistrySigner" ||
		!ptr.Equal(got[0].RegistryThreshold, ptr.To[int32](2)) {
		t.Fatalf("contract set projection %+v", got[0])
	}
	// The registry order survives the account sort.
	if want := []string{identity(2).Address, identity(3).Address}; !reflect.DeepEqual(got[0].RegistrySigners, want) {
		t.Fatalf("registry signers %v, want %v", got[0].RegistrySigners, want)
	}
	sbtc.Status.Admission.Configuration.StacksContractSet.Initialization.SignerAccountRefs = []common.NameRef{
		*ref("bridge-2"),
		*ref("bridge-1"),
	}
	got, err = project([]member{{requirement: req, participant: sbtc}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{identity(3).Address, identity(2).Address}; !reflect.DeepEqual(got[0].RegistrySigners, want) {
		t.Fatalf("swapped registry signers %v, want %v", got[0].RegistrySigners, want)
	}
}

func TestProjectRefusesGeneratedKeys(t *testing.T) {
	for name, key := range map[string]*common.KeySource{
		"omitted":   nil,
		"generated": {Generate: ptr.To(true)},
	} {
		t.Run(name, func(t *testing.T) {
			members := cohortFixture()
			members[0].accounts[1] = account("other", key)
			members[1].accounts = []stacks.StacksAccount{account("other", key), account("signer", key)}
			_, err := project(members)
			if err == nil || !strings.Contains(err.Error(), "accounts other, signer use generated keys") {
				t.Fatalf("got %v", err)
			}
		})
	}
	members := cohortFixture()
	members[0].accounts[1] = account("other", &common.KeySource{PublicIdentity: ptr.To(identity(1))})
	if _, err := project(members); err != nil {
		t.Fatalf("public identity refused: %v", err)
	}
}

func TestProjectRequiresAdmittedActorImages(t *testing.T) {
	members := cohortFixture()
	members[2].participant.Status.Admission.Configuration.BitcoinNode.Image = nil
	if _, err := project(members); err == nil || !strings.Contains(err.Error(), "btc-01 has no admitted actor image") {
		t.Fatalf("got %v", err)
	}
	members = cohortFixture()
	members[2].participant.Status.Admission = nil
	_, err := project(members)
	if err == nil || !strings.Contains(err.Error(), "btc-01 has no admitted configuration") {
		t.Fatalf("got %v", err)
	}
}

func TestProjectRefusesEphemeralActorStorage(t *testing.T) {
	members := cohortFixture()
	members[0].participant.Status.Admission.Configuration.StacksNode.Storage = &common.Storage{Ephemeral: ptr.To(true)}
	_, err := project(members)
	if err == nil || !strings.Contains(err.Error(), "node-01 uses ephemeral storage") {
		t.Fatalf("got %v", err)
	}
	members[0].participant.Status.Admission.Configuration.StacksNode.Storage.Ephemeral = ptr.To(false)
	if _, err = project(members); err != nil {
		t.Fatalf("persistent storage refused: %v", err)
	}
}

// selecting returns a network that selects the members, each with its allocated identity.
func selecting(members []member) *api.StacksNetwork {
	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", UID: networkUID}}
	for _, m := range members {
		name := m.participant.Spec.ParticipantName
		root.Spec.Participants = append(root.Spec.Participants, api.Participant{Name: name, Kind: m.requirement.Kind})
		root.Status.Identities = append(root.Status.Identities,
			api.InstanceIdentity{Name: name, UID: m.participant.UID})
	}
	return root
}

func TestLateConsensusRefusesConsensusParticipantsOutsideTheCohort(t *testing.T) {
	members := cohortFixture()
	if err := lateConsensus(selecting(members), members); err != nil {
		t.Fatalf("initial cohort refused: %v", err)
	}
	join := func(root *api.StacksNetwork, name string, kind api.ParticipantKind) {
		root.Spec.Participants = append(root.Spec.Participants, api.Participant{Name: name, Kind: kind})
		root.Status.Identities = append(root.Status.Identities,
			api.InstanceIdentity{Name: name, UID: types.UID(name + "-uid")})
	}
	root := selecting(members)
	join(root, "node-02", api.ParticipantStacksNode)
	if err := lateConsensus(root, members); err != nil {
		t.Fatalf("a late plain node refused: %v", err)
	}
	for _, kind := range []api.ParticipantKind{api.ParticipantStacksSigner, api.ParticipantStacksStacker} {
		root = selecting(members)
		join(root, "late-01", kind)
		err := lateConsensus(root, members)
		if err == nil || !strings.Contains(err.Error(), "signers or stackers late-01 joined after initialization") {
			t.Fatalf("%s: got %v", kind, err)
		}
	}
	// A removed participant's kind is gone, and its locked stake may still count.
	root = selecting(members)
	root.Status.Identities = append(root.Status.Identities, api.InstanceIdentity{Name: "gone-01", UID: "gone-uid"})
	err := lateConsensus(root, members)
	if err == nil || !strings.Contains(err.Error(), "participants gone-01 were removed after initialization") {
		t.Fatalf("got %v", err)
	}
}

// Stake is mutable after initialization, but a snapshot records the stake frozen in the genesis.
func TestInitialStakeRefusesStakeChangedAfterInitialization(t *testing.T) {
	if err := initialStake(cohortFixture()); err != nil {
		t.Fatalf("initial stake refused: %v", err)
	}
	for name, tc := range map[string]struct {
		change func(*stacks.StacksStackerSpec)
		want   string
	}{
		"amount": {
			change: func(s *stacks.StacksStackerSpec) { s.AmountMicroSTX = ptr.To(common.Amount("2000")) },
			want:   "stacker-01 (initial 1000 micro-STX for 12 cycles, now 2000 micro-STX for 12 cycles)",
		},
		"lock cycles": {
			change: func(s *stacks.StacksStackerSpec) { s.LockCycles = ptr.To[int32](6) },
			want:   "stacker-01 (initial 1000 micro-STX for 12 cycles, now 1000 micro-STX for 6 cycles)",
		},
		// Renewal timing is not recorded in a snapshot.
		"renewal threshold": {change: func(s *stacks.StacksStackerSpec) { s.RenewWhenRemainingCycles = ptr.To[int32](3) }},
	} {
		members := cohortFixture()
		for i := range members {
			if s := members[i].participant.Status.Admission.Configuration.StacksStacker; s != nil {
				tc.change(s)
			}
		}
		err := initialStake(members)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Fatalf("%s: got %v, want %q", name, err, tc.want)
		}
	}
}

func TestAccountBindingsSelectsDistinctStacksAccounts(t *testing.T) {
	req := api.BootstrapRequirement{Accounts: []api.PublicAccount{
		{Binding: common.Binding{Kind: stacks.KindStacksAccount, Name: "a"}},
		{Binding: common.Binding{Kind: "Other", Name: "b"}},
		{Binding: common.Binding{Kind: stacks.KindStacksAccount, Name: "a"}},
	}}
	if got := accountBindings(req); len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("got %v", got)
	}
}

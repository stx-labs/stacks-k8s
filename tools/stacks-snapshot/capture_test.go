package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
)

func at(s int) time.Time { return time.Unix(1_800_000_000+int64(s), 0) }

func views(s int, btc, stx string) []observation {
	return []observation{
		{source: "bitcoin/btc-01", observedAt: at(s), tip: btc},
		{source: "bitcoin/btc-02", observedAt: at(s + 1), tip: btc},
		{source: "stacks/node-01", observedAt: at(s), tip: stx},
	}
}

func TestSettlerWaitsForRepeatedFreshAgreeingTips(t *testing.T) {
	var s settler
	steps := []struct {
		seen  []observation
		still bool
		why   string
	}{
		{views(0, "100/a", "x"), false, ""},
		{views(0, "100/a", "x"), false, ""}, // not refreshed: does not count
		{views(5, "100/a", "y"), false, ""}, // Stacks moved: start over
		{views(10, "100/a", "y"), false, ""},
		{views(15, "100/a", "y"), true, ""},
	}
	for i, st := range steps {
		still, why := s.step(st.seen)
		if still != st.still || why != st.why {
			t.Fatalf("step %d: still=%v why=%q", i, still, why)
		}
	}
}

func TestSettlerRequiresAgreement(t *testing.T) {
	var s settler
	seen := views(0, "100/a", "x")
	seen[1].tip = "99/b"
	if still, why := s.step(seen); still || why != "bitcoin nodes disagree on the tip" {
		t.Fatalf("still=%v why=%q", still, why)
	}
	if s.last != nil || s.rounds != 0 {
		t.Fatal("disagreement did not reset the settler")
	}
}

func TestManifestIsApplicableAndCarriesNoServerFields(t *testing.T) {
	participants, err := project(cohortFixture())
	if err != nil {
		t.Fatal(err)
	}
	for i := range participants {
		if participants[i].Archive != nil {
			participants[i].Archive.SHA256 = strings.Repeat("a", 64)
		}
	}
	btc := api.SnapshotBitcoinTip{Height: 217, BlockHash: strings.Repeat("b", 64)}
	stx := api.SnapshotStacksTip{
		Height: 126, BlockHash: strings.Repeat("c", 64), IndexBlockID: strings.Repeat("d", 64), RewardCycle: 10,
	}
	data, err := manifest("epoch40", "snapshots:v1", "sha256:"+strings.Repeat("e", 64), participants, btc, stx)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"status:", "namespace:", "creationTimestamp", "uid:"} {
		if strings.Contains(string(data), field) {
			t.Fatalf("manifest contains %s:\n%s", field, data)
		}
	}
	var got api.StacksChainstateSnapshot
	if err = yaml.UnmarshalStrict(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.APIVersion != "network.stacks.org/v1alpha2" || got.Kind != "StacksChainstateSnapshot" ||
		got.Name != "epoch40" || got.Spec.Image != "snapshots:v1" || got.Spec.Bitcoin != btc ||
		got.Spec.Stacks != stx || len(got.Spec.Participants) != 5 ||
		got.Spec.Participants[1].Archive.SHA256 != strings.Repeat("a", 64) ||
		!reflect.DeepEqual(got.Spec.Participants[3].Accounts, participants[3].Accounts) ||
		got.Spec.Participants[3].Signer != "signer-01" {
		t.Fatalf("round trip %+v", got)
	}
}

func TestCheckCapturable(t *testing.T) {
	ready := func() *api.StacksNetwork {
		return &api.StacksNetwork{Status: api.StacksNetworkStatus{
			Conditions:    []metav1.Condition{{Type: api.ConditionInitialized, Status: metav1.ConditionTrue}},
			GenesisRef:    &common.Binding{Name: "genesis"},
			GenesisDigest: "sha256:x",
			Bitcoin:       &api.BitcoinRuntimeStatus{ExecutionRefs: []common.Binding{{Name: "e"}}},
		}}
	}
	if err := checkCapturable(ready()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*api.StacksNetwork){
		"stopped":         func(n *api.StacksNetwork) { n.Spec.Operation = api.NetworkOperationStopped },
		"not initialized": func(n *api.StacksNetwork) { n.Status.Conditions[0].Status = metav1.ConditionFalse },
		"no genesis":      func(n *api.StacksNetwork) { n.Status.GenesisRef = nil },
		"no executions":   func(n *api.StacksNetwork) { n.Status.Bitcoin = nil },
	} {
		root := ready()
		mutate(root)
		if checkCapturable(root) == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func unstructuredOf(t *testing.T, obj runtime.Object, gvk schema.GroupVersionKind) *unstructured.Unstructured {
	t.Helper()
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		t.Fatal(err)
	}
	u := &unstructured.Unstructured{Object: m}
	u.SetGroupVersionKind(gvk)
	u.SetNamespace("lab")
	return u
}

// fakeSource builds a frozen network with the cohort fixture and its status observations.
func fakeSource(t *testing.T, mutate func(objects []*unstructured.Unstructured)) (*cluster, *api.StacksNetwork) {
	t.Helper()
	members := cohortFixture()
	root := selecting(members)
	root.Status.GenesisRef = &common.Binding{Name: "genesis", UID: "genesis-uid"}
	root.Status.GenesisDigest = "sha256:" + strings.Repeat("e", 64)
	root.Status.Bitcoin = &api.BitcoinRuntimeStatus{ExecutionRefs: []common.Binding{
		{Name: "exec-btc-01", UID: "exec-uid"},
	}}
	genesis := &api.StacksGenesis{ObjectMeta: metav1.ObjectMeta{Name: "genesis", UID: "genesis-uid"}}
	objects := []*unstructured.Unstructured{
		unstructuredOf(t, root, api.GroupVersion.WithKind(api.KindStacksNetwork)),
	}
	for i := range members {
		m := &members[i]
		m.requirement.Participant = common.Binding{Name: m.participant.Name, UID: m.participant.UID}
		if m.participant.Spec.Kind == api.ParticipantStacksNode {
			m.participant.Status.Runtime = &api.ParticipantRuntimeStatus{Protocol: &api.StacksProtocolObservation{
				ObservedAt: metav1.NewTime(at(0)), StacksHeight: 126, StacksTip: "tip",
				IndexBlockID: "index", RewardCycle: 10,
			}}
		}
		genesis.Spec.Bootstrap.Requirements = append(genesis.Spec.Bootstrap.Requirements, m.requirement)
		objects = append(objects, unstructuredOf(t, &m.participant,
			api.GroupVersion.WithKind(api.KindStacksNetworkParticipant)))
		for j := range m.accounts {
			objects = append(objects, unstructuredOf(t, &m.accounts[j],
				stacks.GroupVersion.WithKind(stacks.KindStacksAccount)))
		}
	}
	// Account bindings carry UIDs in the frozen genesis.
	for i := range genesis.Spec.Bootstrap.Requirements {
		for j := range genesis.Spec.Bootstrap.Requirements[i].Accounts {
			b := &genesis.Spec.Bootstrap.Requirements[i].Accounts[j].Binding
			b.UID = types.UID(b.Name + "-uid")
		}
	}
	execution := &bitcoin.BitcoinExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-btc-01", UID: "exec-uid"},
		Spec:       bitcoin.BitcoinExecutionSpec{Participant: common.Binding{Name: "p-btc-01", UID: "btc-01-uid"}},
		Status: bitcoin.BitcoinExecutionStatus{Observation: &bitcoin.BitcoinObservation{
			Height: 217, Tip: "btc-tip", ObservedAt: metav1.NewTime(at(0)),
		}},
	}
	objects = append(objects,
		unstructuredOf(t, genesis, api.GroupVersion.WithKind(api.KindStacksGenesis)),
		unstructuredOf(t, execution, bitcoin.GroupVersion.WithKind(bitcoin.KindBitcoinExecution)),
	)
	if mutate != nil {
		mutate(objects)
	}
	// Create each object under its real plural; the fake would guess "stacksgenesises".
	resources := map[string]schema.GroupVersionResource{
		api.KindStacksNetwork:            networkResource,
		api.KindStacksNetworkParticipant: participantResource,
		api.KindStacksGenesis:            genesisResource,
		stacks.KindStacksAccount:         accountResource,
		bitcoin.KindBitcoinExecution:     executionResource,
	}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	for _, u := range objects {
		_, err := client.Resource(resources[u.GetKind()]).Namespace("lab").
			Create(context.Background(), u, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}
	return &cluster{dynamic: client, namespace: "lab"}, root
}

func TestClusterReadsTheFrozenCohortAndObservations(t *testing.T) {
	c, root := fakeSource(t, nil)
	ctx := context.Background()
	members, err := c.cohort(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 5 || len(members[0].accounts) != 2 {
		t.Fatalf("cohort %+v", members)
	}
	// Roles come from each participant's admitted configuration read from the cluster.
	projected, err := project(members)
	if err != nil {
		t.Fatal(err)
	}
	if stacker := projected[3]; stacker.Name != "stacker-01" || len(stacker.Accounts) != 2 ||
		stacker.Signer != "signer-01" {
		t.Fatalf("stacker projected from the cluster: %+v", stacker)
	}
	btc, seen, err := c.bitcoinView(ctx, root)
	if err != nil || btc.Height != 217 || btc.BlockHash != "btc-tip" || len(seen) != 1 {
		t.Fatalf("bitcoin %+v %v %v", btc, seen, err)
	}
	stx, seen, err := c.stacksView(ctx, members)
	if err != nil || stx.Height != 126 || stx.IndexBlockID != "index" || stx.BlockHash != "tip" ||
		stx.RewardCycle != 10 || len(seen) != 1 {
		t.Fatalf("stacks %+v %v %v", stx, seen, err)
	}
}

// The network keeps the execution record of a removed Bitcoin node; its frozen observation must
// neither lower the tip nor keep the chain from settling.
func TestBitcoinViewSkipsRemovedNodes(t *testing.T) {
	removed := &bitcoin.BitcoinExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-btc-02", UID: "exec-02-uid"},
		Spec:       bitcoin.BitcoinExecutionSpec{Participant: common.Binding{Name: "p-btc-02", UID: "btc-02-uid"}},
		Status: bitcoin.BitcoinExecutionStatus{Observation: &bitcoin.BitcoinObservation{
			Height: 150, Tip: "old-tip", ObservedAt: metav1.NewTime(at(-600)),
		}},
	}
	c, root := fakeSource(t, nil)
	_, err := c.dynamic.Resource(executionResource).Namespace("lab").Create(context.Background(),
		unstructuredOf(t, removed, bitcoin.GroupVersion.WithKind(bitcoin.KindBitcoinExecution)),
		metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	root.Status.Bitcoin.ExecutionRefs = append(root.Status.Bitcoin.ExecutionRefs,
		common.Binding{Name: "exec-btc-02", UID: "exec-02-uid"})
	root.Status.Identities = append(root.Status.Identities, api.InstanceIdentity{Name: "btc-02", UID: "btc-02-uid"})
	btc, seen, err := c.bitcoinView(context.Background(), root)
	if err != nil || btc.Height != 217 || len(seen) != 1 {
		t.Fatalf("bitcoin %+v %v %v", btc, seen, err)
	}
	// Still selected but being removed: skipped as well.
	root.Spec.Participants = append(root.Spec.Participants, api.Participant{Name: "btc-02"})
	root.Status.Identities[len(root.Status.Identities)-1].Removing = true
	if btc, seen, err = c.bitcoinView(context.Background(), root); err != nil || btc.Height != 217 || len(seen) != 1 {
		t.Fatalf("removing node counted: %+v %v %v", btc, seen, err)
	}
	// Once it is selected again under a live identity it counts, and its lower tip wins.
	root.Status.Identities[len(root.Status.Identities)-1].Removing = false
	if btc, seen, err = c.bitcoinView(context.Background(), root); err != nil || btc.Height != 150 || len(seen) != 2 {
		t.Fatalf("selected node skipped: %+v %v %v", btc, seen, err)
	}
}

func TestImagesUnchangedRefusesAnImageChangedDuringTheCapture(t *testing.T) {
	ctx := context.Background()
	c, root := fakeSource(t, nil)
	members, err := c.cohort(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	participants, err := project(members)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.imagesUnchanged(ctx, members, participants); err != nil {
		t.Fatalf("unchanged images refused: %v", err)
	}
	c, root = fakeSource(t, func(objects []*unstructured.Unstructured) {
		for _, u := range objects {
			if u.GetName() == "p-node-01" {
				if err := unstructured.SetNestedField(u.Object, "stacks-node:3.2",
					"status", "admission", "configuration", "stacksNode", "image"); err != nil {
					t.Fatal(err)
				}
			}
		}
	})
	members, err = c.cohort(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	// The manifest recorded the image read before the stop.
	err = c.imagesUnchanged(ctx, members, participants)
	if err == nil || !strings.Contains(err.Error(), "node-01 (stacks-node:3.1, now stacks-node:3.2)") {
		t.Fatalf("got %v", err)
	}
}

// An account funded only by spec.genesis.allocations is bound to no cohort participant, yet its
// address is in the genesis digest, so its key must be imported too.
func TestFundedAccountsNeedImportedKeys(t *testing.T) {
	generate := &common.KeySource{Generate: ptr.To(true)}
	refused := "accounts treasury use generated keys"
	for name, tc := range map[string]struct {
		treasury *stacks.StacksAccount
		want     string
	}{
		"imported":  {treasury: ptr.To(account("treasury", imported))},
		"generated": {treasury: ptr.To(account("treasury", generate)), want: refused},
		"omitted":   {treasury: ptr.To(account("treasury", nil)), want: refused},
		"recreated": {
			treasury: &stacks.StacksAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "treasury", UID: "recreated"},
				Spec:       stacks.StacksAccountSpec{Key: imported},
			},
			want: "account treasury identity changed",
		},
		"deleted": {want: "treasury"},
	} {
		t.Run(name, func(t *testing.T) {
			c, root := fakeSource(t, func(objects []*unstructured.Unstructured) {
				for _, u := range objects {
					if u.GetKind() != api.KindStacksGenesis {
						continue
					}
					// A cohort account, the allocation-only account, and a non-account pin.
					err := unstructured.SetNestedSlice(u.Object, []any{
						map[string]any{"kind": "StacksAccount", "name": "miner", "uid": "miner-uid"},
						map[string]any{"kind": "StacksAccount", "name": "treasury", "uid": "treasury-uid"},
						map[string]any{"kind": "ChainstateSnapshot", "name": "seed", "uid": "seed-uid"},
					}, "spec", "source", "dependencies")
					if err != nil {
						t.Fatal(err)
					}
				}
			})
			ctx := context.Background()
			if tc.treasury != nil {
				_, err := c.dynamic.Resource(accountResource).Namespace("lab").Create(ctx,
					unstructuredOf(t, tc.treasury, stacks.GroupVersion.WithKind(stacks.KindStacksAccount)),
					metav1.CreateOptions{})
				if err != nil {
					t.Fatal(err)
				}
			}
			err := c.fundedKeysImported(ctx, root)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestClusterRefusesChangedIdentities(t *testing.T) {
	ctx := context.Background()
	c, root := fakeSource(t, func(objects []*unstructured.Unstructured) {
		for _, u := range objects {
			if u.GetName() == "miner" {
				u.SetUID("recreated")
			}
		}
	})
	if _, err := c.cohort(ctx, root); err == nil || !strings.Contains(err.Error(), "account miner identity changed") {
		t.Fatalf("got %v", err)
	}
	c, root = fakeSource(t, nil)
	if _, err := c.network(ctx, "87654321-1234-1234-1234-123456789abc", "network"); err == nil {
		t.Fatal("other network UID accepted")
	}
	root.Status.Bitcoin.ExecutionRefs[0].UID = "other"
	if _, _, err := c.bitcoinView(ctx, root); err == nil {
		t.Fatal("recreated execution accepted")
	}
}

// The stop records the settled tips in the same patch, so a failed export can resume. The patch
// also carries metadata.uid; the API server, not this fake client, refuses it for a recreated
// network of the same name.
func TestStopRecordsTheTipsAResumeExportsFrom(t *testing.T) {
	c, root := fakeSource(t, nil)
	ctx := context.Background()
	tips := capturedTips{
		Bitcoin: api.SnapshotBitcoinTip{Height: 217, BlockHash: strings.Repeat("b", 64)},
		Stacks: api.SnapshotStacksTip{
			Height: 126, BlockHash: strings.Repeat("c", 64), IndexBlockID: strings.Repeat("d", 64), RewardCycle: 10,
		},
	}
	if _, err := checkResumable(root); err == nil {
		t.Fatal("a running network was accepted for resume")
	}
	recorded, err := tipsAnnotation(tips)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.patchOperation(ctx, root, api.NetworkOperationStopped, recorded); err != nil {
		t.Fatal(err)
	}
	got, err := c.network(ctx, networkUID, "network")
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.Operation != api.NetworkOperationStopped || got.UID != networkUID {
		t.Fatalf("network %+v", got.ObjectMeta)
	}
	if resumed, err := checkResumable(got); err != nil || resumed != tips {
		t.Fatalf("resume tips %+v, %v", resumed, err)
	}
	for name, annotations := range map[string]map[string]string{
		"stopped by hand":  nil,
		"corrupt tips":     {captureAnnotation: `{"bitcoin":{"height":217,"blockHash":"zz"}}`},
		"not a tip record": {captureAnnotation: "yes"},
	} {
		stopped := got.DeepCopy()
		stopped.Annotations = annotations
		if _, err := checkResumable(stopped); err == nil {
			t.Fatalf("%s: resume accepted", name)
		}
	}
	if err := checkCapturable(got); err == nil || !strings.Contains(err.Error(), `"resume": true`) {
		t.Fatalf("a stopped network must point to resume: %v", err)
	}
}

func TestDockerfilePlacesReadableArchivesOnTheBase(t *testing.T) {
	got := string(dockerfile(defaultExportImage))
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "# ") ||
		lines[1] != "FROM "+defaultExportImage ||
		lines[2] != "COPY --chmod=0555 *.tar.gz /snapshot/" {
		t.Fatalf("Dockerfile:\n%s", got)
	}
}

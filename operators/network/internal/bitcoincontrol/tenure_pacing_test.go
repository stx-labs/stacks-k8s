package bitcoincontrol

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// pacedMiner is a captured miner whose fresh view holds the tenure that tip elected at height.
func pacedMiner(pod string, height uint64, tip string, now time.Time) *api.ParticipantRuntimeStatus {
	consensus := strings.Repeat("c", 40)
	uid := types.UID("uid-" + pod)
	return &api.ParticipantRuntimeStatus{
		PodRef:              &common.Binding{Kind: "Pod", Name: pod, UID: uid},
		ContainerID:         "containerd://" + pod,
		ConfigurationDigest: "sha256:" + pod,
		Protocol: &api.StacksProtocolObservation{
			Available:           true,
			PodUID:              uid,
			ContainerID:         "containerd://" + pod,
			ConfigurationDigest: "sha256:" + pod,
			GenesisUID:          "genesis-uid",
			ObservedAt:          metav1.NewTime(now),
			BurnHeight:          height,
			BurnConsensusHash:   consensus,
			Tenure: &api.StacksTenureObservation{
				ConsensusHash: consensus,
				BurnBlockHash: tip,
				BurnHeight:    height,
			},
		},
	}
}

func pacingRoot() *api.StacksNetwork {
	return &api.StacksNetwork{Status: api.StacksNetworkStatus{
		GenesisRef: &common.Binding{Kind: api.KindStacksGenesis, Name: "genesis", UID: "genesis-uid"},
	}}
}

func generated(tip string, at time.Time) *bitcoin.BitcoinRPCReceipt {
	return &bitcoin.BitcoinRPCReceipt{
		Request:    bitcoin.BitcoinArmedRPC{Method: bitcoin.RPCGenerate},
		ReceivedAt: metav1.NewTime(at),
		BlockHash:  tip,
	}
}

func TestTenurePacingWaitsForEveryMinerThenExpires(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tip := strings.Repeat("a", 64)
	root, receipt := pacingRoot(), generated(tip, now.Add(-5*time.Second))
	one, two := pacedMiner("miner-1", 260, tip, now), pacedMiner("miner-2", 260, tip, now)
	miners := []*api.ParticipantRuntimeStatus{one, two}

	if got := paceTenure(root, miners, receipt, 252, 251, tip, now); got != (pacingDecision{}) {
		t.Fatalf("before epoch 3.0: %+v", got)
	}
	if got := paceTenure(root, nil, receipt, 252, 260, tip, now); got != (pacingDecision{}) {
		t.Fatalf("without captured miners: %+v", got)
	}
	if got := paceTenure(root, miners, receipt, 252, 260, tip, now); got.outcome != bitcoin.TenurePacingProgressed {
		t.Fatalf("every miner observed the tenure: %+v", got)
	}
	two.Protocol.Tenure.BurnBlockHash = strings.Repeat("b", 64)
	got := paceTenure(root, miners, receipt, 252, 260, tip, now)
	if !got.waiting || got.outcome != "" || got.remaining != 25*time.Second {
		t.Fatalf("one miner without the tenure: %+v", got)
	}
	if got := paceTenure(root, miners, receipt, 252, 260, tip, now.Add(25*time.Second)); got.outcome !=
		bitcoin.TenurePacingExpired || got.waiting {
		t.Fatalf("after the bounded wait: %+v", got)
	}
}

func TestTenurePacingBindsToTheProducersOwnBlock(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tip := strings.Repeat("a", 64)
	root := pacingRoot()
	miners := []*api.ParticipantRuntimeStatus{pacedMiner("miner-1", 260, tip, now)}
	otherBlock := generated(strings.Repeat("d", 64), now)
	wallet := generated(tip, now)
	wallet.Request.Method = bitcoin.RPCLoadWallet
	for name, receipt := range map[string]*bitcoin.BitcoinRPCReceipt{
		"no receipt": nil, "another block": otherBlock, "not a generation": wallet,
	} {
		if got := paceTenure(root, miners, receipt, 252, 260, tip, now); got.outcome != bitcoin.TenurePacingUnbound {
			t.Fatalf("%s: %+v", name, got)
		}
	}
	for name, change := range map[string]func(*api.ParticipantRuntimeStatus){
		"stale view": func(m *api.ParticipantRuntimeStatus) {
			m.Protocol.ObservedAt = metav1.NewTime(now.Add(-time.Minute))
		},
		"replaced Pod":   func(m *api.ParticipantRuntimeStatus) { m.PodRef.UID = "replacement" },
		"other genesis":  func(m *api.ParticipantRuntimeStatus) { m.Protocol.GenesisUID = "other" },
		"unavailable":    func(m *api.ParticipantRuntimeStatus) { m.Protocol.Available = false },
		"unknown tenure": func(m *api.ParticipantRuntimeStatus) { m.Protocol.Tenure = nil },
		"same height, fork": func(m *api.ParticipantRuntimeStatus) {
			m.Protocol.Tenure.BurnBlockHash = otherBlock.BlockHash
		},
		"older tenure": func(m *api.ParticipantRuntimeStatus) { m.Protocol.Tenure.BurnHeight = 259 },
		"node behind":  func(m *api.ParticipantRuntimeStatus) { m.Protocol.BurnHeight = 259 },
		"other Bitcoin tip": func(m *api.ParticipantRuntimeStatus) {
			m.Protocol.BurnConsensusHash = strings.Repeat("e", 40)
		},
		"config replaced":     func(m *api.ParticipantRuntimeStatus) { m.ConfigurationDigest = "sha256:new" },
		"container restarted": func(m *api.ParticipantRuntimeStatus) { m.ContainerID = "containerd://new" },
	} {
		miner := pacedMiner("miner-1", 260, tip, now)
		change(miner)
		if tenureObserved(root, miner, 260, tip, now) {
			t.Fatalf("%s established tenure progress", name)
		}
	}
}

func TestTenurePacingRecordsEachBlockOnce(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	due := metav1.NewTime(now.Add(-1500 * time.Millisecond))
	record := &bitcoin.BitcoinInitialization{Status: bitcoin.BitcoinInitializationStatus{NextOpportunityAt: &due}}
	if recordPacing(record, "", 260, "a", now) || record.Status.TenurePacing != nil {
		t.Fatal("a wait or inapplicable pacing was recorded")
	}
	for i, outcome := range []bitcoin.TenurePacingOutcome{
		bitcoin.TenurePacingUnbound, bitcoin.TenurePacingProgressed, bitcoin.TenurePacingExpired,
	} {
		tip := fmt.Sprintf("%064x", 260+i)
		if !recordPacing(record, outcome, int64(260+i), tip, now) ||
			recordPacing(record, outcome, int64(260+i), tip, now.Add(time.Second)) {
			t.Fatalf("%s was not counted exactly once", outcome)
		}
	}
	got := record.Status.TenurePacing
	if got.Unbound != 1 || got.Progressed != 1 || got.Expired != 1 || got.Outcome != bitcoin.TenurePacingExpired ||
		got.Height != 262 || got.DelayMilliseconds != 1500 {
		t.Fatalf("recorded %+v", got)
	}
}

func TestTenureProgressReleasesBootstrapAdvancementWithoutWaiting(t *testing.T) {
	f, actor := pox5WindowFixture(t, 294)
	ctx := context.Background()
	scheduler := f.schedulerFor()
	//nolint:contextcheck // Fixture reads use testing.T.Context, independent of the exercised operation deadline.
	advance := func(height int64) {
		t.Helper()
		f.reconcile(t, scheduler)
		f.reconcile(t, scheduler)
		if offer := f.readRecord(t).Spec.Offer; offer == nil || offer.ExpectedHeight != height {
			t.Fatalf("no advancement from %d: %+v", height, f.readInitial(t).Status)
		}
		if err := f.worker.Step(ctx); err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool { return f.readRecord(t).Status.LastReceipt != nil })
		if err := f.worker.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.worker.Step(ctx); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, scheduler) // Arms the cadence.
	f.now = f.now.Add(time.Second)
	advance(284) // No own receipt binds 284: Unbound.
	tip := fmt.Sprintf("%064x", 285)
	f.now = f.now.Add(time.Second)
	observe := func(burnBlock string) {
		t.Helper()
		current := &api.StacksNetworkParticipant{}
		if err := f.c.Get(ctx, client.ObjectKeyFromObject(actor), current); err != nil {
			t.Fatal(err)
		}
		rt := current.Status.Runtime
		view := pacedMiner("actor", 285, burnBlock, f.now).Protocol
		view.PodUID, view.ContainerID, view.ConfigurationDigest = rt.PodRef.UID, rt.ContainerID, rt.ConfigurationDigest
		rt.Protocol = view
		if err := f.c.Status().Update(ctx, current); err != nil {
			t.Fatal(err)
		}
	}
	wakes := func(obj client.Object) bool {
		t.Helper()
		return len(scheduler.enqueueTenureProgress(ctx, obj)) == 1
	}
	if wakes(actor) {
		t.Fatal("a miner view woke a scheduler that is not waiting for tenure progress")
	}
	observe(strings.Repeat("b", 64))
	f.reconcile(t, scheduler)
	if got := f.readInitial(t).Status.Reason; got != reasonAwaitingTenureProgress {
		t.Fatalf("a tenure from another block released advancement: %s", got)
	}
	if !wakes(actor) {
		t.Fatal("a captured miner's view did not wake the waiting scheduler")
	}
	if wakes(f.node) {
		t.Fatal("a participant that is not a captured miner woke the scheduler")
	}
	observe(tip)
	advance(285)
	if wakes(actor) {
		t.Fatal("a miner view woke the scheduler after tenure progress released it")
	}
	pacing := f.readInitial(t).Status.TenurePacing
	if pacing == nil || pacing.Unbound != 1 || pacing.Progressed != 1 || pacing.Expired != 0 ||
		pacing.Tip != tip || pacing.Outcome != bitcoin.TenurePacingProgressed {
		t.Fatalf("pacing = %+v", pacing)
	}
}

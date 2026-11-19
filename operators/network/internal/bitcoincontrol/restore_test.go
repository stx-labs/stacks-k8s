package bitcoincontrol

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// restoreFrom freezes a snapshot captured at height into the fixture's genesis and moves the
// modeled node to that restored chain, past the first gate's ceiling. No snapshot resource exists:
// a restored network uses only what its genesis froze.
func (f *testFixture) restoreFrom(t *testing.T, height int64) {
	t.Helper()
	ctx := context.Background()
	genesis := &api.StacksGenesis{}
	if e := f.c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "genesis"}, genesis); e != nil {
		t.Fatal(e)
	}
	genesis.Spec.Source.Dependencies = append(genesis.Spec.Source.Dependencies, common.Binding{
		Kind:        api.KindStacksChainstateSnapshot,
		Name:        "seed",
		UID:         "seed-uid",
		Fingerprint: "sha256:seed",
	})
	genesis.Spec.Source.Restored = &api.RestoredChainstate{
		Image:   "snapshot:test",
		Bitcoin: api.SnapshotBitcoinTip{Height: height, BlockHash: fmt.Sprintf("%064x", height)},
	}
	if e := f.c.Update(ctx, genesis); e != nil {
		t.Fatal(e)
	}
	ref := binding("StacksGenesis", genesis)
	ref.Fingerprint = foundation.Digest(genesis.Spec)
	root := &api.StacksNetwork{}
	if e := f.c.Get(ctx, client.ObjectKeyFromObject(f.root), root); e != nil {
		t.Fatal(e)
	}
	root.Status.GenesisRef = &ref
	if e := f.c.Status().Update(ctx, root); e != nil {
		t.Fatal(e)
	}
	initial := f.readInitial(t)
	initial.Spec.Genesis = ref
	if e := f.c.Update(ctx, initial); e != nil {
		t.Fatal(e)
	}
	f.rpc.height = height
}

func (f *testFixture) readRoot(t *testing.T) *api.StacksNetwork {
	t.Helper()
	root := &api.StacksNetwork{}
	if e := f.c.Get(context.Background(), client.ObjectKeyFromObject(f.root), root); e != nil {
		t.Fatal(e)
	}
	return root
}

func (f *testFixture) generated() int {
	f.rpc.mu.Lock()
	defer f.rpc.mu.Unlock()
	n := 0
	for _, call := range f.rpc.calls {
		if strings.HasPrefix(call, "Generate") {
			n++
		}
	}
	return n
}

func TestRestoreRefreshesTheTipOnceThenPrepares(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.worker.Now = func() time.Time { return f.now }
	f.restoreFrom(t, 217)
	s := f.schedulerFor()
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	f.reconcile(t, s)
	f.reconcile(t, s)
	offer := f.readRecord(t).Spec.Offer
	if offer == nil || offer.Mode != bitcoin.OfferRestore || offer.ExpectedHeight != 217 ||
		offer.ExpectedTip != fmt.Sprintf("%064x", 217) || offer.Ceiling != 218 || offerBlocks(offer) != 1 ||
		offer.Address != testAddress {
		t.Fatalf("restore must offer exactly one block on the snapshot tip: %+v", offer)
	}
	if initial := f.readInitial(t); initial.Status.PreparedAt != nil {
		t.Fatal("preparation recorded before the refresh block")
	}
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return f.readRecord(t).Status.CompletedOffer == offer.Number })
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(time.Second)
	f.reconcile(t, s)
	initial := f.readInitial(t)
	if initial.Status.PreparedAt == nil || initial.Status.Reason != reasonRestorePrepared ||
		initial.Status.LastAccountedOffer != offer.Number || len(initial.Status.Funded) != 0 ||
		f.rpc.height != 218 || f.generated() != 1 {
		t.Fatalf("restore did not prepare on the refreshed tip: %+v height %d", initial.Status, f.rpc.height)
	}
	for range 3 {
		f.now = f.now.Add(time.Second)
		f.reconcile(t, s)
		_ = f.worker.Step(ctx)
	}
	if f.generated() != 1 {
		t.Fatal("restore generated more than the single refresh block")
	}
}

// refreshLanded drives a restore until the worker has generated the refresh block, before the
// scheduler accounts its receipt, and returns the offer it generated.
func (f *testFixture) refreshLanded(t *testing.T, s *Scheduler) *bitcoin.BitcoinBlockOffer {
	t.Helper()
	ctx := context.Background()
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	f.reconcile(t, s)
	f.reconcile(t, s)
	offer := f.readRecord(t).Spec.Offer
	if offer == nil || offer.Mode != bitcoin.OfferRestore {
		t.Fatalf("no restore refresh offered: %+v", f.readInitial(t).Status)
	}
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return f.readRecord(t).Status.CompletedOffer == offer.Number })
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	return offer
}

// Before preparation, only the refresh receipt is ever accounted: an override change that
// withdraws the current offer, or an offer re-issued while the refresh was in flight, must not
// hide the landed refresh and leave the restore blocked at height H+1.
func TestRestorePreparesWhicheverRefreshOfferIsCurrent(t *testing.T) {
	for name, mutate := range map[string]func(offer *bitcoin.BitcoinBlockOffer) *bitcoin.BitcoinBlockOffer{
		"offer withdrawn": func(*bitcoin.BitcoinBlockOffer) *bitcoin.BitcoinBlockOffer { return nil },
		"offer re-issued": func(offer *bitcoin.BitcoinBlockOffer) *bitcoin.BitcoinBlockOffer {
			next := offer.DeepCopy()
			next.Number++
			return next
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t)
			f.worker.Now = func() time.Time { return f.now }
			f.restoreFrom(t, 217)
			s := f.schedulerFor()
			offer := f.refreshLanded(t, s)
			initial := f.readInitial(t)
			initial.Status.Offer = mutate(offer)
			if e := f.c.Status().Update(ctx, initial); e != nil {
				t.Fatal(e)
			}
			f.now = f.now.Add(time.Second)
			f.reconcile(t, s)
			initial = f.readInitial(t)
			if initial.Status.PreparedAt == nil || initial.Status.Reason != reasonRestorePrepared ||
				initial.Status.Offer != nil || initial.Status.LastAccountedOffer != offer.Number {
				t.Fatalf("landed refresh not recognized: %+v", initial.Status)
			}
			_ = f.worker.Step(ctx)
			if f.generated() != 1 {
				t.Fatalf("generated %d blocks, want only the refresh", f.generated())
			}
		})
	}
}

// The worker refuses an unauthorized offer silently; the scheduler surfaces repeated unanswered
// refreshes instead of reporting an outstanding opportunity forever.
func TestRestoreReportsAnUnacknowledgedRefresh(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.worker.Now = func() time.Time { return f.now }
	f.restoreFrom(t, 217)
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	s := f.schedulerFor()
	// One observation stays usable while the worker, standing still, never answers.
	s.Freshness = time.Hour
	var reasons []string
	for range 3 {
		f.reconcile(t, s)
		reasons = append(reasons, f.readInitial(t).Status.Reason)
		f.now = f.now.Add(bootstrapAdmissionWindow + time.Second)
	}
	want := []string{reasonOpportunitySelected, reasonOpportunitySelected, reasonRestoreRefreshUnacknowledged}
	if !reflect.DeepEqual(reasons, want) {
		t.Fatalf("reasons %v, want %v", reasons, want)
	}
	if f.generated() != 0 {
		t.Fatal("a silent worker generated")
	}
}

// An active schedule override authorizes every offer, the restore refresh included: without its
// binding the worker would refuse the refresh until the override ends.
func TestRestoreRefreshCarriesAnActiveOverride(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.worker.Now = func() time.Time { return f.now }
	f.restoreFrom(t, 217)
	request := newOverride(f, "fast", "10s", "10m")
	request.Finalizers = []string{OverrideFinalizer}
	if e := f.c.Create(ctx, request); e != nil {
		t.Fatal(e)
	}
	active := &bitcoin.BitcoinActiveOverride{
		Override:   objectref.BitcoinScheduleOverride(request),
		Production: objectref.Participant(f.production),
		Schedule:   *request.Spec.Schedule,
		StartedAt:  metav1.NewTime(f.now),
		ExpiresAt:  metav1.NewTime(f.now.Add(10 * time.Minute)),
	}
	request.Status.Admission = active.DeepCopy()
	if e := f.c.Update(ctx, request); e != nil {
		t.Fatal(e)
	}
	initial := f.readInitial(t)
	initial.Status.Override = active
	if e := f.c.Status().Update(ctx, initial); e != nil {
		t.Fatal(e)
	}
	offer := f.refreshLanded(t, f.schedulerFor())
	if offer.Override == nil || *offer.Override != active.Override || f.generated() != 1 {
		t.Fatalf("refresh under an override: offer %+v, generated %d", offer, f.generated())
	}
}

// A restored miner has usually spent part of its bootstrap funding on block commits, so the restore
// needs only the mature output the miner start gate requires, not MatureOutputsPerMiner.
func TestRestoreRequiresOneMatureOutputPerMiner(t *testing.T) {
	// The fixture's wallet has one mature coinbase output from height 101.
	for name, height := range map[string]int64{
		"bootstrap funding partly spent": 217,
		"no mature output":               100,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t)
			f.worker.Now = func() time.Time { return f.now }
			f.restoreFrom(t, height)
			initial := f.readInitial(t)
			initial.Spec.MatureOutputsPerMiner = 2
			if e := f.c.Update(ctx, initial); e != nil {
				t.Fatal(e)
			}
			if e := f.worker.Step(ctx); e != nil {
				t.Fatal(e)
			}
			s := f.schedulerFor()
			f.reconcile(t, s)
			f.reconcile(t, s)
			initial = f.readInitial(t)
			if height >= 101 {
				if offer := initial.Status.Offer; offer == nil || offer.Mode != bitcoin.OfferRestore {
					t.Fatalf("restore must offer the refresh with one mature output: %+v", initial.Status)
				}
				return
			}
			if initial.Status.Offer != nil || initial.Status.Phase != bitcoin.InitializationBlocked ||
				initial.Status.Reason != reasonRestoredWalletsImmature || f.generated() != 0 {
				t.Fatalf("restore must block without a mature miner output: %+v", initial.Status)
			}
		})
	}
}

// The worker reads the snapshot tip from the frozen genesis, which its Role already covers, so no
// worker is ever granted the snapshot resource.
func TestWorkerRoleNeverReadsSnapshots(t *testing.T) {
	f := baselineFixture(t)
	restored := &api.StacksGenesis{ObjectMeta: metav1.ObjectMeta{
		Name: f.initial.Spec.Genesis.Name, Namespace: "test", UID: f.initial.Spec.Genesis.UID,
	}}
	restored.Spec.Source.Restored = &api.RestoredChainstate{Image: "snapshot:test"}
	restored.Spec.Source.Dependencies = []common.Binding{
		{Kind: api.KindStacksChainstateSnapshot, Name: "seed", UID: "seed-uid"},
	}
	objects, err := workerResources(f.node, f.record, f.initial, "worker:test", 1, nil, restored)
	if err != nil {
		t.Fatal(err)
	}
	genesisGranted := false
	for _, rule := range objects[1].(*rbacv1.Role).Rules {
		for _, resource := range rule.Resources {
			if resource == api.ResourceStacksChainstateSnapshot {
				t.Fatalf("worker granted snapshots: %+v", rule)
			}
			genesisGranted = genesisGranted || resource == api.ResourceStacksGenesis &&
				len(rule.ResourceNames) == 1 && rule.ResourceNames[0] == restored.Name
		}
	}
	if !genesisGranted {
		t.Fatal("worker cannot read its frozen genesis")
	}
}

// A restored network must never fall back to bootstrap generation, even when its genesis cannot be
// read: the scheduler holds without an offer, and the gate authority refuses a restored genesis.
func TestRestoreFailsClosedWithoutItsFrozenGenesis(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.worker.Now = func() time.Time { return f.now }
	f.restoreFrom(t, 217)
	if _, e := currentGate(ctx, f.c, f.readRoot(t), f.readInitial(t)); e == nil {
		t.Fatal("bootstrap gate authority granted on a restored genesis")
	}
	root := f.readRoot(t)
	root.Status.GenesisRef.Fingerprint = "sha256:changed"
	if e := f.c.Status().Update(ctx, root); e != nil {
		t.Fatal(e)
	}
	s := f.schedulerFor()
	f.reconcile(t, s)
	initial := f.readInitial(t)
	if initial.Status.Offer != nil || initial.Status.PreparedAt != nil ||
		initial.Status.Phase != bitcoin.InitializationHeld || initial.Status.Reason != reasonFrozenGenesisUnavailable ||
		f.generated() != 0 {
		t.Fatalf("unexpected status without the frozen genesis: %+v", initial.Status)
	}
}

func TestRestoreRefusesAnUnexpectedChain(t *testing.T) {
	for name, mutate := range map[string]func(*testFixture){
		"other tip":    func(f *testFixture) { f.rpc.tip = strings.Repeat("f", 64) },
		"lower height": func(f *testFixture) { f.rpc.height = 216 },
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t)
			f.worker.Now = func() time.Time { return f.now }
			f.restoreFrom(t, 217)
			mutate(f)
			if e := f.worker.Step(ctx); e != nil {
				t.Fatal(e)
			}
			s := f.schedulerFor()
			f.reconcile(t, s)
			f.reconcile(t, s)
			initial := f.readInitial(t)
			if initial.Status.Offer != nil || initial.Status.PreparedAt != nil ||
				initial.Status.Phase != bitcoin.InitializationBlocked ||
				initial.Status.Reason != reasonRestoredChainDiffers {
				t.Fatalf("unexpected restore status %+v", initial.Status)
			}
			if f.generated() != 0 {
				t.Fatal("generated on an unproven chain")
			}
		})
	}
}

// Each refused case passes the live-chain preflight and is refused by the exact rule its expected
// error names, so removing that rule fails the case even though another check would still stop it.
func TestWorkerRefusesRestoreOffersBeyondTheSnapshot(t *testing.T) {
	const (
		shared       = "generation opportunity is not currently authorized"
		unauthorized = "restore refresh is not authorized"
		offSnapshot  = "restore refresh does not extend the snapshot tip"
	)
	type offerCase struct {
		mutate  func(*testFixture, *bitcoin.BitcoinInitialization, *bitcoin.BitcoinBlockOffer)
		refusal string
	}
	for name, tc := range map[string]offerCase{
		// The control case proves this path generates when the offer is exactly the refresh.
		"valid": {func(*testFixture, *bitcoin.BitcoinInitialization, *bitcoin.BitcoinBlockOffer) {}, ""},
		"two blocks": {func(_ *testFixture, _ *bitcoin.BitcoinInitialization, o *bitcoin.BitcoinBlockOffer) {
			o.Count, o.Ceiling = 2, 219
		}, shared},
		// The shared count check allows one block under a wider ceiling; the refresh is exactly H+1.
		"wider ceiling": {func(_ *testFixture, _ *bitcoin.BitcoinInitialization, o *bitcoin.BitcoinBlockOffer) {
			o.Ceiling = 219
		}, unauthorized},
		// The live chain matches the offer at another height than the snapshot's.
		"other height": {func(_ *testFixture, _ *bitcoin.BitcoinInitialization, o *bitcoin.BitcoinBlockOffer) {
			o.ExpectedHeight, o.Ceiling, o.ExpectedTip = 216, 217, fmt.Sprintf("%064x", 216)
		}, offSnapshot},
		// The live chain matches the offer at the snapshot's height, on another tip.
		"off the snapshot tip": {func(f *testFixture, _ *bitcoin.BitcoinInitialization, o *bitcoin.BitcoinBlockOffer) {
			f.rpc.tip = strings.Repeat("f", 64)
			o.ExpectedTip = f.rpc.tip
		}, offSnapshot},
		"other wallet": {func(_ *testFixture, _ *bitcoin.BitcoinInitialization, o *bitcoin.BitcoinBlockOffer) {
			o.Address = "other"
		}, unauthorized},
		"after preparation": {func(f *testFixture, initial *bitcoin.BitcoinInitialization, _ *bitcoin.BitcoinBlockOffer) {
			prepared := metav1.NewTime(f.now)
			initial.Status.PreparedAt = &prepared
		}, unauthorized},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t)
			f.worker.Now = func() time.Time { return f.now }
			f.restoreFrom(t, 217)
			f.offer(t, 217)
			initial := f.readInitial(t)
			offer := initial.Status.Offer
			offer.Mode, offer.Ceiling = bitcoin.OfferRestore, 218
			tc.mutate(f, initial, offer)
			if e := f.c.Status().Update(ctx, initial); e != nil {
				t.Fatal(e)
			}
			record := f.readRecord(t)
			record.Spec.Offer = offer.DeepCopy()
			if e := f.c.Update(ctx, record); e != nil {
				t.Fatal(e)
			}
			f.rpc.height = offer.ExpectedHeight
			for range 2 {
				err := f.worker.Step(ctx)
				if tc.refusal == "" && err != nil || tc.refusal != "" && (err == nil || err.Error() != tc.refusal) {
					t.Fatalf("worker step: %v, want refusal %q", err, tc.refusal)
				}
			}
			// The worker sends the armed RPC from its own goroutine: wait for the valid offer to
			// complete, and require that a refused one was never armed.
			want := 0
			if tc.refusal == "" {
				want = 1
				eventually(t, func() bool { return f.readRecord(t).Status.CompletedOffer == offer.Number })
			} else if armed := f.readRecord(t).Status.Armed; armed != nil {
				t.Fatalf("refused offer was armed: %+v", armed)
			}
			if got := f.generated(); got != want {
				t.Fatalf("worker generated %d blocks, want %d", got, want)
			}
		})
	}
}

package bitcoincontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func fixed(interval string) bitcoin.Cadence {
	return bitcoin.Cadence{Mode: bitcoin.CadenceFixed, Interval: ptr.To(common.Duration(interval))}
}

// heightSchedule lists entries out of order; selection must not depend on it.
func heightSchedule(fast string, boundary int64, slow bitcoin.Cadence) *bitcoin.BitcoinBlockScheduleSpec {
	return &bitcoin.BitcoinBlockScheduleSpec{Cadences: []bitcoin.HeightCadence{
		{FromHeight: boundary, Cadence: slow},
		{FromHeight: 0, Cadence: fixed(fast)},
	}}
}

// setSchedule replaces the producer's admitted timing as a new admission would.
func (f *testFixture) setSchedule(t *testing.T, schedule *bitcoin.BitcoinBlockScheduleSpec) {
	t.Helper()
	ctx := context.Background()
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(f.production), f.production); err != nil {
		t.Fatal(err)
	}
	f.production.Status.Admission.Configuration.BitcoinBlockProduction.Schedule = schedule
	f.production.Status.Admission.PolicyDigest = foundation.Digest(f.production.Status.Admission.Configuration)
	if err := f.c.Status().Update(ctx, f.production); err != nil {
		t.Fatal(err)
	}
}

// setBaselineHeights freshly observes every baseline target at height.
func (f *testFixture) setBaselineHeights(t *testing.T, height int64) {
	t.Helper()
	ctx := context.Background()
	for _, name := range []string{f.record.Name, "execution-two"} {
		var execution bitcoin.BitcoinExecution
		if err := f.c.Get(ctx, client.ObjectKey{Namespace: f.record.Namespace, Name: name}, &execution); err != nil {
			t.Fatal(err)
		}
		execution.Status.Observation.Height = height
		execution.Status.Observation.ObservedAt = metav1.NewTime(f.now)
		if err := f.c.Status().Update(ctx, &execution); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSelectCadenceByObservedHeight(t *testing.T) {
	record := &bitcoin.BitcoinInitialization{}
	schedule := heightSchedule("5s", 282, fixed("60s"))
	for _, tc := range []struct {
		height int64
		from   int64
		want   string
	}{
		{0, 0, "5s"}, {281, 0, "5s"}, {282, 282, "60s"}, {290, 282, "60s"}, {1 << 40, 282, "60s"},
	} {
		selection, err := selectCadence(record, schedule, ptr.To(tc.height))
		if err != nil || ptr.Deref(selection.FromHeight, -1) != tc.from ||
			string(*selection.Cadence.Interval) != tc.want || *selection.Height != tc.height {
			t.Fatalf("height %d selected %+v: %v", tc.height, selection, err)
		}
	}
	if _, err := selectCadence(record, schedule, nil); !errors.Is(err, errCadenceHeightUnavailable) {
		t.Fatalf("height cadences selected without a tip: %v", err)
	}
	if phase, reason := cadenceFailure(errCadenceHeightUnavailable); phase != bitcoin.InitializationWaiting ||
		reason != reasonCadenceHeightUnavailable {
		t.Fatal("missing tip is not transient")
	}
	single := &bitcoin.BitcoinBlockScheduleSpec{Cadence: ptr.To(fixed("60s"))}
	if selection, err := selectCadence(record, single, nil); err != nil || selection.FromHeight != nil ||
		selection.Height != nil {
		t.Fatalf("single cadence needed a height: %+v %v", selection, err)
	}
	if _, err := selectCadence(record, &bitcoin.BitcoinBlockScheduleSpec{Cadences: []bitcoin.HeightCadence{
		{FromHeight: 10, Cadence: fixed("1s")},
	}}, ptr.To[int64](5)); err == nil {
		t.Fatal("uncovered height selected a cadence")
	}
}

func TestSelectCadenceOverrideSpansHeightBoundary(t *testing.T) {
	override := common.Binding{Kind: bitcoin.KindBitcoinBlockScheduleOverride, Name: "fast", UID: "override-uid"}
	record := &bitcoin.BitcoinInitialization{}
	record.Status.Override = &bitcoin.BitcoinActiveOverride{
		Override: override,
		Schedule: bitcoin.BitcoinBlockScheduleSpec{Cadence: ptr.To(fixed("2s"))},
	}
	schedule := heightSchedule("5s", 282, fixed("60s"))
	below, err := selectCadence(record, schedule, ptr.To[int64](281))
	if err != nil {
		t.Fatal(err)
	}
	above, err := selectCadence(record, schedule, ptr.To[int64](282))
	if err != nil || *above.Override != override || above.FromHeight != nil ||
		string(*above.Cadence.Interval) != "2s" || !sameCadence(below, above) {
		t.Fatalf("override did not govern across the boundary: %+v %v", above, err)
	}
	record.Status.Override = nil
	after, err := selectCadence(record, schedule, ptr.To[int64](282))
	if err != nil || sameCadence(above, after) || ptr.Deref(after.FromHeight, -1) != 282 {
		t.Fatalf("override end did not resume the height entry: %+v %v", after, err)
	}
}

func TestOverrideRejectsHeightCadences(t *testing.T) {
	s := &Scheduler{}
	request := &bitcoin.BitcoinBlockScheduleOverride{}
	request.Spec.Schedule = heightSchedule("5s", 282, fixed("60s"))
	if _, _, err := s.overrideSchedule(context.Background(), "test", request); err == nil {
		t.Fatal("override accepted height cadences")
	}
}

func TestScheduleValidChecksEveryCadence(t *testing.T) {
	for _, schedule := range []*bitcoin.BitcoinBlockScheduleSpec{
		nil,
		{},
		{Cadence: ptr.To(fixed("5s")), Cadences: heightSchedule("5s", 1, fixed("1s")).Cadences},
		heightSchedule("5s", 1, fixed("0s")),
	} {
		if scheduleValid(schedule) == nil {
			t.Fatalf("invalid schedule accepted: %+v", schedule)
		}
	}
	if err := scheduleValid(heightSchedule("5s", 282, fixed("60s"))); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapHeightCadenceReanchorsAtBoundaryAndReorg(t *testing.T) {
	f := newFixture(t)
	f.setSchedule(t, heightSchedule("1s", 2, fixed("10s")))
	f.worker.Now = func() time.Time { return f.now }
	s := f.schedulerFor()
	step := func() {
		t.Helper()
		if err := f.worker.Step(t.Context()); err != nil {
			t.Fatal(err)
		}
		f.worker.workers.Wait()
	}
	step()
	f.reconcile(t, s)
	if c := f.readInitial(t).Status.Cadence; c == nil || ptr.Deref(c.FromHeight, -1) != 0 {
		t.Fatalf("bootstrap did not record its fast selection: %+v", c)
	}
	for range 2 {
		f.now = f.now.Add(time.Second)
		f.reconcile(t, s)
		f.reconcile(t, s)
		step()
		f.reconcile(t, s)
		step()
	}
	if f.rpc.count() != 2 {
		t.Fatalf("expected two fast blocks, sent %d", f.rpc.count())
	}
	f.reconcile(t, s)
	state := f.readInitial(t).Status
	if state.Reason != reasonCadenceArmed || ptr.Deref(state.Cadence.FromHeight, -1) != 2 ||
		ptr.Deref(state.Cadence.Height, -1) != 2 || !state.NextOpportunityAt.Time.Equal(f.now.Add(10*time.Second)) {
		t.Fatalf("boundary height did not re-anchor to the slow cadence: %+v", state)
	}
	anchor := state.NextOpportunityAt.DeepCopy()
	f.now = f.now.Add(5 * time.Second)
	f.reconcile(t, f.schedulerFor())
	if state = f.readInitial(t).Status; state.Reason != reasonCadenceWaiting ||
		!state.NextOpportunityAt.Equal(anchor) || f.rpc.count() != 2 {
		t.Fatalf("restart moved the anchor or sent early: %+v", state)
	}
	f.rpc.mu.Lock()
	f.rpc.height = 1
	f.rpc.mu.Unlock()
	step()
	f.reconcile(t, s)
	if state = f.readInitial(t).Status; ptr.Deref(state.Cadence.FromHeight, -1) != 0 ||
		!state.NextOpportunityAt.Time.Equal(f.now.Add(time.Second)) {
		t.Fatalf("reorg below the boundary kept the slow cadence: %+v", state)
	}
}

func TestBaselineHeightCadenceReanchorsWithoutRedrawOrCatchUp(t *testing.T) {
	f := baselineFixture(t)
	slow := bitcoin.Cadence{
		Mode:            bitcoin.CadenceUniform,
		MinimumInterval: ptr.To(common.Duration("50s")),
		MaximumInterval: ptr.To(common.Duration("70s")),
	}
	f.setSchedule(t, heightSchedule("1s", 240, slow))
	s := f.schedulerFor()
	draws := 0
	s.Draw = func(int64) int64 { draws++; return 0 }
	armBaseline(t, f, s)
	fastWindow, _ := progressWindowSeconds(ptr.To(fixed("1s")))
	slowWindow, _ := progressWindowSeconds(&slow)
	state := f.readInitial(t).Status
	if ptr.Deref(state.Cadence.FromHeight, -1) != 0 || !state.NextOpportunityAt.Time.Equal(f.now.Add(time.Second)) ||
		ptr.Deref(state.Baseline.Scheduling.Cadence.FromHeight, -1) != 0 ||
		state.Baseline.Scheduling.ProgressWindowSeconds != fastWindow {
		t.Fatalf("baseline did not publish its fast selection: %+v", state)
	}
	f.setBaselineHeights(t, 241) // skips the boundary height itself
	f.reconcile(t, s)
	state = f.readInitial(t).Status
	if ptr.Deref(state.Cadence.FromHeight, -1) != 240 || ptr.Deref(state.Cadence.Height, -1) != 241 ||
		!state.NextOpportunityAt.Time.Equal(f.now.Add(50*time.Second)) ||
		ptr.Deref(state.Baseline.Scheduling.Cadence.FromHeight, -1) != 240 ||
		state.Baseline.Scheduling.ProgressWindowSeconds != slowWindow ||
		state.Baseline.Scheduling.Opportunities != 0 {
		t.Fatalf("crossing the boundary did not re-anchor once: %+v", state)
	}
	anchor, drawn := state.NextOpportunityAt.DeepCopy(), draws
	for range 3 {
		f.now = f.now.Add(10 * time.Second)
		f.setBaselineHeights(t, 241)
		f.reconcile(t, f.schedulerFor())
		f.reconcile(t, s)
	}
	if state = f.readInitial(t).Status; !state.NextOpportunityAt.Equal(anchor) || draws != drawn {
		t.Fatalf("waiting or restart redrew uniform timing: %+v draws %d->%d", state, drawn, draws)
	}
	f.setBaselineHeights(t, 239)
	f.reconcile(t, s)
	if state = f.readInitial(t).Status; ptr.Deref(state.Cadence.FromHeight, -1) != 0 ||
		!state.NextOpportunityAt.Time.Equal(f.now.Add(time.Second)) ||
		state.Baseline.Scheduling.ProgressWindowSeconds != fastWindow {
		t.Fatalf("reorg below the boundary kept the slow cadence: %+v", state)
	}
}

func TestBaselineHeightCadencePreservesCommittedOpportunity(t *testing.T) {
	f := baselineFixture(t)
	f.setSchedule(t, heightSchedule("1s", 240, fixed("60s")))
	s := f.schedulerFor()
	armBaseline(t, f, s)
	f.now = f.now.Add(time.Second)
	f.reconcile(t, s)
	selected := f.readInitial(t).Status
	if selected.Baseline.Stage != bitcoin.BaselineSelected {
		t.Fatalf("no opportunity selected: %+v", selected)
	}
	f.setBaselineHeights(t, 241)
	f.reconcile(t, s)
	f.reconcile(t, s)
	state := f.readInitial(t).Status
	if state.Baseline.Stage != bitcoin.BaselineAssigned || state.Offer == nil ||
		!state.Offer.ExpiresAt.Equal(selected.NextOpportunityAt) {
		t.Fatalf("boundary withdrew a committed opportunity: %+v", state)
	}
	offer := state.Offer.DeepCopy()
	f.reconcile(t, s)
	state = f.readInitial(t).Status
	if ptr.Deref(state.Cadence.FromHeight, -1) != 240 || !state.NextOpportunityAt.Time.Equal(f.now.Add(time.Minute)) ||
		state.Baseline.Scheduling.Assigned != 1 || !equality.Semantic.DeepEqual(state.Offer, offer) {
		t.Fatalf("assigned work was disturbed or timing not re-anchored: %+v", state)
	}
}

func TestBaselineInputsAcceptReorderedReferencedSchedule(t *testing.T) {
	f := baselineFixture(t)
	ctx := context.Background()
	stored := &bitcoin.BitcoinBlockSchedule{
		ObjectMeta: metav1.ObjectMeta{Namespace: f.production.Namespace, Name: "steady", UID: "schedule-uid"},
		Spec:       *heightSchedule("5s", 282, fixed("60s")),
	}
	if err := f.c.Create(ctx, stored); err != nil {
		t.Fatal(err)
	}
	admitted := foundation.CanonicalSchedule(stored.Spec)
	f.setSchedule(t, &admitted)
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(f.production), f.production); err != nil {
		t.Fatal(err)
	}
	f.production.Status.Admission.Dependencies = append(
		f.production.Status.Admission.Dependencies,
		objectref.WithFingerprint(objectref.BitcoinBlockSchedule(stored), foundation.Digest(admitted)),
	)
	if err := f.c.Status().Update(ctx, f.production); err != nil {
		t.Fatal(err)
	}
	inputs, err := baselineInputs(ctx, f.c, f.root, f.readInitial(t), f.production)
	if err != nil || inputs.ScheduleRef == nil || inputs.ScheduleRef.UID != stored.UID {
		t.Fatalf("reordered stored schedule lost its admitted identity: %+v %v", inputs.ScheduleRef, err)
	}
}

func TestBaselineTimingHeightRequiresCurrentProcessObservation(t *testing.T) {
	f := baselineFixture(t)
	f.setSchedule(t, heightSchedule("1s", 240, fixed("60s")))
	s := f.schedulerFor()
	armBaseline(t, f, s)
	f.setBaselineHeights(t, 239)
	ctx := context.Background()
	updateSecond := func(change func(*bitcoin.BitcoinExecution)) {
		t.Helper()
		var execution bitcoin.BitcoinExecution
		key := client.ObjectKey{Namespace: f.record.Namespace, Name: "execution-two"}
		if err := f.c.Get(ctx, key, &execution); err != nil {
			t.Fatal(err)
		}
		change(&execution)
		if err := f.c.Status().Update(ctx, &execution); err != nil {
			t.Fatal(err)
		}
	}
	// A replaced process's last sample above the boundary must not select the slow cadence.
	updateSecond(func(e *bitcoin.BitcoinExecution) {
		e.Status.Observation.Height = 241
		e.Status.Observation.Target.Pod.UID = "replaced-pod-uid"
	})
	f.reconcile(t, s)
	state := f.readInitial(t).Status
	if ptr.Deref(state.Cadence.FromHeight, -1) != 0 || !state.NextOpportunityAt.Time.Equal(f.now.Add(time.Second)) {
		t.Fatalf("obsolete process observation selected timing: %+v", state)
	}
	// The current process's sample counts even while its target cannot take an offer.
	updateSecond(func(e *bitcoin.BitcoinExecution) {
		e.Status.Observation.Target.Pod.UID = "pod-two-uid"
		e.Status.Reservation = &common.Binding{Kind: bitcoin.KindBitcoinInitialization, Name: "other", UID: "other"}
	})
	f.reconcile(t, s)
	state = f.readInitial(t).Status
	if ptr.Deref(state.Cadence.FromHeight, -1) != 240 || state.Baseline.Scheduling.EligibleTargets != 1 ||
		!state.NextOpportunityAt.Time.Equal(f.now.Add(time.Minute)) {
		t.Fatalf("busy current-process observation ignored for timing: %+v", state)
	}
}

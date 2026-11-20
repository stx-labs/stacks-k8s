package bitcoincontrol

import (
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestSchedulerFiltersHeartbeatsButRetainsAuthorityAndReceipts(t *testing.T) {
	old := &bitcoin.BitcoinExecution{
		Status: bitcoin.BitcoinExecutionStatus{Observation: &bitcoin.BitcoinObservation{ObservedAt: metav1.Now()}},
	}
	current := old.DeepCopy()
	current.Status.Observation.ObservedAt = metav1.NewTime(time.Now().Add(time.Second))
	pred := schedulerEvents()
	if pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: current}) {
		t.Fatal("heartbeat enqueued")
	}
	current.Status.Observation.Height++
	if !pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: current}) {
		t.Fatal("block progress suppressed")
	}
	current = old.DeepCopy()
	current.Generation++
	if !pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: current}) {
		t.Fatal("authority change suppressed")
	}
	p := &api.StacksNetworkParticipant{Spec: api.StacksNetworkParticipantSpec{Kind: "StacksNode"}}
	changed := p.DeepCopy()
	changed.Status.Runtime = &api.ParticipantRuntimeStatus{Terminated: true}
	if pred.Update(event.UpdateEvent{ObjectOld: p, ObjectNew: changed}) {
		t.Fatal("unrelated actor status enqueued Bitcoin scheduler")
	}
	changed.Generation++
	if !pred.Update(event.UpdateEvent{ObjectOld: p, ObjectNew: changed}) {
		t.Fatal("declaration change suppressed")
	}
}

func TestSchedulerTimerPreservesShortCadenceAndRepairsWithoutEvents(t *testing.T) {
	now := time.Now()
	for _, d := range []time.Duration{time.Second, 10 * time.Second} {
		due := metav1.NewTime(now.Add(d))
		record := &bitcoin.BitcoinInitialization{Status: bitcoin.BitcoinInitializationStatus{NextOpportunityAt: &due}}
		if got := schedulerDelay(record, now); got != min(d, 2*time.Second) {
			t.Fatalf("delay=%s", got)
		}
	}
	if got := schedulerDelay(&bitcoin.BitcoinInitialization{}, now); got != 2*time.Second {
		t.Fatalf("repair delay=%s", got)
	}
}

func TestTenureViewEventsPassOnlyPacedProtocolChanges(t *testing.T) {
	pred := tenureViewEvents()
	now := metav1.Now()
	old := &api.StacksNetworkParticipant{Spec: api.StacksNetworkParticipantSpec{Kind: api.ParticipantStacksNode}}
	old.Status.Runtime = &api.ParticipantRuntimeStatus{Protocol: &api.StacksProtocolObservation{
		Available: true, BurnHeight: 260, BurnConsensusHash: "a", ObservedAt: now,
		Tenure: &api.StacksTenureObservation{ConsensusHash: "a", BurnBlockHash: "b", BurnHeight: 260},
	}}
	update := func(mutate func(*api.StacksProtocolObservation)) bool {
		current := old.DeepCopy()
		mutate(current.Status.Runtime.Protocol)
		return pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: current})
	}
	if update(func(v *api.StacksProtocolObservation) {
		v.ObservedAt = metav1.NewTime(now.Add(time.Second))
		v.StacksHeight++
	}) {
		t.Fatal("a refresh that leaves the paced view unchanged passed")
	}
	for name, mutate := range map[string]func(*api.StacksProtocolObservation){
		"availability": func(v *api.StacksProtocolObservation) { v.Available = false },
		"burn height":  func(v *api.StacksProtocolObservation) { v.BurnHeight++ },
		"burn tenure":  func(v *api.StacksProtocolObservation) { v.BurnConsensusHash = "c" },
		"tenure":       func(v *api.StacksProtocolObservation) { v.Tenure.ConsensusHash = "c" },
	} {
		if !update(mutate) {
			t.Fatalf("%s change suppressed", name)
		}
	}
	bitcoinNode := old.DeepCopy()
	bitcoinNode.Spec.Kind = api.ParticipantBitcoinNode
	changed := bitcoinNode.DeepCopy()
	changed.Status.Runtime.Protocol.BurnHeight++
	if pred.Update(event.UpdateEvent{ObjectOld: bitcoinNode, ObjectNew: changed}) {
		t.Fatal("a non-Stacks participant passed")
	}
	if pred.Create(event.CreateEvent{Object: old}) || pred.Delete(event.DeleteEvent{Object: old}) {
		t.Fatal("lifecycle events passed; the general participant watch owns them")
	}
}

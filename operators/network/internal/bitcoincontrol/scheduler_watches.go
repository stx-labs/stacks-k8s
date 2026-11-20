package bitcoincontrol

import (
	"context"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// schedulerDelay preserves due cadence while bounding observation repair polls.
func schedulerDelay(record *bitcoin.BitcoinInitialization, now time.Time) time.Duration {
	delay := time.Duration(foundation.ObservationPolicy().PollIntervalSeconds) * time.Second
	if due := record.Status.NextOpportunityAt; due != nil && due.After(now) {
		delay = min(delay, due.Sub(now))
	}
	return delay
}

// schedulerEvents ignores its own status and heartbeat-only events, retaining authority and receipt changes.
func schedulerEvents() predicate.Predicate {
	return predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		a, b := e.ObjectOld, e.ObjectNew
		if a == nil || b == nil {
			return false
		}
		same := equality.Semantic.DeepEqual
		if a.GetUID() != b.GetUID() || a.GetGeneration() != b.GetGeneration() ||
			!same(a.GetDeletionTimestamp(), b.GetDeletionTimestamp()) ||
			!same(a.GetOwnerReferences(), b.GetOwnerReferences()) ||
			!same(a.GetFinalizers(), b.GetFinalizers()) {
			return true
		}
		switch old := a.(type) {
		case *bitcoin.BitcoinExecution:
			current, ok := b.(*bitcoin.BitcoinExecution)
			return !ok || foundation.BitcoinExecutionChanged(old, current)
		case *bitcoin.BitcoinInitialization:
			return false // This controller publishes status and explicitly schedules its next continuation.
		case *api.StacksNetwork:
			current, ok := b.(*api.StacksNetwork)
			return !ok || !same(old.Status.Bitcoin, current.Status.Bitcoin) ||
				!same(old.Status.Initialization, current.Status.Initialization) ||
				failed(old) != failed(current)
		case *api.StacksNetworkParticipant:
			current, ok := b.(*api.StacksNetworkParticipant)
			if ok && current.Spec.Kind != api.ParticipantBitcoinNode &&
				current.Spec.Kind != api.ParticipantBitcoinBlockProduction {
				return false // Protocol gates are observed by the bounded continuation timer.
			}
			return !ok || !same(old.Status.Admission, current.Status.Admission) ||
				!same(old.Status.Runtime, current.Status.Runtime) ||
				!same(old.Status.BitcoinControl, current.Status.BitcoinControl) ||
				!same(old.Status.Conditions, current.Status.Conditions)
		}
		return true
	}}
}

// tenureViewEvents passes Stacks node updates that change the protocol facts tenure pacing
// reads; observation timestamps and other status refreshes are ignored.
func tenureViewEvents() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return false },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			a, okA := e.ObjectOld.(*api.StacksNetworkParticipant)
			b, okB := e.ObjectNew.(*api.StacksNetworkParticipant)
			return okA && okB && b.Spec.Kind == api.ParticipantStacksNode &&
				!equality.Semantic.DeepEqual(tenureView(a), tenureView(b))
		},
	}
}

// pacedView is the part of a protocol observation that tenureObserved compares.
type pacedView struct {
	Available         bool
	BurnHeight        uint64
	BurnConsensusHash string
	Tenure            *api.StacksTenureObservation
}

func tenureView(p *api.StacksNetworkParticipant) *pacedView {
	if p.Status.Runtime == nil || p.Status.Runtime.Protocol == nil {
		return nil
	}
	view := p.Status.Runtime.Protocol
	return &pacedView{view.Available, view.BurnHeight, view.BurnConsensusHash, view.Tenure}
}

// enqueueTenureProgress wakes a scheduler that is waiting for tenure progress when a captured
// miner's view changes. The continuation timer still bounds expiry and recovery; this only
// shortens the wait, and the scheduler re-reads every miner before advancing.
func (s *Scheduler) enqueueTenureProgress(ctx context.Context, obj client.Object) []ctrl.Request {
	requests := s.enqueue(ctx, obj)
	if len(requests) == 0 {
		return nil
	}
	record := &bitcoin.BitcoinInitialization{}
	if s.Client.Get(ctx, requests[0].NamespacedName, record) != nil ||
		record.Status.Reason != reasonAwaitingTenureProgress {
		return nil
	}
	genesis := &api.StacksGenesis{}
	key := client.ObjectKey{Namespace: record.Namespace, Name: record.Spec.Genesis.Name}
	if s.Client.Get(ctx, key, genesis) != nil || genesis.UID != record.Spec.Genesis.UID {
		return nil
	}
	for _, required := range genesis.Spec.Bootstrap.Requirements {
		if required.Participant.UID == obj.GetUID() && ptr.Deref(required.MiningEnabled, false) {
			return requests
		}
	}
	return nil
}

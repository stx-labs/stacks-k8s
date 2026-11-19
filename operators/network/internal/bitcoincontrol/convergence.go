package bitcoincontrol

import (
	"context"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// convergence is one identity-bound, identical observation of every frozen Bitcoin node.
type convergence struct {
	target *bitcoin.BitcoinExecution
	height int64
	tip    string
	// mature counts each wallet's best observed mature outputs, by wallet UID.
	mature map[string]int32
	// ceilingObservedAt is the target's observation time once it reached the first ceiling,
	// retained even when another node then fails to converge.
	ceilingObservedAt *metav1.Time
}

// executions reads the network's retained execution records by participant UID.
func (s *Scheduler) executions(
	ctx context.Context,
	root *api.StacksNetwork,
) (map[string]*bitcoin.BitcoinExecution, string) {
	allRecords := make(map[string]*bitcoin.BitcoinExecution, len(root.Status.Bitcoin.ExecutionRefs))
	for _, ref := range root.Status.Bitcoin.ExecutionRefs {
		candidate := &bitcoin.BitcoinExecution{}
		if e := s.Reader.Get(ctx, client.ObjectKey{
			Namespace: root.Namespace,
			Name:      ref.Name,
		}, candidate); e != nil {
			return nil, reasonExecutionRecordUnavailable
		}
		if candidate.UID != ref.UID {
			return nil, reasonExecutionRecordReplaced
		}
		allRecords[string(candidate.Spec.Participant.UID)] = candidate
	}
	return allRecords, ""
}

// converge requires fresh, identity-bound and identical chain and wallet observations from every
// frozen node. A non-empty reason reports the first unmet requirement with its phase.
func (s *Scheduler) converge(
	ctx context.Context,
	root *api.StacksNetwork,
	record *bitcoin.BitcoinInitialization,
	allRecords map[string]*bitcoin.BitcoinExecution,
) (convergence, bitcoin.InitializationPhase, string) {
	view := convergence{height: -1, mature: map[string]int32{}}
	// Observe the frozen target first so another node cannot postpone the ceiling deadline.
	nodes := make([]common.Binding, 0, len(record.Spec.Nodes))
	for _, node := range record.Spec.Nodes {
		if node.UID == record.Spec.Target.UID {
			nodes = append(nodes, node)
		}
	}
	for _, node := range record.Spec.Nodes {
		if node.UID != record.Spec.Target.UID {
			nodes = append(nodes, node)
		}
	}
	for _, node := range nodes {
		execution := allRecords[string(node.UID)]
		if execution == nil || execution.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(execution, root) {
			return view, bitcoin.InitializationBlocked, reasonExecutionBindingUnavailable
		}
		if node.UID == record.Spec.Target.UID {
			view.target = execution
		}
		if execution.Status.Armed != nil {
			return view, bitcoin.InitializationWaiting, reasonRPCOutstanding
		}
		observation := execution.Status.Observation
		if observation == nil || s.Now().Sub(observation.ObservedAt.Time) > s.Freshness ||
			observation.ObservedAt.After(s.Now()) {
			return view, bitcoin.InitializationWaiting, reasonNodeObservationUnavailable
		}
		p := &api.StacksNetworkParticipant{}
		if e := s.Reader.Get(
			ctx,
			client.ObjectKey{Namespace: root.Namespace, Name: node.Name},
			p,
		); e != nil ||
			p.UID != node.UID ||
			!participantCurrent(
				root,
				p,
			) ||
			p.Status.Runtime == nil ||
			p.Status.Runtime.PodRef == nil ||
			p.Status.Runtime.PodRef.UID != observation.Target.Pod.UID ||
			p.Status.Runtime.ContainerID != observation.Target.ContainerID ||
			p.Status.Admission.PolicyDigest != observation.Target.PolicyDigest ||
			p.Status.Runtime.ConfigRef == nil ||
			*p.Status.Runtime.ConfigRef != observation.Target.Configuration ||
			p.Status.Runtime.RPCSecretRef == nil ||
			*p.Status.Runtime.RPCSecretRef != observation.Target.Credentials {
			return view, bitcoin.InitializationWaiting, reasonNodeObservationIdentityChanged
		}
		if node.UID == record.Spec.Target.UID && observation.Height >= record.Spec.MinimumHeight {
			observed := observation.ObservedAt
			view.ceilingObservedAt = &observed
		}
		if view.height < 0 {
			view.height, view.tip = observation.Height, observation.Tip
		} else if view.height != observation.Height || view.tip != observation.Tip {
			return view, bitcoin.InitializationWaiting, reasonCoreViewsDiverged
		}
		expectedWallets := ptr.Deref(p.Status.Admission.Configuration.BitcoinNode.WalletRefs, nil)
		if len(observation.Wallets) != len(expectedWallets) {
			return view, bitcoin.InitializationWaiting, reasonWalletInventoryIncomplete
		}
		for _, ref := range expectedWallets {
			found := false
			for _, observed := range observation.Wallets {
				for _, bound := range p.Status.Admission.Dependencies {
					found = found ||
						bound.Kind == bitcoin.KindBitcoinWallet &&
							bound.Name == ref.Name &&
							bound.UID == observed.Wallet.UID &&
							bound.Fingerprint == observed.Wallet.Fingerprint
				}
			}
			if !found {
				return view, bitcoin.InitializationWaiting, reasonWalletIdentityUnavailable
			}
		}
		for _, wallet := range observation.Wallets {
			if !wallet.Ready {
				return view, bitcoin.InitializationWaiting, reasonWalletsPreparing
			}
			if wallet.MatureOutputs > view.mature[string(wallet.Wallet.UID)] {
				view.mature[string(wallet.Wallet.UID)] = wallet.MatureOutputs
			}
		}
	}
	if view.target == nil || view.height < 0 {
		return view, bitcoin.InitializationBlocked, reasonInitializationTargetUnavailable
	}
	return view, "", ""
}

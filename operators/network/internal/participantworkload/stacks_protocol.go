package participantworkload

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Fixed observation values match the regtest-pox4-pox5-v1 release profile.
var (
	protocolPollInterval      = time.Duration(foundation.ObservationPolicy().PollIntervalSeconds) * time.Second
	protocolRPCAllowance      = time.Duration(foundation.ObservationPolicy().RPCAllowanceSeconds) * time.Second
	protocolHeartbeatInterval = time.Duration(foundation.ObservationPolicy().HeartbeatIntervalSeconds) * time.Second
)

// errPoXUnreadableAtTip is a PoX read answered with HTTP 404 or 500 at the node's own canonical
// tip, unchanged across the read. It locates the failure, not its cause: Core answers 404 while
// its Stacks tip predates the PoX contract its Bitcoin tip selects, and 500 when it cannot
// compute PoX info, e.g. its PoX-1 to PoX-3 rejection-vote arithmetic overflowing on regtest.
var errPoXUnreadableAtTip = errors.New("PoX unreadable at the canonical Stacks tip")

// StacksProtocolRPC exposes only bounded native protocol observations.
type StacksProtocolRPC interface {
	// ChainView observes complete canonical Stacks and Bitcoin fork identity.
	ChainView(context.Context) (rpc.ChainView, error)
	// PoXAt observes native timing at the explicitly selected Stacks tip.
	PoXAt(context.Context, string) (rpc.PoX, error)
	// StackerSet observes the canonical prepared signer set at that same tip.
	StackerSet(context.Context, uint64, string) (rpc.StackerSet, error)
	// TenureSortition observes the Bitcoin block that elected a tenure.
	TenureSortition(context.Context, string) (rpc.TenureSortition, error)
}

// collectStacksProtocol requires a coherent canonical bracket and preserves height progress time.
func collectStacksProtocol(
	ctx context.Context,
	node StacksProtocolRPC,
	previous *api.StacksProtocolObservation,
	cycle *uint64,
	now time.Time,
) (*api.StacksProtocolObservation, error) {
	before, err := node.ChainView(ctx)
	if err != nil {
		return nil, err
	}
	// Boot contracts are not readable until a first anchored block is confirmed.
	// Preserve this positive startup observation separately from failed RPC reads.
	if before.StacksHeight == 0 && before.Tip == strings.Repeat("0", 64) &&
		before.ConsensusHash == strings.Repeat("0", 40) {
		if before.NetworkID != 0x80000000 || previous != nil && previous.HighestStacksHeight > 0 {
			return nil, fmt.Errorf("initial canonical view differs from startup identity")
		}
		after, err := node.ChainView(ctx)
		if err != nil || before != after {
			return nil, fmt.Errorf("initial canonical view unavailable or changed")
		}
		at := metav1.NewTime(now.UTC().Truncate(time.Second))
		advancedAt := at
		if previous != nil {
			advancedAt = previous.LastHeightAdvancedAt
		}
		return &api.StacksProtocolObservation{
			Reason:               api.ReasonAwaitingFirstAnchor,
			ObservedAt:           at,
			LastHeightAdvancedAt: advancedAt,
			NetworkID:            uint64(before.NetworkID),
			BurnHeight:           before.BurnHeight,
			StacksTip:            before.Tip,
			IndexBlockID:         before.IndexBlockID,
			BurnConsensusHash:    before.BurnConsensusHash,
			FullySynced:          before.FullySynced,
		}, nil
	}
	pox, err := node.PoXAt(ctx, before.IndexBlockID)
	if err != nil {
		return nil, poxFailure(ctx, node, before, err)
	}
	if before.NetworkID != 0x80000000 || before.BurnHeight != pox.BurnHeight {
		return nil, fmt.Errorf("native chain views disagree")
	}
	at := metav1.NewTime(now.UTC().Truncate(time.Second))
	observation := &api.StacksProtocolObservation{
		Available:            true,
		Reason:               reasonObserved,
		ObservedAt:           at,
		HighestStacksHeight:  before.StacksHeight,
		LastHeightAdvancedAt: at,
		NetworkID:            uint64(before.NetworkID),
		BurnHeight:           before.BurnHeight,
		StacksHeight:         before.StacksHeight,
		StacksTip:            before.Tip,
		IndexBlockID:         before.IndexBlockID,
		BurnConsensusHash:    before.BurnConsensusHash,
		FullySynced:          before.FullySynced,
		PoXContract:          pox.Contract,
		PoXBurnHeight:        pox.BurnHeight,
		RewardCycle:          pox.RewardCycle,
		CycleLength:          pox.CycleLength,
	}
	if previous != nil && before.StacksHeight <= previous.HighestStacksHeight {
		observation.HighestStacksHeight = previous.HighestStacksHeight
		observation.LastHeightAdvancedAt = previous.LastHeightAdvancedAt
	}
	observation.Tenure = observeTenure(ctx, node, previous, before.ConsensusHash)
	if cycle != nil {
		set, err := node.StackerSet(ctx, *cycle, before.IndexBlockID)
		if err != nil {
			return nil, err
		}
		observation.PreparedSet = &api.PreparedSignerSetObservation{
			Cycle:      *cycle,
			Available:  set.Available,
			Version:    set.Version,
			ObservedAt: at,
		}
		if set.Available {
			observation.PreparedSet.Threshold = set.Threshold.Integer.String()
			for _, signer := range set.Signers {
				observation.PreparedSet.Signers = append(
					observation.PreparedSet.Signers,
					api.PreparedSignerObservation{
						PublicKey:     signer.PublicKey,
						Weight:        uint64(signer.Weight),
						StackedAmount: signer.StackedAmount.Integer.String(),
					},
				)
			}
		}
	}
	after, err := node.ChainView(ctx)
	if err != nil {
		return nil, err
	}
	if before != after {
		return nil, fmt.Errorf("canonical view changed during observation")
	}
	return observation, nil
}

// poxFailure distinguishes errPoXUnreadableAtTip from other failed PoX reads.
func poxFailure(ctx context.Context, node StacksProtocolRPC, before rpc.ChainView, err error) error {
	var status *rpc.StatusError
	if !errors.As(err, &status) || status.Code != http.StatusNotFound && status.Code != http.StatusInternalServerError {
		return err
	}
	after, err := node.ChainView(ctx)
	if err != nil {
		return err
	}
	if before != after {
		return fmt.Errorf("canonical view changed during observation")
	}
	return errPoXUnreadableAtTip
}

// observeTenure reuses the immutable sortition of an unchanged tenure. A failed read omits it:
// the tenure is then unknown, which never establishes progress.
func observeTenure(
	ctx context.Context,
	node StacksProtocolRPC,
	previous *api.StacksProtocolObservation,
	consensus string,
) *api.StacksTenureObservation {
	if previous != nil && previous.Tenure != nil && previous.Tenure.ConsensusHash == consensus {
		return previous.Tenure.DeepCopy()
	}
	sortition, err := node.TenureSortition(ctx, consensus)
	if err != nil {
		return nil
	}
	return &api.StacksTenureObservation{
		ConsensusHash: sortition.ConsensusHash,
		BurnBlockHash: sortition.BurnBlockHash,
		BurnHeight:    sortition.BurnHeight,
	}
}

// observeStacksProtocol reads the exact Pod IP, then confirms the same process still owns it.
// Failures and PoX unreadable at the confirmed process's tip withdraw availability and keep the
// last successful observation's time and heights. The latter is a recorded diagnostic, not an
// error: the process is reachable and its workload need not converge again.
func (r *Reconciler) observeStacksProtocol(
	ctx context.Context,
	root *api.StacksNetwork,
	p *api.StacksNetworkParticipant,
	pod *corev1.Pod,
	state *api.ParticipantRuntimeStatus,
) (observeErr error) {
	defer func() {
		if observeErr != nil && state.Protocol != nil {
			state.Protocol.Available = false
			state.Protocol.Reason = api.ReasonObservationUnavailable
		}
	}()
	if root.Status.GenesisRef == nil || root.Status.GenesisRef.Kind != api.KindStacksGenesis || state.PodRef == nil ||
		state.ContainerID == "" ||
		net.ParseIP(pod.Status.PodIP) == nil {
		return fmt.Errorf("protocol target identity unavailable")
	}
	if p.Status.Runtime != nil && p.Status.Runtime.Protocol != nil {
		previous := p.Status.Runtime.Protocol
		if previous.Available && previous.PodUID == pod.UID && previous.ContainerID == state.ContainerID &&
			previous.ConfigurationDigest == state.ConfigurationDigest &&
			previous.GenesisUID == root.Status.GenesisRef.UID &&
			time.Since(previous.ObservedAt.Time) >= 0 &&
			time.Since(previous.ObservedAt.Time) < protocolPollInterval {
			state.Protocol = previous.DeepCopy()
			return nil
		}
	}
	var genesis api.StacksGenesis
	if err := r.Reader.Get(
		ctx,
		client.ObjectKey{Namespace: p.Namespace, Name: root.Status.GenesisRef.Name},
		&genesis,
	); err != nil {
		return err
	}
	if genesis.UID != root.Status.GenesisRef.UID || genesis.Spec.Source.NetworkUID != root.UID ||
		genesis.DeletionTimestamp != nil ||
		!metav1.IsControlledBy(&genesis, root) ||
		foundation.Digest(genesis.Spec.Chain) != root.Status.GenesisDigest {
		return fmt.Errorf("protocol genesis identity changed")
	}
	var cycle *uint64
	// Observe the next unfinished prepared-set gate; the cycle always comes from frozen genesis.
	for _, gate := range genesis.Spec.Bootstrap.Gates {
		if gate.Name != api.GatePrepareNakamoto && gate.Name != api.GatePrepareWaterfall {
			continue
		}
		completed := false
		if root.Status.Initialization != nil && root.Status.Initialization.GenesisUID == genesis.UID {
			for _, observed := range root.Status.Initialization.Gates {
				if observed.Name == gate.Name && observed.CompletedAt != nil {
					completed = true
				}
			}
		}
		if completed {
			continue
		}
		if gate.TargetCycle == nil || *gate.TargetCycle < 0 {
			return fmt.Errorf("prepared-set target cycle unavailable")
		}
		selected := uint64(*gate.TargetCycle)
		cycle = &selected
		break
	}
	endpoint := (&url.URL{Scheme: "http", Host: net.JoinHostPort(pod.Status.PodIP, "20443")}).String()
	var node StacksProtocolRPC
	var err error
	if r.ProtocolClient != nil {
		node, err = r.ProtocolClient(endpoint)
	} else {
		node, err = rpc.New(rpc.Config{Endpoint: endpoint, Timeout: protocolRPCAllowance, MaxResponseBytes: 2 << 20})
	}
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, protocolRPCAllowance)
	defer cancel()
	observed, err := collectStacksProtocol(bounded, node, state.Protocol, cycle, time.Now())
	if err != nil && !errors.Is(err, errPoXUnreadableAtTip) {
		return err
	}
	// The specific reason also requires that the observed process still owns the address.
	var current corev1.Pod
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(pod), &current); err != nil {
		return err
	}
	currentState := api.ParticipantRuntimeStatus{}
	observePod(&currentState, &current)
	if current.UID != pod.UID || current.Status.PodIP != pod.Status.PodIP || current.DeletionTimestamp != nil ||
		!podReady(&current) ||
		currentState.ContainerID != state.ContainerID ||
		current.Annotations[api.AnnotationConfigurationDigest] != state.ConfigurationDigest {
		return fmt.Errorf("protocol target process changed")
	}
	if err != nil {
		if state.Protocol != nil {
			state.Protocol.Available = false
			state.Protocol.Reason = reasonPoXUnreadableAtTip
		}
		return nil
	}
	observed.PodUID = pod.UID
	observed.ContainerID = state.ContainerID
	observed.ConfigurationDigest = state.ConfigurationDigest
	observed.GenesisUID = genesis.UID
	if p.Status.Runtime != nil && p.Status.Runtime.Protocol != nil {
		previous := p.Status.Runtime.Protocol
		if observed.ObservedAt.Sub(previous.ObservedAt.Time) < protocolHeartbeatInterval &&
			protocolObservationUnchanged(previous, observed) {
			state.Protocol = previous.DeepCopy()
			return nil
		}
	}
	state.Protocol = observed
	return nil
}

// protocolObservationUnchanged coalesces fresh reads without changing their source timestamps.
func protocolObservationUnchanged(previous, current *api.StacksProtocolObservation) bool {
	before, after := previous.DeepCopy(), current.DeepCopy()
	before.ObservedAt, after.ObservedAt = metav1.Time{}, metav1.Time{}
	if before.PreparedSet != nil {
		before.PreparedSet.ObservedAt = metav1.Time{}
	}
	if after.PreparedSet != nil {
		after.PreparedSet.ObservedAt = metav1.Time{}
	}
	return reflect.DeepEqual(before, after)
}

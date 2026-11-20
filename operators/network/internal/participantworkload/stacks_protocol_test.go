package participantworkload

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// nativeObservationFixture exposes deterministic public reads without a network connection.
type nativeObservationFixture struct {
	view        rpc.ChainView
	after       *rpc.ChainView
	reads       int
	unavailable bool
	fail        bool
	cycle       uint64
	sortitions  int
	noSortition bool
	poxErr      error
}

func (n *nativeObservationFixture) TenureSortition(_ context.Context, consensus string) (rpc.TenureSortition, error) {
	n.sortitions++
	if n.noSortition {
		return rpc.TenureSortition{}, fmt.Errorf("unavailable")
	}
	return rpc.TenureSortition{ConsensusHash: consensus, BurnBlockHash: strings.Repeat("7", 64), BurnHeight: 238}, nil
}

func (n *nativeObservationFixture) ChainView(context.Context) (rpc.ChainView, error) {
	n.reads++
	if n.fail {
		return rpc.ChainView{}, fmt.Errorf("unavailable")
	}
	if n.after != nil && n.reads%2 == 0 {
		return *n.after, nil
	}
	return n.view, nil
}

func (n *nativeObservationFixture) PoXAt(_ context.Context, tip string) (rpc.PoX, error) {
	if n.poxErr != nil {
		return rpc.PoX{}, n.poxErr
	}
	if tip != n.view.IndexBlockID {
		return rpc.PoX{}, fmt.Errorf("wrong tip")
	}
	return rpc.PoX{
		Contract:    "ST000000000000000000002AMW42H.pox-4",
		BurnHeight:  n.view.BurnHeight,
		RewardCycle: 12,
		CycleLength: 20,
	}, nil
}

func (n *nativeObservationFixture) StackerSet(_ context.Context, cycle uint64, tip string) (rpc.StackerSet, error) {
	n.cycle = cycle
	if tip != n.view.IndexBlockID {
		return rpc.StackerSet{}, fmt.Errorf("wrong tip")
	}
	return rpc.StackerSet{
		Available: !n.unavailable,
		Threshold: clarity.Uint(10),
		Signers: []rpc.PreparedSigner{
			{PublicKey: "02" + strings.Repeat("1", 64), Weight: 7, StackedAmount: clarity.Uint(100)},
		},
	}, nil
}

func TestCoherentProtocolObservationPreservesHeightProgressAcrossReorgs(t *testing.T) {
	now := time.Now()
	node := &nativeObservationFixture{
		view: rpc.ChainView{
			Info: rpc.Info{
				NetworkID:    0x80000000,
				BurnHeight:   240,
				StacksHeight: 30,
				Tip:          strings.Repeat("1", 64),
			},
			ConsensusHash:     strings.Repeat("2", 40),
			BurnConsensusHash: strings.Repeat("3", 40),
			IndexBlockID:      strings.Repeat("4", 64),
		},
	}
	first, err := collectStacksProtocol(context.Background(), node, nil, ptr.To(uint64(12)), now)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Available || !first.PreparedSet.Available || node.cycle != 12 ||
		first.PreparedSet.Signers[0].Weight != 7 {
		t.Fatal("prepared native set missing before Nakamoto")
	}
	node.view.Tip = strings.Repeat("5", 64)
	node.view.IndexBlockID = strings.Repeat("6", 64)
	second, err := collectStacksProtocol(context.Background(), node, first, ptr.To(uint64(12)), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !second.LastHeightAdvancedAt.Equal(&first.LastHeightAdvancedAt) || second.ObservedAt.Equal(&first.ObservedAt) {
		t.Fatal("same-height reorg refreshed height progress or lost fresh observation")
	}
	node.view.StacksHeight = 29
	lower, err := collectStacksProtocol(context.Background(), node, second, nil, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if lower.HighestStacksHeight != 30 || !lower.LastHeightAdvancedAt.Equal(&first.LastHeightAdvancedAt) {
		t.Fatal("height regression reset progress watermark")
	}
	node.view.StacksHeight = 31
	advanced, err := collectStacksProtocol(context.Background(), node, lower, nil, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if advanced.HighestStacksHeight != 31 || advanced.LastHeightAdvancedAt.Equal(&first.LastHeightAdvancedAt) {
		t.Fatal("strictly greater height did not advance progress")
	}
}

func TestTenureIsReadOncePerConsensusHashAndOmittedWhenUnavailable(t *testing.T) {
	now := time.Now()
	node := &nativeObservationFixture{
		view: rpc.ChainView{
			Info: rpc.Info{
				NetworkID:    0x80000000,
				BurnHeight:   240,
				StacksHeight: 30,
				Tip:          strings.Repeat("1", 64),
			},
			ConsensusHash:     strings.Repeat("2", 40),
			BurnConsensusHash: strings.Repeat("3", 40),
			IndexBlockID:      strings.Repeat("4", 64),
		},
	}
	first, err := collectStacksProtocol(context.Background(), node, nil, nil, now)
	want := api.StacksTenureObservation{
		ConsensusHash: strings.Repeat("2", 40), BurnBlockHash: strings.Repeat("7", 64), BurnHeight: 238,
	}
	if err != nil || first.Tenure == nil || *first.Tenure != want || node.sortitions != 1 {
		t.Fatalf("tenure = %+v, %v after %d reads", first.Tenure, err, node.sortitions)
	}
	node.view.StacksHeight, node.view.Tip = 31, strings.Repeat("5", 64)
	same, err := collectStacksProtocol(context.Background(), node, first, nil, now.Add(time.Second))
	if err != nil || same.Tenure == nil || *same.Tenure != want || node.sortitions != 1 {
		t.Fatal("a block in the same tenure read its sortition again")
	}
	node.view.ConsensusHash = strings.Repeat("8", 40)
	node.noSortition = true
	unknown, err := collectStacksProtocol(context.Background(), node, same, nil, now.Add(2*time.Second))
	if err != nil || !unknown.Available || unknown.Tenure != nil || node.sortitions != 2 {
		t.Fatal("an unreadable new tenure kept the previous tenure or withdrew the observation")
	}
}

func TestProtocolErrorsAndMidReadForkChangeDoNotRefreshPriorEvidence(t *testing.T) {
	node := &nativeObservationFixture{
		view: rpc.ChainView{Info: rpc.Info{NetworkID: 0x80000000, BurnHeight: 240, StacksHeight: 30}},
	}
	prior, err := collectStacksProtocol(context.Background(), node, nil, ptr.To(uint64(12)), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := prior.DeepCopy()
	changed := node.view
	changed.ConsensusHash = "different-consensus-same-header"
	node.after = &changed
	if _, err := collectStacksProtocol(
		context.Background(),
		node,
		prior,
		ptr.To(uint64(12)),
		time.Now().Add(time.Minute),
	); err == nil {
		t.Fatal("mixed fork read authorized")
	}
	node.after = nil
	node.fail = true
	if _, err := collectStacksProtocol(
		context.Background(),
		node,
		prior,
		nil,
		time.Now().Add(time.Minute),
	); err == nil {
		t.Fatal("RPC error accepted")
	}
	if !reflect.DeepEqual(prior, snapshot) {
		t.Fatal("failed observation freshened retained evidence")
	}
	node.fail = false
	node.unavailable = true
	waiting, err := collectStacksProtocol(
		context.Background(),
		node,
		prior,
		ptr.To(uint64(12)),
		time.Now().Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !waiting.Available || waiting.PreparedSet.Available {
		t.Fatal("explicit prepared-set waiting was mistaken for unavailable node")
	}
}

func TestProtocolHeartbeatDoesNotTriggerActorConfigurationDependencies(t *testing.T) {
	before := &api.ParticipantRuntimeStatus{
		ConfigurationDigest: "config",
		Protocol:            &api.StacksProtocolObservation{StacksHeight: 1},
	}
	after := before.DeepCopy()
	after.Protocol.StacksHeight = 2
	if actorRuntimeDependencyChanged(before, after) {
		t.Fatal("native telemetry triggered peer configuration reconciliation")
	}
	after.ConfigurationDigest = "new-config"
	if !actorRuntimeDependencyChanged(before, after) {
		t.Fatal("real actor configuration change was suppressed")
	}
}

// protocolTargetFixture is a ready Stacks node Pod, its frozen genesis and a minute-old observation.
type protocolTargetFixture struct {
	root    *api.StacksNetwork
	p       *api.StacksNetworkParticipant
	genesis *api.StacksGenesis
	pod     *corev1.Pod
	state   *api.ParticipantRuntimeStatus
	oldAt   metav1.Time
}

func newProtocolTarget() protocolTargetFixture {
	p := stacksParticipantFixture("StacksNode")
	root := &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: p.Namespace, UID: p.Spec.NetworkUID},
	}
	genesis := &api.StacksGenesis{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "genesis",
			Namespace:       p.Namespace,
			UID:             "genesis-uid",
			OwnerReferences: p.OwnerReferences,
		},
		Spec: api.StacksGenesisSpec{
			Source:    api.GenesisSource{NetworkUID: root.UID},
			Bootstrap: api.Bootstrap{Gates: []api.Gate{{Name: "PrepareNakamoto", TargetCycle: ptr.To(int64(12))}}},
		},
	}
	root.Status.GenesisRef = &common.Binding{Kind: "StacksGenesis", Name: genesis.Name, UID: genesis.UID}
	root.Status.GenesisDigest = digest(genesis.Spec.Chain)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "actor-0",
			Namespace:   p.Namespace,
			UID:         "original-pod",
			Labels:      Labels(p, "actor"),
			Annotations: map[string]string{"network.stacks.org/configuration-digest": "config"},
		},
		Status: corev1.PodStatus{
			PodIP:      "10.10.0.2",
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: actorContainer(p.Spec.Kind), ContainerID: "containerd://native"},
			},
		},
	}
	oldAt := metav1.NewTime(time.Now().Add(-time.Minute).UTC().Truncate(time.Second))
	state := &api.ParticipantRuntimeStatus{
		PodRef:              binding("Pod", pod),
		ContainerID:         "containerd://native",
		ConfigurationDigest: "config",
		Protocol: &api.StacksProtocolObservation{
			Available:            true,
			Reason:               reasonObserved,
			ObservedAt:           oldAt,
			LastHeightAdvancedAt: oldAt,
			HighestStacksHeight:  20,
			StacksHeight:         20,
			BurnHeight:           230,
		},
	}
	return protocolTargetFixture{root: root, p: p, genesis: genesis, pod: pod, state: state, oldAt: oldAt}
}

func TestProtocolObservationRejectsSameNamePodReplacementWithoutFreshening(t *testing.T) {
	f := newProtocolTarget()
	current := f.pod.DeepCopy()
	current.UID = "replacement-pod"
	node := &nativeObservationFixture{
		view: rpc.ChainView{Info: rpc.Info{NetworkID: 0x80000000, BurnHeight: 240, StacksHeight: 30}},
	}
	c := testClient(t, f.genesis, current)
	r := Reconciler{Reader: c, ProtocolClient: func(endpoint string) (StacksProtocolRPC, error) {
		if endpoint != "http://10.10.0.2:20443" {
			t.Fatal("observer used a load-balanced Service instead of exact Pod IP")
		}
		return node, nil
	}}
	if err := r.observeStacksProtocol(context.Background(), f.root, f.p, f.pod, f.state); err == nil {
		t.Fatal("same-name Pod replacement retained protocol authority")
	}
	if f.state.Protocol.Available || !f.state.Protocol.ObservedAt.Equal(&f.oldAt) ||
		!f.state.Protocol.LastHeightAdvancedAt.Equal(&f.oldAt) {
		t.Fatal("failed identity observation retained authority or freshened evidence")
	}
}

func TestPoXUnreadableAtTipRequiresAStable404Or500(t *testing.T) {
	view := rpc.ChainView{Info: rpc.Info{NetworkID: 0x80000000, BurnHeight: 240, StacksHeight: 30}}
	changed := view
	changed.BurnHeight = 241
	notFound := &rpc.StatusError{Code: http.StatusNotFound}
	serverError := &rpc.StatusError{Code: http.StatusInternalServerError}
	for name, c := range map[string]struct {
		poxErr     error
		tipMoved   bool
		unreadable bool
	}{
		"stable 404":    {poxErr: notFound, unreadable: true},
		"stable 500":    {poxErr: serverError, unreadable: true},
		"stable 503":    {poxErr: &rpc.StatusError{Code: http.StatusServiceUnavailable}},
		"transport":     {poxErr: fmt.Errorf("native RPC transport unavailable")},
		"deadline":      {poxErr: context.DeadlineExceeded},
		"404 tip moved": {poxErr: notFound, tipMoved: true},
		"500 tip moved": {poxErr: serverError, tipMoved: true},
	} {
		node := &nativeObservationFixture{view: view, poxErr: c.poxErr}
		if c.tipMoved {
			node.after = &changed
		}
		_, err := collectStacksProtocol(context.Background(), node, nil, nil, time.Now())
		if err == nil || errors.Is(err, errPoXUnreadableAtTip) != c.unreadable {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestPoXUnreadableAtTipKeepsTheLastSuccessfulObservation(t *testing.T) {
	ctx := context.Background()
	view := rpc.ChainView{Info: rpc.Info{NetworkID: 0x80000000, BurnHeight: 240, StacksHeight: 30}}
	observe := func(t *testing.T, f protocolTargetFixture, pod *corev1.Pod, poxErr error) error {
		t.Helper()
		node := &nativeObservationFixture{view: view, poxErr: poxErr}
		r := Reconciler{Reader: testClient(t, f.genesis, pod), ProtocolClient: func(string) (StacksProtocolRPC, error) {
			return node, nil
		}}
		return r.observeStacksProtocol(ctx, f.root, f.p, f.pod, f.state)
	}
	notFound := &rpc.StatusError{Code: http.StatusNotFound}
	for _, code := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		f := newProtocolTarget()
		want := f.state.Protocol.DeepCopy()
		want.Available, want.Reason = false, reasonPoXUnreadableAtTip
		if err := observe(t, f, f.pod, &rpc.StatusError{Code: code}); err != nil {
			t.Fatalf("HTTP %d: a confirmed process with unreadable PoX failed the observation: %v", code, err)
		}
		if !reflect.DeepEqual(f.state.Protocol, want) {
			t.Fatalf("HTTP %d: diagnostic changed retained evidence: %+v", code, f.state.Protocol)
		}
	}

	f := newProtocolTarget()
	if err := observe(t, f, f.pod, &rpc.StatusError{Code: http.StatusInternalServerError}); err != nil {
		t.Fatal(err)
	}
	if err := observe(t, f, f.pod, nil); err != nil {
		t.Fatal(err)
	}
	recovered := f.state.Protocol
	if !recovered.Available || recovered.Reason != reasonObserved || !recovered.ObservedAt.After(f.oldAt.Time) ||
		recovered.StacksHeight != 30 || recovered.HighestStacksHeight != 30 {
		t.Fatalf("PoX recovery did not restore a complete observation: %+v", recovered)
	}

	replaced := newProtocolTarget()
	current := replaced.pod.DeepCopy()
	current.UID = "replacement-pod"
	for name, c := range map[string]struct {
		pod *corev1.Pod
		err error
	}{
		"unavailable":          {replaced.pod, &rpc.StatusError{Code: http.StatusServiceUnavailable}},
		"deadline":             {replaced.pod, context.DeadlineExceeded},
		"replaced process 404": {current, notFound},
		"replaced process 500": {current, &rpc.StatusError{Code: http.StatusInternalServerError}},
	} {
		f := newProtocolTarget()
		want := f.state.Protocol.DeepCopy()
		want.Available, want.Reason = false, api.ReasonObservationUnavailable
		if err := observe(t, f, c.pod, c.err); err == nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(f.state.Protocol, want) {
			t.Fatalf("%s: %+v", name, f.state.Protocol)
		}
	}
}

func TestProtocolHeartbeatCoalescesOnlyUnchangedSuccessfulFacts(t *testing.T) {
	if protocolPollInterval != 2*time.Second || protocolRPCAllowance != 10*time.Second ||
		protocolHeartbeatInterval != 5*time.Second {
		t.Fatal("native observation timing diverges from release profile")
	}
	before := &api.StacksProtocolObservation{
		Available:            true,
		ObservedAt:           metav1.Now(),
		HighestStacksHeight:  30,
		LastHeightAdvancedAt: metav1.Now(),
		PreparedSet: &api.PreparedSignerSetObservation{
			Cycle:      12,
			Available:  true,
			ObservedAt: metav1.Now(),
		},
	}
	after := before.DeepCopy()
	after.ObservedAt = metav1.NewTime(before.ObservedAt.Add(time.Second))
	after.PreparedSet.ObservedAt = after.ObservedAt
	if !protocolObservationUnchanged(before, after) {
		t.Fatal("unchanged successful native read bypassed heartbeat coalescing")
	}
	after.PreparedSet.Available = false
	if protocolObservationUnchanged(before, after) {
		t.Fatal("native prepared-set withdrawal was delayed as routine heartbeat")
	}
	after = before.DeepCopy()
	after.LastHeightAdvancedAt = metav1.NewTime(before.LastHeightAdvancedAt.Add(time.Second))
	after.StacksHeight = 31
	if protocolObservationUnchanged(before, after) {
		t.Fatal("new protocol progress was delayed as unchanged observation")
	}
}

// TestFirstAnchorObservationDoesNotClaimPoXOrProtocolReadiness covers native pre-anchor startup.
func TestFirstAnchorObservationDoesNotClaimPoXOrProtocolReadiness(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	node := &nativeObservationFixture{
		view: rpc.ChainView{
			Info:              rpc.Info{NetworkID: 0x80000000, BurnHeight: 210, Tip: strings.Repeat("0", 64)},
			ConsensusHash:     strings.Repeat("0", 40),
			BurnConsensusHash: strings.Repeat("2", 40),
			IndexBlockID:      strings.Repeat("3", 64),
			FullySynced:       true,
		},
	}
	observed, err := collectStacksProtocol(context.Background(), node, nil, ptr.To(uint64(12)), now)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Available || observed.Reason != "AwaitingFirstAnchor" || observed.PoXContract != "" ||
		observed.PreparedSet != nil ||
		node.reads != 2 ||
		node.cycle != 0 ||
		observed.BurnHeight != 210 ||
		!observed.FullySynced {
		t.Fatalf("startup observation misclassified: %+v", observed)
	}
	previous := observed.DeepCopy()
	waiting, err := collectStacksProtocol(context.Background(), node, previous, nil, now.Add(time.Second))
	if err != nil || !waiting.LastHeightAdvancedAt.Equal(&previous.LastHeightAdvancedAt) ||
		waiting.ObservedAt.Equal(&previous.ObservedAt) {
		t.Fatalf("unchanged empty chain refreshed height progress: %+v %v", waiting, err)
	}
	previous.HighestStacksHeight = 1
	if _, err = collectStacksProtocol(context.Background(), node, previous, nil, now); err == nil {
		t.Fatal("lost established chainstate accepted as initial startup")
	}
	changed := node.view
	changed.BurnHeight++
	node.after = &changed
	if _, err = collectStacksProtocol(context.Background(), node, nil, nil, now); err == nil {
		t.Fatal("incoherent startup bracket accepted")
	}
	node.after = nil
	node.fail = true
	if _, err = collectStacksProtocol(context.Background(), node, nil, nil, now); err == nil {
		t.Fatal("failed native read accepted as first-anchor demand")
	}
}

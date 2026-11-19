package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	networkResource     = api.GroupVersion.WithResource(api.ResourceStacksNetwork)
	participantResource = api.GroupVersion.WithResource(api.ResourceStacksNetworkParticipant)
	genesisResource     = api.GroupVersion.WithResource(api.ResourceStacksGenesis)
	accountResource     = stacks.GroupVersion.WithResource(stacks.ResourceStacksAccount)
	executionResource   = bitcoin.GroupVersion.WithResource(bitcoin.ResourceBitcoinExecution)
)

// pollInterval paces every wait on operator status.
const pollInterval = 2 * time.Second

// cluster reads operator resources through the dynamic client and runs export Pods through the
// typed core client. It only acts in one namespace.
type cluster struct {
	dynamic   dynamic.Interface
	core      kubernetes.Interface
	exec      *rest.Config
	namespace string
	// streamer replaces the exec stream in tests; nil uses the cluster's exec API.
	streamer streamer
}

func newCluster(config *rest.Config, namespace string) (*cluster, error) {
	// Exec streams last as long as an archive takes; the capture deadline bounds them instead.
	exec := rest.CopyConfig(config)
	config = rest.CopyConfig(config)
	config.Timeout = 30 * time.Second
	d, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, errors.New("could not create Kubernetes client")
	}
	core, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.New("could not create Kubernetes client")
	}
	return &cluster{dynamic: d, core: core, exec: exec, namespace: namespace}, nil
}

// get reads one namespaced custom resource into its typed API struct.
func (c *cluster) get(ctx context.Context, gvr schema.GroupVersionResource, name string, into any) error {
	u, err := c.dynamic.Resource(gvr).Namespace(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read %s %s: %w", gvr.Resource, name, err)
	}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, into); err != nil {
		return fmt.Errorf("decode %s %s: %w", gvr.Resource, name, err)
	}
	return nil
}

// fatal marks an error that waiting cannot resolve.
type fatal struct{ error }

func (f fatal) Unwrap() error { return f.error }

// poll calls check until it reports done, fails fatally or the context ends. Other errors,
// such as a transient API failure, are retried and reported if the wait times out.
func poll(ctx context.Context, what string, check func() (bool, error)) error {
	var last error
	for {
		done, err := check()
		var f fatal
		switch {
		case errors.As(err, &f):
			return f.error
		case err != nil:
			last = err
		case done:
			return nil
		}
		select {
		case <-ctx.Done():
			if last != nil {
				return fmt.Errorf("timed out waiting for %s: %w", what, last)
			}
			return fmt.Errorf("timed out waiting for %s", what)
		case <-time.After(pollInterval):
		}
	}
}

// network reads the source network and confirms it is still the requested one.
func (c *cluster) network(ctx context.Context, uid string, name string) (*api.StacksNetwork, error) {
	var root api.StacksNetwork
	if err := c.get(ctx, networkResource, name, &root); err != nil {
		return nil, err
	}
	if string(root.UID) != uid || root.DeletionTimestamp != nil {
		return nil, fatal{errors.New("network UID differs or deletion is in progress")}
	}
	return &root, nil
}

// checkCapturable requires an initialized network that has not been asked to stop.
func checkCapturable(root *api.StacksNetwork) error {
	switch {
	case root.Spec.Operation == api.NetworkOperationStopped:
		return errors.New("network is already stopped; capture records its live tips before stopping it " +
			"(to retry the export of an earlier capture, set \"resume\": true)")
	case !meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionInitialized):
		return errors.New("network is not Initialized")
	case root.Status.GenesisRef == nil || root.Status.GenesisDigest == "":
		return errors.New("network has no frozen genesis")
	case root.Status.Bitcoin == nil || len(root.Status.Bitcoin.ExecutionRefs) == 0:
		return errors.New("network has no Bitcoin execution records")
	}
	return nil
}

// frozenGenesis reads the genesis the network published.
func (c *cluster) frozenGenesis(ctx context.Context, root *api.StacksNetwork) (*api.StacksGenesis, error) {
	var genesis api.StacksGenesis
	if err := c.get(ctx, genesisResource, root.Status.GenesisRef.Name, &genesis); err != nil {
		return nil, err
	}
	if genesis.UID != root.Status.GenesisRef.UID {
		return nil, errors.New("frozen genesis identity changed")
	}
	return &genesis, nil
}

// cohort reads the frozen initial cohort with its participants and accounts.
func (c *cluster) cohort(ctx context.Context, root *api.StacksNetwork) ([]member, error) {
	genesis, err := c.frozenGenesis(ctx, root)
	if err != nil {
		return nil, err
	}
	members := make([]member, 0, len(genesis.Spec.Bootstrap.Requirements))
	for _, req := range genesis.Spec.Bootstrap.Requirements {
		m := member{requirement: req}
		if err := c.get(ctx, participantResource, req.Participant.Name, &m.participant); err != nil {
			return nil, err
		}
		if m.participant.UID != req.Participant.UID || m.participant.Spec.NetworkUID != root.UID {
			return nil, fmt.Errorf("participant %s identity changed", req.Participant.Name)
		}
		for _, b := range accountBindings(req) {
			var a stacks.StacksAccount
			if err := c.get(ctx, accountResource, b.Name, &a); err != nil {
				return nil, err
			}
			if a.UID != b.UID {
				return nil, fmt.Errorf("account %s identity changed", b.Name)
			}
			m.accounts = append(m.accounts, a)
		}
		members = append(members, m)
	}
	return members, nil
}

// fundedKeysImported requires an imported key on every account the frozen genesis pins as a
// source of its balances. An account funded only by spec.genesis.allocations belongs to no cohort
// participant, yet its address is in the genesis digest: regenerated on restore, it would change
// the digest after the source has been stopped.
func (c *cluster) fundedKeysImported(ctx context.Context, root *api.StacksNetwork) error {
	genesis, err := c.frozenGenesis(ctx, root)
	if err != nil {
		return err
	}
	var funded []stacks.StacksAccount
	for _, b := range genesis.Spec.Source.Dependencies {
		if b.Kind != stacks.KindStacksAccount {
			continue
		}
		var a stacks.StacksAccount
		if err := c.get(ctx, accountResource, b.Name, &a); err != nil {
			return err
		}
		if a.UID != b.UID {
			return fmt.Errorf("account %s identity changed", b.Name)
		}
		funded = append(funded, a)
	}
	return refuseGenerated(generatedKeys(funded))
}

// observation is one status-reported chain view and the time the operator made it.
type observation struct {
	source     string
	observedAt time.Time
	tip        string
}

// bitcoinView reads the Bitcoin execution observation of every selected Bitcoin node. The network
// keeps the records of removed nodes, whose observations no longer advance; they are skipped, as
// the operator's own pause acknowledgement does once they are drained.
func (c *cluster) bitcoinView(
	ctx context.Context,
	root *api.StacksNetwork,
) (api.SnapshotBitcoinTip, []observation, error) {
	var tip api.SnapshotBitcoinTip
	var seen []observation
	active := activeParticipants(root)
	for _, ref := range root.Status.Bitcoin.ExecutionRefs {
		var e bitcoin.BitcoinExecution
		if err := c.get(ctx, executionResource, ref.Name, &e); err != nil {
			return tip, nil, err
		}
		if e.UID != ref.UID {
			return tip, nil, fatal{fmt.Errorf("bitcoin execution %s identity changed", ref.Name)}
		}
		if !active[string(e.Spec.Participant.UID)] {
			continue
		}
		o := e.Status.Observation
		if o == nil {
			return tip, nil, fmt.Errorf("bitcoin execution %s has no observation", ref.Name)
		}
		seen = append(seen, observation{
			source:     "bitcoin/" + ref.Name,
			observedAt: o.ObservedAt.Time,
			tip:        fmt.Sprintf("%d/%s", o.Height, o.Tip),
		})
		if len(seen) == 1 || o.Height < tip.Height {
			tip = api.SnapshotBitcoinTip{Height: o.Height, BlockHash: o.Tip}
		}
	}
	if len(seen) == 0 {
		return tip, nil, fatal{errors.New("the network has no selected Bitcoin node")}
	}
	return tip, seen, nil
}

// activeParticipants returns the UIDs of the participants the network still selects.
func activeParticipants(root *api.StacksNetwork) map[string]bool {
	selected := map[string]bool{}
	for _, entry := range root.Spec.Participants {
		selected[entry.Name] = true
	}
	active := map[string]bool{}
	for _, id := range root.Status.Identities {
		if selected[id.Name] && !id.Removing {
			active[string(id.UID)] = true
		}
	}
	return active
}

// stacksView reads the protocol observation of every Stacks node in the cohort.
func (c *cluster) stacksView(
	ctx context.Context,
	members []member,
) (api.SnapshotStacksTip, []observation, error) {
	var tip api.SnapshotStacksTip
	var seen []observation
	for _, m := range members {
		if m.requirement.Kind != api.ParticipantStacksNode {
			continue
		}
		var p api.StacksNetworkParticipant
		if err := c.get(ctx, participantResource, m.participant.Name, &p); err != nil {
			return tip, nil, err
		}
		if p.UID != m.participant.UID {
			return tip, nil, fatal{fmt.Errorf("participant %s identity changed", m.participant.Name)}
		}
		if p.Status.Runtime == nil || p.Status.Runtime.Protocol == nil {
			return tip, nil, fmt.Errorf("%s has no protocol observation", p.Spec.ParticipantName)
		}
		o := p.Status.Runtime.Protocol
		seen = append(seen, observation{
			source:     "stacks/" + p.Spec.ParticipantName,
			observedAt: o.ObservedAt.Time,
			tip:        o.IndexBlockID,
		})
		tip = api.SnapshotStacksTip{
			Height:       int64(o.StacksHeight), // #nosec G115 -- Stacks heights stay far below 2^63.
			BlockHash:    o.StacksTip,
			IndexBlockID: o.IndexBlockID,
			RewardCycle:  int64(o.RewardCycle), // #nosec G115 -- reward cycles stay far below 2^63.
		}
	}
	if len(seen) == 0 {
		return tip, nil, fatal{errors.New("the initial cohort has no Stacks node")}
	}
	return tip, seen, nil
}

// patchOperation sets the network operation, with any annotations in the same patch. The UID in
// the patch makes the API server refuse it for a recreated network of the same name.
func (c *cluster) patchOperation(
	ctx context.Context,
	root *api.StacksNetwork,
	op api.NetworkOperation,
	annotations map[string]string,
) error {
	metadata := map[string]any{"uid": root.UID}
	if len(annotations) > 0 {
		metadata["annotations"] = annotations
	}
	patch, err := json.Marshal(map[string]any{
		"metadata": metadata,
		"spec":     map[string]any{"operation": op},
	})
	if err != nil {
		return err
	}
	_, err = c.dynamic.Resource(networkResource).
		Namespace(c.namespace).
		Patch(ctx, root.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("set network operation %s: %w", op, err)
	}
	return nil
}

// waitPhase waits until the network reports a phase.
func (c *cluster) waitPhase(ctx context.Context, uid, name string, phase api.NetworkPhase) error {
	return poll(ctx, "network phase "+string(phase), func() (bool, error) {
		root, err := c.network(ctx, uid, name)
		if err != nil {
			return false, err
		}
		return root.Status.Phase == phase, nil
	})
}

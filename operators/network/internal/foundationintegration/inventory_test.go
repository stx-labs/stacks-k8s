//go:build integration

package foundationintegration

import (
	"context"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// afterList runs once after the next successful List, making its snapshot stale.
type afterList struct {
	client.Reader
	once func()
}

func (a *afterList) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	err := a.Reader.List(ctx, list, opts...)
	if err == nil && a.once != nil {
		run := a.once
		a.once = nil
		run()
	}
	return err
}

// coreNetwork declares one inline Bitcoin node participant named core.
func coreNetwork(ns string) *api.StacksNetwork {
	return &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: ns},
		Spec: api.StacksNetworkSpec{
			Operation: "Paused",
			Participants: []api.Participant{{
				Name:       "core",
				Kind:       "BitcoinNode",
				Definition: api.Definition{Inline: &api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{}}},
			}},
		},
	}
}

func requireFailed(t *testing.T, root *api.StacksNetwork, reason string) {
	t.Helper()
	condition := meta.FindStatusCondition(root.Status.Conditions, "Resolved")
	if root.Status.Phase != "Failed" || condition == nil || condition.Reason != reason {
		t.Fatalf("wanted terminal %s, got %+v", reason, root.Status)
	}
}

// verifyParticipantInventory covers the namespace-wide participant read used by topology
// passes: identity checks still apply to every expected name, and the snapshot never
// overwrites concurrent changes.
func verifyParticipantInventory(t *testing.T, ctx context.Context, c client.Client, scheme *runtime.Scheme) {
	for _, mode := range []string{"unrelated", "replaced", "deleting", "concurrent"} {
		t.Run(mode, func(t *testing.T) {
			ns := "inventory-" + mode
			if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
				t.Fatal(err)
			}
			reader := &afterList{Reader: c}
			r := &foundation.Reconciler{Client: c, Reader: reader, Scheme: scheme}
			root := coreNetwork(ns)
			if err := c.Create(ctx, root); err != nil {
				t.Fatal(err)
			}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(root)}
			driveRoot(t, ctx, c, r, request, root)
			p := participant(t, ctx, c, root, "core")
			requireReason(t, p, "Admitted")
			switch mode {
			case "unrelated":
				// An unowned participant at an unexpected name shares the namespace inventory.
				unrelated := &api.StacksNetworkParticipant{
					ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: ns},
					Spec: api.StacksNetworkParticipantSpec{
						NetworkUID:      "foreign",
						ParticipantName: "core",
						Kind:            "BitcoinNode",
					},
				}
				if err := c.Create(ctx, unrelated); err != nil {
					t.Fatal(err)
				}
				driveRoot(t, ctx, c, r, request, root)
				requireReason(t, participant(t, ctx, c, root, "core"), "Admitted")
				if root.Status.Phase == "Failed" || len(root.Status.Identities) != 1 ||
					root.Status.Identities[0].UID != p.UID {
					t.Fatalf("unrelated participant affected the network: %+v", root.Status)
				}
				current := &api.StacksNetworkParticipant{}
				if err := c.Get(ctx, client.ObjectKeyFromObject(unrelated), current); err != nil ||
					current.ResourceVersion != unrelated.ResourceVersion {
					t.Fatalf("unrelated participant was modified: %v", err)
				}
			case "replaced":
				// A foreign object at the expected name is not mistaken for the participant.
				if err := c.Delete(ctx, p); err != nil {
					t.Fatal(err)
				}
				foreign := &api.StacksNetworkParticipant{
					ObjectMeta: metav1.ObjectMeta{Name: p.Name, Namespace: ns},
					Spec: api.StacksNetworkParticipantSpec{
						NetworkUID:      root.UID,
						ParticipantName: "core",
						Kind:            "BitcoinNode",
					},
				}
				if err := c.Create(ctx, foreign); err != nil {
					t.Fatal(err)
				}
				driveRoot(t, ctx, c, r, request, root)
				requireFailed(t, root, "InstanceIdentityChanged")
			case "deleting":
				controllerutil.AddFinalizer(p, "test.stacks.org/hold")
				updateObject(t, ctx, c, p)
				if err := c.Delete(ctx, p); err != nil {
					t.Fatal(err)
				}
				driveRoot(t, ctx, c, r, request, root)
				requireFailed(t, root, "InstanceIdentityChanged")
				held := participant(t, ctx, c, root, "core")
				controllerutil.RemoveFinalizer(held, "test.stacks.org/hold")
				if err := c.Update(ctx, held); err != nil && !apierrors.IsNotFound(err) {
					t.Fatal(err)
				}
			case "concurrent":
				// A change after the pass's inventory read must surface as a conflict, not be lost.
				root.Spec.Participants[0].Control = &api.Control{Suspended: ptr.To(true)}
				updateObject(t, ctx, c, root)
				reader.once = func() {
					current := participant(t, ctx, c, root, "core")
					current.Labels = map[string]string{"test.stacks.org/concurrent": "kept"}
					updateObject(t, ctx, c, current)
				}
				if _, err := r.Reconcile(ctx, request); !apierrors.IsConflict(err) {
					t.Fatalf("stale inventory write was not rejected: %v", err)
				}
				driveRoot(t, ctx, c, r, request, root)
				got := participant(t, ctx, c, root, "core")
				if !ptr.Deref(got.Spec.Control.Suspended, false) || got.Labels["test.stacks.org/concurrent"] != "kept" {
					t.Fatalf("control or concurrent change lost: %+v %+v", got.Spec.Control, got.Labels)
				}
			}
		})
	}
}

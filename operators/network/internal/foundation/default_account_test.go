package foundation

import (
	"context"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var accountResource = schema.GroupResource{Group: "stacks.stacks.org", Resource: "stacksaccounts"}

func defaultAccountFixture(t *testing.T) (*runtime.Scheme, *stacks.StacksFaucet, client.ObjectKey) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := stacks.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	owner := &stacks.StacksFaucet{
		ObjectMeta: metav1.ObjectMeta{Name: "faucet", Namespace: "test", UID: "faucet-uid"},
		Spec: stacks.StacksFaucetSpec{
			TargetNodeRef:      &common.NameRef{Name: "node"},
			MaxRequestMicroSTX: ptr.To(common.Amount("1")),
		},
	}
	return scheme, owner, client.ObjectKey{Namespace: owner.Namespace, Name: DefaultAccountName(owner.Name, "account")}
}

// generatedAccount is a resolved default account controlled by ownerUID.
func generatedAccount(key client.ObjectKey, uid, ownerUID types.UID, deleting bool) *stacks.StacksAccount {
	a := &stacks.StacksAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
			UID:       uid,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "stacks.stacks.org/v1alpha2",
				Kind:       "StacksFaucet",
				Name:       "faucet",
				UID:        ownerUID,
				Controller: ptr.To(true),
			}},
		},
		Spec: stacks.StacksAccountSpec{Key: &common.KeySource{Generate: ptr.To(true)}},
	}
	resolveAccount(a, string(uid))
	if deleting {
		a.Finalizers = []string{"test/hold"}
		a.DeletionTimestamp = ptr.To(metav1.Now())
	}
	return a
}

func resolveAccount(a *stacks.StacksAccount, digest string) {
	a.Status = common.ResolutionStatus{
		ObservedGeneration: a.Generation,
		Identity:           &common.PublicIdentity{Address: "public-" + digest},
		Digest:             digest,
		CredentialsRef:     &common.SecretKeyRef{Name: "key", Key: "privateKey"},
		CredentialsUID:     "key-uid",
		Conditions:         []metav1.Condition{{Type: common.ConditionResolved, Status: metav1.ConditionTrue}},
	}
}

func TestEnsureDefaultAccountChecksCacheBeforeCreate(t *testing.T) {
	scheme, owner, key := defaultAccountFixture(t)
	owned := generatedAccount(key, "account-uid", owner.UID, false)
	foreign := generatedAccount(key, "account-uid", "other-uid", false)
	deleting := generatedAccount(key, "account-uid", owner.UID, true)
	notFound := apierrors.NewNotFound(accountResource, key.Name)
	unavailable := apierrors.NewServiceUnavailable("cache unavailable")
	for _, tc := range []struct {
		name     string
		existing *stacks.StacksAccount
		// firstGet replaces the first (cached) read result.
		firstGet error
		wantErr  func(error) bool
		creates  int
	}{
		{name: "existing", existing: owned, creates: 0},
		{name: "foreign", existing: foreign, wantErr: isOwnershipErr, creates: 0},
		{name: "deleting", existing: deleting, wantErr: isOwnershipErr, creates: 0},
		{name: "missing", creates: 1},
		{name: "read-error", firstGet: unavailable, wantErr: apierrors.IsServiceUnavailable, creates: 0},
		{name: "read-error-existing", existing: owned, firstGet: unavailable, wantErr: apierrors.IsServiceUnavailable},
		{name: "stale-miss-owned", existing: owned, firstGet: notFound, creates: 1},
		{name: "stale-miss-foreign", existing: foreign, firstGet: notFound, wantErr: isOwnershipErr, creates: 1},
		{name: "stale-miss-deleting", existing: deleting, firstGet: notFound, wantErr: isOwnershipErr, creates: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tc.existing != nil {
				builder = builder.WithObjects(tc.existing.DeepCopy())
			}
			gets, creates := 0, 0
			var createErr error
			c := interceptor.NewClient(builder.Build(), interceptor.Funcs{
				Get: func(
					ctx context.Context,
					w client.WithWatch,
					k client.ObjectKey,
					obj client.Object,
					opts ...client.GetOption,
				) error {
					gets++
					if gets == 1 && tc.firstGet != nil {
						return tc.firstGet
					}
					return w.Get(ctx, k, obj, opts...)
				},
				Create: func(ctx context.Context, w client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					creates++
					createErr = w.Create(ctx, obj, opts...)
					return createErr
				},
			})
			err := EnsureDefaultAccount(t.Context(), c, scheme, owner, key.Name)
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !tc.wantErr(err) {
				t.Fatalf("unexpected error: %v", err)
			}
			if creates != tc.creates {
				t.Fatalf("creates = %d, want %d", creates, tc.creates)
			}
			if tc.existing != nil && creates > 0 && !apierrors.IsAlreadyExists(createErr) {
				t.Fatalf("racing create did not observe AlreadyExists: %v", createErr)
			}
			if tc.existing == nil && tc.firstGet == nil {
				var created stacks.StacksAccount
				if err := c.Get(t.Context(), key, &created); err != nil {
					t.Fatal(err)
				}
				if !ownedUID(&created, owner.UID) || !ptr.Deref(created.Spec.Key.Generate, false) {
					t.Fatalf("created account not owned or not generated: %+v", created)
				}
			}
		})
	}
}

func isOwnershipErr(err error) bool {
	return err != nil && !apierrors.IsNotFound(err) && !apierrors.IsAlreadyExists(err) &&
		err.Error() == "owned output unavailable or foreign: faucet-account"
}

// TestStaleDefaultAccountCacheHitFailsFreshAdmission gives the cached client a stale account
// snapshot while the live (uncached) reader observes the current server state.
func TestStaleDefaultAccountCacheHitFailsFreshAdmission(t *testing.T) {
	for _, mode := range []string{"absent", "deleting", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			scheme, owner, key := defaultAccountFixture(t)
			cachedAccount := generatedAccount(key, "cached-uid", owner.UID, false)
			credentials := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "key", Namespace: "test", UID: "key-uid"}}
			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner.DeepCopy(), credentials)
			switch mode {
			case "deleting":
				builder = builder.WithObjects(generatedAccount(key, "cached-uid", owner.UID, true))
			case "replaced":
				builder = builder.WithObjects(generatedAccount(key, "live-uid", owner.UID, false))
			}
			live := builder.Build()
			stale, creates := true, 0
			cached := interceptor.NewClient(live, interceptor.Funcs{
				Get: func(
					ctx context.Context,
					w client.WithWatch,
					k client.ObjectKey,
					obj client.Object,
					opts ...client.GetOption,
				) error {
					if a, ok := obj.(*stacks.StacksAccount); ok && stale && k == key {
						cachedAccount.DeepCopyInto(a)
						return nil
					}
					return w.Get(ctx, k, obj, opts...)
				},
				Create: func(ctx context.Context, w client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					creates++
					obj.SetUID("created-uid") // The fake API server does not assign UIDs.
					return w.Create(ctx, obj, opts...)
				},
			})
			r := Reconciler{Client: cached, Reader: live, Scheme: scheme}
			root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "root"}}
			entry := api.Participant{
				Name:       "faucet",
				Kind:       api.ParticipantStacksFaucet,
				Definition: api.Definition{Ref: &common.NameRef{Name: owner.Name}},
			}
			instance := &api.StacksNetworkParticipant{
				ObjectMeta: metav1.ObjectMeta{Name: "faucet", Namespace: "test", UID: "instance"},
			}
			node := &candidate{
				instance: &api.StacksNetworkParticipant{
					Spec: api.StacksNetworkParticipantSpec{Kind: api.ParticipantStacksNode},
				},
				configuration: api.Configuration{StacksNode: &stacks.StacksNodeSpec{}},
			}
			// pass composes and validates as at admission (cached reader) or freeze (fresh reader).
			pass := func(reader client.Reader) (*candidate, error) {
				c, err := r.compose(ctx, reader, root, entry, instance)
				if err != nil {
					t.Fatal(err)
				}
				return c, c.validate(ctx, reader, map[string]*candidate{entry.Name: c, "node": node})
			}
			prior, err := pass(r.Client)
			if err != nil {
				t.Fatal(err)
			}
			fresh, freshErr := pass(r.Reader)
			if creates != 0 {
				t.Fatalf("cache hit created account %d times", creates)
			}
			if freshErr == nil && equal(fresh.dependencies, prior.dependencies) {
				t.Fatal("fresh admission accepted stale cached identity")
			}
			check := newDependencyCheck(r.Reader, root)
			if err := check.validate(ctx, accountBindings(prior.dependencies)); err == nil {
				t.Fatal("fresh dependency check accepted stale cached identity")
			}

			// Cache convergence: later reconciliation recovers a live, owned identity.
			stale = false
			if mode == "deleting" {
				if err := EnsureDefaultAccount(ctx, cached, scheme, owner, key.Name); !isOwnershipErr(err) {
					t.Fatalf("deleting account not rejected: %v", err)
				}
				var a stacks.StacksAccount
				if err := live.Get(ctx, key, &a); err != nil {
					t.Fatal(err)
				}
				a.Finalizers = nil
				if err := live.Update(ctx, &a); err != nil {
					t.Fatal(err)
				}
			}
			if err := EnsureDefaultAccount(ctx, cached, scheme, owner, key.Name); err != nil {
				t.Fatal(err)
			}
			want := map[string]int{"absent": 1, "deleting": 1, "replaced": 0}[mode]
			if creates != want {
				t.Fatalf("recovery creates = %d, want %d", creates, want)
			}
			var current stacks.StacksAccount
			if err := live.Get(ctx, key, &current); err != nil {
				t.Fatal(err)
			}
			if current.UID == cachedAccount.UID || !ownedUID(&current, owner.UID) {
				t.Fatalf("recovered account has stale or foreign identity: %s", current.UID)
			}
			if current.Status.Identity == nil {
				// Stand in for the identity controller resolving the new account.
				resolveAccount(&current, string(current.UID))
				if err := live.Update(ctx, &current); err != nil {
					t.Fatal(err)
				}
			}
			recovered, err := pass(r.Reader)
			if err != nil {
				t.Fatal(err)
			}
			check = newDependencyCheck(r.Reader, root) // A new pass must not reuse stale observations.
			if err := check.validate(ctx, accountBindings(recovered.dependencies)); err != nil {
				t.Fatalf("recovered identity rejected: %v", err)
			}
		})
	}
}

func accountBindings(bindings []common.Binding) []common.Binding {
	var accounts []common.Binding
	for _, b := range bindings {
		if b.Kind == stacks.KindStacksAccount {
			accounts = append(accounts, b)
		}
	}
	return accounts
}

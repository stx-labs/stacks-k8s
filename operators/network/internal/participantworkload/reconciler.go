package participantworkload

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// actorWorkers reconciles independent participants in parallel. Each Stacks node poll runs
// the full reconcile, so a serial pass over every node must stay within the observation
// freshness window for prepared-set gates to see the whole cohort fresh at once.
const actorWorkers = 8

// Reconciler owns one actor kind's workload facts, never admission or protocol mutations.
type Reconciler struct {
	// Kind opts into one actor domain; omission retains the qualified Bitcoin controller.
	Kind api.ParticipantKind
	// Client writes owned workloads and minimal runtime status.
	Client client.Client
	// Reader must be uncached for lifecycle, admitted identity and termination checks.
	Reader client.Reader
	// ResolverImage provides the scoped configuration entrypoint.
	ResolverImage string
	// ObserverImage pins the Bitcoin sidecar independently of resolver/operator rollouts.
	ObserverImage string
	// ProtocolClient optionally supplies the bounded native read-only observer.
	ProtocolClient func(string) (StacksProtocolRPC, error)
	// BeforeStop drains the node's control executor before terminal shutdown or disposal.
	// It may be nil only when this actor has no mutation control worker.
	BeforeStop func(context.Context, *api.StacksNetworkParticipant) (bool, error)
	// candidateConfiguration is confined to a local support-preparation reconciler copy.
	candidateConfiguration *foundation.CandidateConfiguration
	// converged lets ready Stacks node polls skip convergence; nil always converges.
	converged *convergenceMemory
}

// kind preserves Bitcoin registration compatibility while allowing explicit actor domains.
func (r *Reconciler) kind() api.ParticipantKind {
	if r.Kind == "" {
		return api.ParticipantBitcoinNode
	}
	return r.Kind
}

// fieldManager isolates each actor domain's minimal status ownership.
func (r *Reconciler) fieldManager() string {
	return "stacks-network-domain-" + strings.ToLower(string(r.kind()))
}

// finalizer scopes terminal actor lifecycle to its corresponding domain controller.
func (r *Reconciler) finalizer() string {
	if r.kind() == api.ParticipantBitcoinNode {
		return Finalizer
	}
	return "network.stacks.org/" + strings.ToLower(string(r.kind())) + "-workload"
}

// Reconcile converges actor resources only while current admission permits activation.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	epoch := r.converged.epoch(request.NamespacedName)
	path := pathConverge
	defer observePass(r.kind(), &path, time.Now())
	var p api.StacksNetworkParticipant
	if err := r.Reader.Get(ctx, request.NamespacedName, &p); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if p.Spec.Kind != r.kind() {
		return ctrl.Result{}, nil
	}
	state := api.ParticipantRuntimeStatus{ObservedGeneration: p.Generation}
	if p.Status.Runtime != nil {
		state = *p.Status.Runtime.DeepCopy()
		state.ObservedGeneration = p.Generation
	}
	if p.Spec.Kind == api.ParticipantStacksNode && state.Protocol != nil {
		state.Protocol.Available = false
		state.Protocol.Reason = reasonActorUnavailable
	}
	conditions := ownConditions(p.Status.Conditions)
	// held is the runtime an established placement error was recorded with. Until that Pod
	// recovers, status keeps reporting it, so no later pass can record a changed workload.
	var held *api.ParticipantRuntimeStatus
	finish := func(status metav1.ConditionStatus, reason, message string) (ctrl.Result, error) {
		meta.SetStatusCondition(
			&conditions,
			metav1.Condition{
				Type:               api.ConditionWorkloadReady,
				Status:             status,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: p.Generation,
			},
		)
		out := api.ParticipantStatus{Runtime: published(&state, held), Conditions: conditions}
		result := ctrl.Result{}
		if p.Spec.Kind == api.ParticipantStacksNode && reason == reasonReady {
			result.RequeueAfter = protocolPollInterval
			if state.Protocol != nil && state.Protocol.Available {
				due := time.Until(state.Protocol.ObservedAt.Add(protocolHeartbeatInterval))
				if due > 0 && due < result.RequeueAfter {
					result.RequeueAfter = due
				}
			}
		}
		if retriesUnwatched(reason) {
			result.RequeueAfter = 5 * time.Second
		}
		if reflect.DeepEqual(p.Status.Runtime, out.Runtime) &&
			reflect.DeepEqual(ownConditions(p.Status.Conditions), conditions) {
			return result, nil
		}
		return result, participantstatus.Apply(ctx, r.Client, &p, out, r.fieldManager())
	}
	var root api.StacksNetwork
	err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: "network"}, &root)
	if err != nil && !apierrors.IsNotFound(err) {
		return finish(metav1.ConditionUnknown, reasonControlUnavailable, "Current network state could not be read")
	}
	removing := p.DeletionTimestamp != nil || apierrors.IsNotFound(err) || root.UID != p.Spec.NetworkUID ||
		root.DeletionTimestamp != nil ||
		!selected(&root, &p)
	stopping := removing || root.Spec.Operation == api.NetworkOperationStopped
	suspended := false
	for _, entry := range root.Spec.Participants {
		if entry.Name == p.Spec.ParticipantName && entry.Control != nil {
			suspended = ptr.Deref(entry.Control.Suspended, false)
		}
	}
	if stopping || suspended {
		var reason string
		var err error
		if stopping {
			reason, err = r.stopActor(ctx, &p, &state, removing)
		} else {
			reason, err = r.shutdown(ctx, &p, &state, false)
		}
		if err != nil {
			return finish(metav1.ConditionUnknown, api.ReasonTerminationUnknown, err.Error())
		}
		if reason == reasonDrainingControl {
			return finish(metav1.ConditionFalse, reason, "Waiting for control executor drainage before actor shutdown")
		}
		if reason == api.ReasonTerminated && removing && controllerutil.ContainsFinalizer(&p, r.finalizer()) {
			if _, err := finish(
				metav1.ConditionFalse,
				api.ReasonTerminated,
				"Actor process termination confirmed",
			); err != nil {
				return ctrl.Result{}, err
			}
			base := p.DeepCopy()
			controllerutil.RemoveFinalizer(&p, r.finalizer())
			return ctrl.Result{}, r.Client.Patch(
				ctx,
				&p,
				client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}),
			)
		}
		if reason == api.ReasonTerminated {
			if suspended && !stopping {
				reason = reasonSuspended
			} else {
				reason = api.ReasonStopped
			}
		}
		return finish(
			metav1.ConditionFalse,
			reason,
			"Actor shutdown retains process identity until termination is confirmed",
		)
	}
	if !controllerutil.ContainsFinalizer(&p, r.finalizer()) {
		base := p.DeepCopy()
		controllerutil.AddFinalizer(&p, r.finalizer())
		if err := r.Client.Patch(
			ctx,
			&p,
			client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}),
		); err != nil {
			return ctrl.Result{}, err
		}
	}
	if pod := r.convergedPod(ctx, &root, &p, &state); pod != nil {
		path = pathObserve
		if err := r.observeStacksProtocol(ctx, &root, &p, pod, &state); err != nil {
			r.converged.forget(request.NamespacedName)
		}
		return finish(metav1.ConditionTrue, reasonReady, readyMessage)
	}
	if err := r.authorized(ctx, &root, &p); err != nil {
		return finish(metav1.ConditionFalse, api.ReasonAdmissionUnavailable, err.Error())
	}
	if root.Spec.Operation == api.NetworkOperationPaused && len(state.WorkloadRefs) == 0 {
		var existing appsv1.StatefulSet
		err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: Name(&p, actorPurpose)}, &existing)
		if apierrors.IsNotFound(err) {
			return finish(metav1.ConditionFalse, reasonStartupPaused, "Network pause holds actor activation")
		}
		if err != nil || !owned(&existing, &p) {
			return finish(
				metav1.ConditionUnknown,
				reasonRuntimeIdentityUnavailable,
				"Cannot establish whether actor activation already occurred",
			)
		}
		state.WorkloadRefs = []common.Binding{objectref.StatefulSet(&existing)}
	}
	requiresReplacement := func() (ctrl.Result, error) {
		return finish(
			metav1.ConditionFalse,
			api.ReasonRequiresReplacement,
			"Established placement failure requires a new participant name",
		)
	}
	if condition := meta.FindStatusCondition(
		conditions,
		api.ConditionPlacementReady,
	); condition != nil && condition.Status == metav1.ConditionFalse &&
		condition.Reason == api.ReasonPlacementError {
		if p.Status.Runtime == nil || p.Status.Runtime.PodRef == nil {
			return requiresReplacement()
		}
		held = p.Status.Runtime.DeepCopy()
	}
	var ready bool
	if p.Spec.Kind == api.ParticipantBitcoinNode {
		ready, err = r.configuration(ctx, &root, &p, &state)
		status,
			reason,
			message := metav1.ConditionUnknown,
			reasonResolvingConfiguration,
			"Waiting for scoped native configuration agreement"
		if ready {
			status, reason = metav1.ConditionTrue, reasonVerified
			message = "Native syntax and required managed settings agree; Core startup validates " +
				"semantic compatibility"
			if cfg := p.Status.Admission.Configuration.BitcoinNode.Config; cfg != nil &&
				ptr.Deref(cfg.Compatibility, common.CompatibilityManaged) == common.CompatibilityUnverified {
				status, reason = metav1.ConditionFalse, common.CompatibilityUnverified
				message = "Experimental custom actor is excluded from protocol prerequisites and mutation " +
					"ingress"
			}
		}
		meta.SetStatusCondition(
			&conditions,
			metav1.Condition{
				Type:               api.ConditionConfigVerified,
				Status:             status,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: p.Generation,
			},
		)
	} else {
		for _, service := range Services(&p) {
			if err := r.reconcileService(ctx, &p, service); err != nil {
				return finish(metav1.ConditionFalse, api.ReasonOwnershipConflict, err.Error())
			}
		}
		var verified bool
		ready, verified, err = r.stacksConfiguration(ctx, &root, &p, &state)
		status,
			reason,
			message := metav1.ConditionUnknown,
			reasonResolvingConfiguration,
			"Waiting for scoped private configuration agreement"
		if ready {
			if verified {
				status,
					reason,
					message = metav1.ConditionTrue,
					reasonVerified,
					"Native configuration matches frozen genesis and admitted bindings"
			} else {
				status, reason = metav1.ConditionFalse, common.CompatibilityUnverified
				message = "Experimental custom actor is excluded from protocol prerequisites and mutation " +
					"ingress"
			}
		}
		meta.SetStatusCondition(
			&conditions,
			metav1.Condition{
				Type:               api.ConditionConfigVerified,
				Status:             status,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: p.Generation,
			},
		)
	}
	if err != nil {
		return finish(metav1.ConditionFalse, reasonConfigurationUnavailable, err.Error())
	}
	if !ready {
		return finish(metav1.ConditionFalse, reasonResolvingConfiguration, "Waiting for scoped configuration resolver")
	}
	// Recheck current control and admission after resolver/resource operations.
	var current api.StacksNetworkParticipant
	var currentRoot api.StacksNetwork
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(&p), &current); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(&root), &currentRoot); err != nil {
		return ctrl.Result{}, err
	}
	if current.UID != p.UID || current.Generation != p.Generation || currentRoot.UID != root.UID ||
		currentRoot.Generation != root.Generation ||
		currentRoot.DeletionTimestamp != nil ||
		currentRoot.Spec.Operation != root.Spec.Operation ||
		current.Status.Admission == nil ||
		current.Status.Admission.PolicyDigest != p.Status.Admission.PolicyDigest {
		return ctrl.Result{RequeueAfter: time.Millisecond}, nil
	}
	if err := r.authorized(ctx, &currentRoot, &current); err != nil {
		return finish(metav1.ConditionFalse, api.ReasonAdmissionUnavailable, err.Error())
	}
	p = current
	if p.Spec.Kind != api.ParticipantBitcoinNode {
		captured := state
		latest, err := r.stacksInput(ctx, &currentRoot, &p, &captured)
		if err != nil {
			return finish(metav1.ConditionFalse, reasonConfigurationUnavailable, err.Error())
		}
		if digest(latest) != state.ConfigurationDigest {
			return ctrl.Result{RequeueAfter: time.Millisecond}, nil
		}
		if err := r.stacksStartAuthorized(ctx, &currentRoot, &p, time.Now()); err != nil {
			return finish(metav1.ConditionFalse, reasonPrepareBitcoinUnavailable, err.Error())
		}
	}
	// Only the unchanged failed workload may recover; any other workload needs a new name.
	if held != nil && !sameWorkload(held, &state) {
		return requiresReplacement()
	}
	for _, service := range Services(&p) {
		if err := r.reconcileService(ctx, &p, service); err != nil {
			return finish(metav1.ConditionFalse, api.ReasonOwnershipConflict, err.Error())
		}
	}
	workload, err := actorWorkload(&p, &state)
	if err != nil {
		return finish(metav1.ConditionFalse, api.ReasonInvalidConfiguration, err.Error())
	}
	if err := attachObserver(&p, &state, workload, r.ObserverImage); err != nil {
		return finish(metav1.ConditionFalse, api.ReasonInvalidConfiguration, err.Error())
	}
	if err := r.reconcileStatefulSet(ctx, &p, workload); err != nil {
		return finish(metav1.ConditionFalse, reasonWorkloadUnavailable, err.Error())
	}
	state.WorkloadRefs = []common.Binding{}
	state.WorkloadRefs = append(state.WorkloadRefs, objectref.StatefulSet(workload))
	state.Endpoints = runtimeEndpoints(&p)
	state.Terminated = false
	pod, err := r.actorPod(ctx, &p, workload)
	if apierrors.IsNotFound(err) {
		return finish(metav1.ConditionFalse, api.ReasonPending, "Actor Pod has not been observed")
	}
	if err != nil {
		return finish(metav1.ConditionUnknown, reasonRuntimeIdentityUnavailable, err.Error())
	}
	if held != nil && heldPlacement(held, pod) == placementReplaced {
		return requiresReplacement()
	}
	observePod(&state, pod)
	if pod.DeletionTimestamp != nil {
		if terminated(pod) {
			state.Terminated = true
			if !terminationRecorded(&p, pod) {
				return finish(metav1.ConditionFalse, reasonRestarting, "Actor termination confirmed before replacement")
			}
			if err := r.releasePod(ctx, pod); err != nil {
				return ctrl.Result{}, err
			}
		}
		return finish(metav1.ConditionFalse, reasonRestarting, "Actor Pod replacement is in progress")
	}
	if err := r.retainPod(ctx, pod); err != nil {
		return ctrl.Result{}, err
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse &&
			condition.Reason == corev1.PodReasonUnschedulable {
			meta.SetStatusCondition(
				&conditions,
				metav1.Condition{
					Type:               api.ConditionPlacementReady,
					Status:             metav1.ConditionFalse,
					Reason:             api.ReasonPlacementError,
					Message:            condition.Message,
					ObservedGeneration: p.Generation,
				},
			)
			return finish(metav1.ConditionFalse, api.ReasonPlacementError, condition.Message)
		}
	}
	placement := metav1.Condition{
		Type:               api.ConditionPlacementReady,
		Status:             metav1.ConditionUnknown,
		Reason:             api.ReasonPending,
		Message:            "Waiting for a scheduler decision",
		ObservedGeneration: p.Generation,
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionTrue {
			placement.Status = metav1.ConditionTrue
			placement.Reason = api.ReasonScheduled
			placement.Message = "Actor Pod scheduling confirmed"
		}
	}
	if held != nil {
		if heldPlacement(held, pod) != placementRecovered {
			return finish(
				metav1.ConditionFalse,
				api.ReasonPlacementError,
				"Waiting for the failed actor Pod to schedule",
			)
		}
		held = nil
	}
	meta.SetStatusCondition(&conditions, placement)
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name == "config-check" &&
			((status.State.Terminated != nil &&
				status.State.Terminated.ExitCode != 0) ||
				(status.State.Waiting != nil &&
					status.LastTerminationState.Terminated != nil &&
					status.LastTerminationState.Terminated.ExitCode != 0)) {
			return finish(
				metav1.ConditionFalse,
				api.ReasonInvalidConfiguration,
				"Selected actor image rejected its native configuration; private validation output is suppressed",
			)
		}
	}
	if servesAdmitted(p.Spec.Kind, pod, &state) {
		if p.Spec.Kind != api.ParticipantStacksNode {
			return finish(metav1.ConditionTrue, reasonReady, readyMessage)
		}
		if err := r.observeStacksProtocol(ctx, &currentRoot, &p, pod, &state); err != nil && state.Protocol != nil {
			state.Protocol.Available = false
			state.Protocol.Reason = api.ReasonObservationUnavailable
		}
		result, err := finish(metav1.ConditionTrue, reasonReady, readyMessage)
		if err == nil {
			r.converged.record(request.NamespacedName, epoch, convergedPass(&currentRoot, &p, &state))
		}
		return result, err
	}
	return finish(metav1.ConditionFalse, api.ReasonPending, "Waiting for the admitted actor and RPC readiness probe")
}

// selected checks current membership and the durable single-use identity ledger.
func selected(root *api.StacksNetwork, p *api.StacksNetworkParticipant) bool {
	selected := false
	for _, entry := range root.Spec.Participants {
		selected = selected || entry.Name == p.Spec.ParticipantName && entry.Kind == p.Spec.Kind
	}
	if !selected {
		return false
	}
	for _, identity := range root.Status.Identities {
		if identity.Name == p.Spec.ParticipantName {
			return identity.UID == p.UID && !identity.Removing
		}
	}
	return false
}

// authorized verifies current lifecycle, admission and published genesis identities.
func (r *Reconciler) authorized(ctx context.Context, root *api.StacksNetwork, p *api.StacksNetworkParticipant) error {
	if lifecycleWithdrawn(root, p) {
		return fmt.Errorf("network lifecycle withdraws actor activation")
	}
	if err := foundation.ValidateParticipantAdmission(ctx, r.Reader, root, p); err != nil {
		return err
	}
	if _, err := actorFields(p); err != nil {
		return err
	}
	if root.Status.GenesisRef == nil {
		return fmt.Errorf("published genesis is required")
	}
	var genesis api.StacksGenesis
	if err := r.Reader.Get(
		ctx,
		client.ObjectKey{Namespace: root.Namespace, Name: root.Status.GenesisRef.Name},
		&genesis,
	); err != nil {
		return fmt.Errorf("genesis unavailable")
	}
	owner := metav1.GetControllerOf(&genesis)
	if genesis.UID != root.Status.GenesisRef.UID || genesis.DeletionTimestamp != nil || owner == nil ||
		owner.UID != root.UID ||
		genesis.Spec.Source.NetworkUID != root.UID ||
		digest(genesis.Spec.Chain) != root.Status.GenesisDigest {
		return fmt.Errorf("published genesis identity or digest changed")
	}
	return nil
}

// lifecycleWithdrawn reports network or participant lifecycle that forbids actor activation.
func lifecycleWithdrawn(root *api.StacksNetwork, p *api.StacksNetworkParticipant) bool {
	return root.UID != p.Spec.NetworkUID || root.DeletionTimestamp != nil ||
		root.Spec.Operation == api.NetworkOperationStopped ||
		root.Status.Phase == api.NetworkPhaseFailed ||
		meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionFailed) ||
		p.DeletionTimestamp != nil
}

// ownConditions selects only the domain condition entries.
func ownConditions(conditions []metav1.Condition) []metav1.Condition {
	var result []metav1.Condition
	for _, condition := range conditions {
		if condition.Type == api.ConditionWorkloadReady || condition.Type == api.ConditionPlacementReady ||
			condition.Type == api.ConditionConfigVerified {
			result = append(result, condition)
		}
	}
	return result
}

// SetupWithManager routes admitted-policy, lifecycle and owned-workload observations.
func (r *Reconciler) SetupWithManager(manager ctrl.Manager) error {
	if r.Client == nil {
		r.Client = manager.GetClient()
	}
	if r.Reader == nil {
		r.Reader = manager.GetAPIReader()
	}
	if r.converged == nil {
		r.converged = &convergenceMemory{}
	}
	participantChanges := predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		old, oldOK := e.ObjectOld.(*api.StacksNetworkParticipant)
		current, currentOK := e.ObjectNew.(*api.StacksNetworkParticipant)
		return !oldOK || !currentOK || old.Generation != current.Generation || old.UID != current.UID ||
			!reflect.DeepEqual(old.DeletionTimestamp, current.DeletionTimestamp) ||
			!reflect.DeepEqual(old.Status.Admission, current.Status.Admission) ||
			!reflect.DeepEqual(
				meta.FindStatusCondition(old.Status.Conditions, common.ConditionResolved),
				meta.FindStatusCondition(current.Status.Conditions, common.ConditionResolved),
			)
	}}
	rootChanges := predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		old, oldOK := e.ObjectOld.(*api.StacksNetwork)
		current, currentOK := e.ObjectNew.(*api.StacksNetwork)
		return !oldOK || !currentOK || old.Generation != current.Generation || old.UID != current.UID ||
			!reflect.DeepEqual(old.DeletionTimestamp, current.DeletionTimestamp) ||
			!reflect.DeepEqual(old.Status.GenesisRef, current.Status.GenesisRef) ||
			old.Status.GenesisDigest != current.Status.GenesisDigest ||
			(old.Status.Phase == api.NetworkPhaseFailed) != (current.Status.Phase == api.NetworkPhaseFailed) ||
			!reflect.DeepEqual(old.Status.Identities, current.Status.Identities) ||
			!reflect.DeepEqual(
				meta.FindStatusCondition(old.Status.Conditions, api.ConditionFailed),
				meta.FindStatusCondition(current.Status.Conditions, api.ConditionFailed),
			)
	}}
	rootMap := r.routed(func(_ context.Context, object client.Object) []reconcile.Request {
		root, ok := object.(*api.StacksNetwork)
		if !ok {
			return nil
		}
		requests := make([]reconcile.Request, 0, len(root.Status.Identities))
		for _, identity := range root.Status.Identities {
			requests = append(
				requests,
				reconcile.Request{
					NamespacedName: client.ObjectKey{
						Namespace: root.Namespace,
						Name:      foundation.ParticipantName(string(root.UID), identity.Name),
					},
				},
			)
		}
		return requests
	})
	podMap := r.routed(func(_ context.Context, object client.Object) []reconcile.Request {
		labels := object.GetLabels()
		if labels[api.LabelParticipantKind] != string(r.kind()) {
			return nil
		}
		return []reconcile.Request{
			{
				NamespacedName: client.ObjectKey{
					Namespace: object.GetNamespace(),
					Name:      foundation.ParticipantName(labels[api.LabelNetworkUID], labels[api.LabelParticipant]),
				},
			},
		}
	})
	registration := ctrl.NewControllerManagedBy(manager).
		Named("participant-"+strings.ToLower(string(r.kind()))+"-workload").
		WithOptions(controller.Options{MaxConcurrentReconciles: actorWorkers}).
		Watches(&api.StacksNetworkParticipant{}, r.routed(self), builder.WithPredicates(participantChanges)).
		Watches(&appsv1.StatefulSet{}, r.routed(controllingParticipant)).
		Watches(&corev1.Service{}, r.routed(controllingParticipant)).
		Watches(&corev1.ConfigMap{}, r.routed(controllingParticipant)).
		Watches(&batchv1.Job{}, r.routed(controllingParticipant)).
		Watches(&corev1.Pod{}, podMap).
		Watches(&api.StacksNetwork{}, rootMap, builder.WithPredicates(rootChanges))
	if r.kind() != api.ParticipantBitcoinNode {
		dependencies := r.routed(
			func(ctx context.Context, object client.Object) []reconcile.Request {
				// Routing only selects candidates; each reconcile re-reads admitted identity uncached.
				var participants api.StacksNetworkParticipantList
				if err := r.Client.List(ctx, &participants, client.InNamespace(object.GetNamespace())); err != nil {
					return nil
				}
				var requests []reconcile.Request
				for _, p := range participants.Items {
					if p.Spec.Kind == r.kind() {
						requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&p)})
					}
				}
				return requests
			},
		)
		bitcoinChanges := predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
			return stacksStartDependencyChanged(e.ObjectOld, e.ObjectNew)
		}}
		registration = registration.
			Watches(&bitcoin.BitcoinInitialization{}, dependencies, builder.WithPredicates(bitcoinChanges)).
			Watches(&bitcoin.BitcoinExecution{}, dependencies, builder.WithPredicates(bitcoinChanges)).
			Watches(
				&api.StacksNetworkParticipant{},
				dependencies,
				builder.WithPredicates(predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
					old, ok1 := e.ObjectOld.(*api.StacksNetworkParticipant)
					current, ok2 := e.ObjectNew.(*api.StacksNetworkParticipant)
					return !ok1 || !ok2 || actorRuntimeDependencyChanged(old.Status.Runtime, current.Status.Runtime) ||
						!reflect.DeepEqual(old.Status.Admission, current.Status.Admission) ||
						!reflect.DeepEqual(old.DeletionTimestamp, current.DeletionTimestamp)
				}}),
			)
	}
	return registration.Complete(r)
}

// retriesUnwatched reports waiting states that can recover without a watched change, such as
// a Bitcoin observation becoming fresh again with identical contents.
func retriesUnwatched(reason string) bool {
	switch reason {
	case reasonControlUnavailable, reasonConfigurationUnavailable, reasonPrepareBitcoinUnavailable,
		reasonRuntimeIdentityUnavailable, reasonWorkloadUnavailable, api.ReasonStopping,
		api.ReasonTerminationUnknown, reasonRestarting, reasonDrainingControl:
		return true
	}
	return false
}

// stacksStartDependencyChanged keeps Bitcoin record changes that stacksStartAuthorized can observe,
// omitting observation refreshes and scheduler progress. Freshness recovery relies on the
// PrepareBitcoinUnavailable retry instead.
func stacksStartDependencyChanged(old, current client.Object) bool {
	if old == nil || current == nil || old.GetUID() != current.GetUID() ||
		!reflect.DeepEqual(old.GetDeletionTimestamp(), current.GetDeletionTimestamp()) ||
		!reflect.DeepEqual(old.GetOwnerReferences(), current.GetOwnerReferences()) {
		return true
	}
	switch before := old.(type) {
	case *bitcoin.BitcoinExecution:
		after, ok := current.(*bitcoin.BitcoinExecution)
		return !ok || foundation.BitcoinExecutionChanged(before, after)
	case *bitcoin.BitcoinInitialization:
		after, ok := current.(*bitcoin.BitcoinInitialization)
		return !ok || !reflect.DeepEqual(before.Spec, after.Spec) ||
			!reflect.DeepEqual(before.Status.PreparedAt, after.Status.PreparedAt)
	}
	return true
}

// actorRuntimeDependencyChanged excludes protocol telemetry from actor configuration watches.
func actorRuntimeDependencyChanged(old, current *api.ParticipantRuntimeStatus) bool {
	if old == nil || current == nil {
		return old != current
	}
	before, after := *old, *current
	before.Protocol, after.Protocol = nil, nil
	return !reflect.DeepEqual(before, after)
}

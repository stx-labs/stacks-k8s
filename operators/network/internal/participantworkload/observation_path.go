package participantworkload

import (
	"context"
	"sync"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// convergenceInterval bounds how long Stacks protocol polls may skip workload convergence.
// Watched changes end it sooner; the bound covers unwatched inputs such as Secrets.
const convergenceInterval = 30 * time.Second

// readyMessage describes a ready actor on both the convergence and observation paths.
const readyMessage = "Native actor listener and admitted workload are ready; protocol progress is observed separately"

// Pass paths label actor reconcile durations.
const (
	pathConverge = "converge"
	pathObserve  = "observe"
)

// passSeconds times actor passes by path; it is exported only through the metrics registry.
var passSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "stacks_network_actor_pass_seconds",
	Help:    "Actor workload reconcile duration by actor kind and path.",
	Buckets: prometheus.ExponentialBuckets(0.005, 2, 12),
}, []string{"kind", "path"})

func init() { metrics.Registry.MustRegister(passSeconds) }

// observePass records one pass; path is read when the pass returns.
func observePass(kind api.ParticipantKind, path *string, start time.Time) {
	passSeconds.WithLabelValues(string(kind), *path).Observe(time.Since(start).Seconds())
}

// converged holds the validated inputs and published runtime of one ready convergence pass.
type converged struct {
	at             time.Time
	uid            types.UID
	generation     int64
	admission      *api.Admission
	runtime        api.ParticipantRuntimeStatus
	rootUID        types.UID
	rootGeneration int64
	genesis        common.Binding
	genesisDigest  string
}

// convergedPass captures a Stacks node pass that published Ready, without protocol telemetry.
func convergedPass(
	root *api.StacksNetwork,
	p *api.StacksNetworkParticipant,
	state *api.ParticipantRuntimeStatus,
) converged {
	runtime := *state.DeepCopy()
	runtime.Protocol = nil
	result := converged{
		at:             time.Now(),
		uid:            p.UID,
		generation:     p.Generation,
		admission:      p.Status.Admission.DeepCopy(),
		runtime:        runtime,
		rootUID:        root.UID,
		rootGeneration: root.Generation,
		genesisDigest:  root.Status.GenesisDigest,
	}
	if root.Status.GenesisRef != nil {
		result.genesis = *root.Status.GenesisRef
	}
	return result
}

// matches requires unchanged admitted, lifecycle and published runtime identity.
func (c *converged) matches(root *api.StacksNetwork, p *api.StacksNetworkParticipant) bool {
	if p.Status.Runtime == nil || root.Status.GenesisRef == nil {
		return false
	}
	runtime := *p.Status.Runtime.DeepCopy()
	runtime.Protocol = nil
	return time.Since(c.at) < convergenceInterval &&
		p.UID == c.uid && p.Generation == c.generation &&
		equality.Semantic.DeepEqual(p.Status.Admission, c.admission) &&
		equality.Semantic.DeepEqual(runtime, c.runtime) &&
		root.UID == c.rootUID && root.Generation == c.rootGeneration &&
		*root.Status.GenesisRef == c.genesis && root.Status.GenesisDigest == c.genesisDigest &&
		!lifecycleWithdrawn(root, p)
}

// convergenceMemory keeps the last ready pass per participant. Every routed watch event bumps the
// key's epoch, discarding it and any pass that began before the event.
type convergenceMemory struct {
	mu      sync.Mutex
	entries map[types.NamespacedName]*convergenceEntry
}

// convergenceEntry pairs an invalidation epoch with the pass recorded within it.
type convergenceEntry struct {
	epoch uint64
	last  *converged
}

// entry returns the key's entry, creating it; callers hold mu.
func (m *convergenceMemory) entry(key types.NamespacedName) *convergenceEntry {
	if m.entries == nil {
		m.entries = map[types.NamespacedName]*convergenceEntry{}
	}
	e := m.entries[key]
	if e == nil {
		e = &convergenceEntry{}
		m.entries[key] = e
	}
	return e
}

// epoch identifies the invalidation state at the start of a pass.
func (m *convergenceMemory) epoch(key types.NamespacedName) uint64 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entry(key).epoch
}

// forget discards the key's pass and any pass still in flight.
func (m *convergenceMemory) forget(key types.NamespacedName) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entry(key)
	e.epoch++
	e.last = nil
}

// record keeps a pass only if no watched change arrived since it began.
func (m *convergenceMemory) record(key types.NamespacedName, epoch uint64, pass converged) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entry(key); e.epoch == epoch {
		e.last = &pass
	}
}

// recall returns the key's last ready pass, if any.
func (m *convergenceMemory) recall(key types.NamespacedName) *converged {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entry(key).last
}

// routed enqueues fn's requests after discarding their convergence, so any watched change makes
// the next pass converge instead of only observing protocol.
func (r *Reconciler) routed(fn handler.MapFunc) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []reconcile.Request {
		requests := fn(ctx, object)
		for _, request := range requests {
			r.converged.forget(request.NamespacedName)
		}
		return requests
	})
}

// self routes a participant change to that participant.
func self(_ context.Context, object client.Object) []reconcile.Request {
	return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(object)}}
}

// controllingParticipant routes an owned resource change to its controlling participant.
func controllingParticipant(_ context.Context, object client.Object) []reconcile.Request {
	owner := metav1.GetControllerOf(object)
	if owner == nil || owner.APIVersion != api.GroupVersion.String() || owner.Kind != api.KindStacksNetworkParticipant {
		return nil
	}
	return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: object.GetNamespace(), Name: owner.Name}}}
}

// servesAdmitted requires a ready Pod running the admitted policy and, for Stacks actors, configuration.
func servesAdmitted(kind api.ParticipantKind, pod *corev1.Pod, state *api.ParticipantRuntimeStatus) bool {
	return podReady(pod) && pod.Annotations[api.AnnotationPolicyDigest] == state.PolicyDigest &&
		(kind == api.ParticipantBitcoinNode ||
			pod.Annotations[api.AnnotationConfigurationDigest] == state.ConfigurationDigest)
}

// convergedPod returns the published actor Pod when a ready Stacks node's last convergence still
// holds, so the pass only observes protocol. Any mismatch returns nil and the pass converges.
func (r *Reconciler) convergedPod(
	ctx context.Context,
	root *api.StacksNetwork,
	p *api.StacksNetworkParticipant,
	state *api.ParticipantRuntimeStatus,
) *corev1.Pod {
	if p.Spec.Kind != api.ParticipantStacksNode {
		return nil
	}
	last := r.converged.recall(client.ObjectKeyFromObject(p))
	if last == nil || !last.matches(root, p) || state.PodRef == nil || len(state.WorkloadRefs) != 1 {
		return nil
	}
	ready := meta.FindStatusCondition(p.Status.Conditions, api.ConditionWorkloadReady)
	if ready == nil || ready.Status != metav1.ConditionTrue || ready.Reason != reasonReady ||
		ready.ObservedGeneration != p.Generation {
		return nil
	}
	var pod corev1.Pod
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: state.PodRef.Name}, &pod); err != nil {
		return nil
	}
	var process api.ParticipantRuntimeStatus
	observePod(&process, &pod)
	if pod.UID != state.PodRef.UID || process.ContainerID != state.ContainerID || pod.Status.PodIP != state.PodIP ||
		!actorPodOwned(&pod, p, state.WorkloadRefs[0].UID) ||
		!controllerutil.ContainsFinalizer(&pod, PodFinalizer) ||
		!servesAdmitted(p.Spec.Kind, &pod, state) {
		return nil
	}
	return &pod
}

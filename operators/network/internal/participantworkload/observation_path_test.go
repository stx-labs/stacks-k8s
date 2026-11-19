package participantworkload

import (
	"context"
	"strings"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/priorityqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// readyStacksNode is a converged, ready Stacks node with its network, genesis and actor Pod.
type readyStacksNode struct {
	root    *api.StacksNetwork
	genesis *api.StacksGenesis
	p       *api.StacksNetworkParticipant
	pod     *corev1.Pod
}

func readyStacksNodeFixture() readyStacksNode {
	p := stacksParticipantFixture("StacksNode")
	p.Finalizers = []string{(&Reconciler{Kind: "StacksNode"}).finalizer()}
	root := &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: p.Namespace, UID: p.Spec.NetworkUID, Generation: 1},
		Spec: api.StacksNetworkSpec{
			Participants: []api.Participant{{Name: p.Spec.ParticipantName, Kind: p.Spec.Kind}},
		},
		Status: api.StacksNetworkStatus{
			Phase:      "Running",
			Identities: []api.InstanceIdentity{{Name: p.Spec.ParticipantName, UID: p.UID}},
		},
	}
	genesis := &api.StacksGenesis{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "genesis",
			Namespace:       p.Namespace,
			UID:             "genesis-uid",
			OwnerReferences: p.OwnerReferences,
		},
		Spec: api.StacksGenesisSpec{Source: api.GenesisSource{NetworkUID: root.UID}},
	}
	root.Status.GenesisRef = binding("StacksGenesis", genesis)
	root.Status.GenesisDigest = digest(genesis.Spec.Chain)
	workload := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: Name(p, actorPurpose), UID: "workload-uid"}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:       workload.Name + "-0",
			Namespace:  p.Namespace,
			UID:        "pod-uid",
			Labels:     Labels(p, api.RoleActor),
			Finalizers: []string{PodFinalizer},
			Annotations: map[string]string{
				api.AnnotationPolicyDigest:        p.Status.Admission.PolicyDigest,
				api.AnnotationConfigurationDigest: "config",
			},
			OwnerReferences: []metav1.OwnerReference{
				{Kind: common.KindStatefulSet, Name: workload.Name, UID: workload.UID, Controller: ptr.To(true)},
			},
		},
		Status: corev1.PodStatus{
			PodIP:      "10.10.0.2",
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: actorContainer(p.Spec.Kind), ContainerID: "containerd://native"},
			},
		},
	}
	p.Status.Runtime = &api.ParticipantRuntimeStatus{
		ObservedGeneration:  p.Generation,
		PolicyDigest:        p.Status.Admission.PolicyDigest,
		ConfigurationDigest: "config",
		WorkloadRefs:        []common.Binding{*binding("StatefulSet", workload)},
	}
	observePod(p.Status.Runtime, pod)
	p.Status.Conditions = []metav1.Condition{{
		Type:               api.ConditionWorkloadReady,
		Status:             metav1.ConditionTrue,
		Reason:             reasonReady,
		Message:            readyMessage,
		ObservedGeneration: p.Generation,
		LastTransitionTime: metav1.Now(),
	}}
	return readyStacksNode{root: root, genesis: genesis, p: p, pod: pod}
}

// remembered returns memory holding f's last ready convergence.
func (f readyStacksNode) remembered() *convergenceMemory {
	memory := &convergenceMemory{}
	key := client.ObjectKeyFromObject(f.p)
	memory.record(key, memory.epoch(key), convergedPass(f.root, f.p, f.p.Status.Runtime))
	return memory
}

func TestReadyStacksPollObservesWithoutConvergenceReads(t *testing.T) {
	f := readyStacksNodeFixture()
	c := testClient(t, f.root, f.genesis, f.p, f.pod)
	reads := map[string]int{}
	reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		Get: func(
			ctx context.Context,
			c client.WithWatch,
			key client.ObjectKey,
			obj client.Object,
			opts ...client.GetOption,
		) error {
			gvk, err := c.GroupVersionKindFor(obj)
			if err != nil {
				return err
			}
			reads[gvk.Kind]++
			return c.Get(ctx, key, obj, opts...)
		},
	})
	node := &nativeObservationFixture{view: rpc.ChainView{
		Info:         rpc.Info{NetworkID: 0x80000000, BurnHeight: 240, StacksHeight: 30, Tip: strings.Repeat("1", 64)},
		IndexBlockID: strings.Repeat("4", 64),
	}}
	r := &Reconciler{
		Kind:           "StacksNode",
		Client:         c,
		Reader:         reader,
		converged:      f.remembered(),
		ProtocolClient: func(string) (StacksProtocolRPC, error) { return node, nil },
	}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.p)}
	result, err := r.Reconcile(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter <= 0 || result.RequeueAfter > protocolPollInterval {
		t.Fatalf("ready poll did not keep the protocol cadence: %v", result.RequeueAfter)
	}
	want := map[string]int{"StacksNetworkParticipant": 1, "StacksNetwork": 1, "Pod": 2, "StacksGenesis": 1}
	if len(reads) != len(want) {
		t.Fatalf("observation poll read %v, want only %v", reads, want)
	}
	for kind, n := range want {
		if reads[kind] != n {
			t.Fatalf("observation poll read %v, want only %v", reads, want)
		}
	}
	var published api.StacksNetworkParticipant
	if err := c.Get(context.Background(), request.NamespacedName, &published); err != nil {
		t.Fatal(err)
	}
	ready := meta.FindStatusCondition(published.Status.Conditions, api.ConditionWorkloadReady)
	if ready == nil || ready.Status != metav1.ConditionTrue || published.Status.Runtime.Protocol == nil ||
		!published.Status.Runtime.Protocol.Available || published.Status.Runtime.Protocol.StacksHeight != 30 {
		t.Fatalf("observation poll did not publish fresh protocol evidence: %+v", published.Status)
	}
}

func TestConvergedPodRequiresUnchangedIdentityAndLifecycle(t *testing.T) {
	cases := map[string]func(*readyStacksNode){
		"unchanged":            func(*readyStacksNode) {},
		"participant replaced": func(f *readyStacksNode) { f.p.UID = "replacement" },
		"generation":           func(f *readyStacksNode) { f.p.Generation++ },
		"admission":            func(f *readyStacksNode) { f.p.Status.Admission.PolicyDigest = "other-policy" },
		"runtime":              func(f *readyStacksNode) { f.p.Status.Runtime.ConfigurationDigest = "other-config" },
		"not ready": func(f *readyStacksNode) {
			f.p.Status.Conditions[0].Status = metav1.ConditionFalse
		},
		"participant deleting": func(f *readyStacksNode) { f.p.DeletionTimestamp = ptr.To(metav1.Now()) },
		"network generation":   func(f *readyStacksNode) { f.root.Generation++ },
		"network stopped":      func(f *readyStacksNode) { f.root.Spec.Operation = api.NetworkOperationStopped },
		"network failed":       func(f *readyStacksNode) { f.root.Status.Phase = api.NetworkPhaseFailed },
		"genesis replaced":     func(f *readyStacksNode) { f.root.Status.GenesisRef.UID = "other-genesis" },
		"pod replaced":         func(f *readyStacksNode) { f.pod.UID = "replacement-pod" },
		"process restarted": func(f *readyStacksNode) {
			f.pod.Status.ContainerStatuses[0].ContainerID = "containerd://restarted"
		},
		"pod not ready": func(f *readyStacksNode) {
			f.pod.Status.Conditions[0].Status = corev1.ConditionFalse
		},
		"pod configuration": func(f *readyStacksNode) {
			f.pod.Annotations[api.AnnotationConfigurationDigest] = "other-config"
		},
		"pod not retained": func(f *readyStacksNode) { f.pod.Finalizers = nil },
		"pod foreign":      func(f *readyStacksNode) { f.pod.OwnerReferences[0].UID = "other-workload" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := readyStacksNodeFixture()
			memory := f.remembered()
			mutate(&f)
			c := testClient(t, f.pod)
			r := &Reconciler{Kind: "StacksNode", Reader: c, converged: memory}
			state := f.p.Status.Runtime.DeepCopy()
			if got := r.convergedPod(context.Background(), f.root, f.p, state) != nil; got != (name == "unchanged") {
				t.Fatalf("observation path selected = %v", got)
			}
		})
	}
}

func TestConvergenceExpiresAndIgnoresPassesOverlappingWatchedChanges(t *testing.T) {
	f := readyStacksNodeFixture()
	key := client.ObjectKeyFromObject(f.p)
	pass := convergedPass(f.root, f.p, f.p.Status.Runtime)
	memory := &convergenceMemory{}
	began := memory.epoch(key)
	memory.forget(key)
	memory.record(key, began, pass)
	if memory.recall(key) != nil {
		t.Fatal("a pass that began before a watched change was remembered")
	}
	memory.record(key, memory.epoch(key), pass)
	if last := memory.recall(key); last == nil || !last.matches(f.root, f.p) {
		t.Fatal("a pass after the change was not remembered")
	}
	memory.recall(key).at = time.Now().Add(-convergenceInterval)
	if memory.recall(key).matches(f.root, f.p) {
		t.Fatal("expired convergence still skipped unwatched inputs")
	}
	if (*convergenceMemory)(nil).recall(key) != nil {
		t.Fatal("a reconciler without memory skipped convergence")
	}
}

func TestOwnedResourceChangesDiscardConvergence(t *testing.T) {
	f := readyStacksNodeFixture()
	memory := f.remembered()
	r := &Reconciler{converged: memory}
	service := Services(f.p)[0]
	service.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: api.GroupVersion.String(),
		Kind:       api.KindStacksNetworkParticipant,
		Name:       f.p.Name,
		UID:        f.p.UID,
		Controller: ptr.To(true),
	}}
	queue := priorityqueue.New[reconcile.Request]("test")
	defer queue.ShutDown()
	r.routed(controllingParticipant).Delete(context.Background(), event.DeleteEvent{Object: service}, queue)
	if memory.recall(client.ObjectKeyFromObject(f.p)) != nil {
		t.Fatal("deleting an owned Service left the observation path selected")
	}
	if queue.Len() != 1 {
		t.Fatal("owned Service deletion did not enqueue its participant for repair")
	}
}

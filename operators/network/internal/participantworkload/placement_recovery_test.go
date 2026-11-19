package participantworkload

import (
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// failedPlacement is the runtime recorded with a placement error, and its unscheduled Pod.
func failedPlacement() (*api.ParticipantRuntimeStatus, *corev1.Pod) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "actor-0", UID: "failed-pod"},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
			Type:               corev1.PodScheduled,
			Status:             corev1.ConditionFalse,
			Reason:             corev1.PodReasonUnschedulable,
			Message:            `persistentvolumeclaim "data-actor-0" not found`,
			LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
		}}},
	}
	return &api.ParticipantRuntimeStatus{
		PodRef:              &common.Binding{Kind: "Pod", Name: pod.Name, UID: pod.UID},
		PolicyDigest:        "sha256:policy",
		ConfigurationDigest: "sha256:config",
	}, pod
}

func TestHeldPlacementRecoversOnlyTheFailedPodWithoutDeadline(t *testing.T) {
	held, pod := failedPlacement()
	if got := heldPlacement(held, pod); got != placementFailing {
		t.Fatalf("Pod unschedulable for an hour = %v, want still failing", got)
	}
	pod.Status.Conditions[0].Status = corev1.ConditionTrue
	pod.Status.Conditions[0].Reason = ""
	if got := heldPlacement(held, pod); got != placementRecovered {
		t.Fatalf("the failed Pod scheduled = %v, want recovered", got)
	}
	replacement := pod.DeepCopy()
	replacement.UID = "replacement-pod"
	if got := heldPlacement(held, replacement); got != placementReplaced {
		t.Fatalf("a different scheduled Pod = %v, want replacement required", got)
	}
	held.PodRef = nil
	if got := heldPlacement(held, pod); got != placementReplaced {
		t.Fatalf("an error without a recorded Pod = %v, want replacement required", got)
	}
}

func TestPlacementRecoveryRejectsWorkloadChangesWithoutGenerationChange(t *testing.T) {
	held, _ := failedPlacement()
	state := held.DeepCopy()
	if !sameWorkload(held, state) {
		t.Fatal("the unchanged workload could not recover")
	}
	admission := held.DeepCopy()
	admission.PolicyDigest = "sha256:readmitted"
	configuration := held.DeepCopy()
	configuration.ConfigurationDigest = "sha256:new-dependency"
	for name, changed := range map[string]*api.ParticipantRuntimeStatus{
		"admission": admission, "configuration": configuration,
	} {
		if sameWorkload(held, changed) {
			t.Fatalf("%s change slipped through placement recovery", name)
		}
	}
}

func TestHeldPlacementKeepsReportingTheFailedWorkload(t *testing.T) {
	held, _ := failedPlacement()
	state := &api.ParticipantRuntimeStatus{
		ObservedGeneration:  7,
		PodRef:              &common.Binding{Kind: "Pod", Name: "actor-0", UID: "other-pod"},
		PolicyDigest:        "sha256:readmitted",
		ConfigurationDigest: "sha256:new-dependency",
		Protocol:            &api.StacksProtocolObservation{Reason: reasonActorUnavailable},
	}
	got := published(state, held)
	if got.PodRef.UID != held.PodRef.UID || got.PolicyDigest != held.PolicyDigest ||
		got.ConfigurationDigest != held.ConfigurationDigest {
		t.Fatalf("a pass during the error overwrote the failed identity: %+v", got)
	}
	if got.ObservedGeneration != 7 || got.Protocol != state.Protocol {
		t.Fatal("held identity hid the current observation")
	}
	if published(state, nil) != state {
		t.Fatal("without a placement error the pass did not report its own runtime")
	}
}

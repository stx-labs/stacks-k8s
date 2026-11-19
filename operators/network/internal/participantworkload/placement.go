package participantworkload

import (
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
)

// sameWorkload reports whether a pass renders the workload a placement error was recorded with.
func sameWorkload(held, state *api.ParticipantRuntimeStatus) bool {
	return state.PolicyDigest == held.PolicyDigest && state.ConfigurationDigest == held.ConfigurationDigest
}

// heldPlacementState classifies the actor Pod against a held placement error.
type heldPlacementState int

const (
	// placementFailing: the failed Pod has not scheduled yet.
	placementFailing heldPlacementState = iota
	// placementRecovered: the failed Pod itself has since scheduled; the error was transient.
	placementRecovered
	// placementReplaced: the Pod is not the one that failed.
	placementReplaced
)

// heldPlacement allows recovery only by the Pod the error was recorded for, with no deadline.
func heldPlacement(held *api.ParticipantRuntimeStatus, pod *corev1.Pod) heldPlacementState {
	if held.PodRef == nil || pod.UID != held.PodRef.UID {
		return placementReplaced
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionTrue {
			return placementRecovered
		}
	}
	return placementFailing
}

// published is the runtime a pass reports: the held identity while a placement error holds, so
// no pass can record a changed workload as the one that failed.
func published(state, held *api.ParticipantRuntimeStatus) *api.ParticipantRuntimeStatus {
	if held == nil {
		return state
	}
	runtime := held.DeepCopy()
	runtime.ObservedGeneration, runtime.Protocol = state.ObservedGeneration, state.Protocol
	return runtime
}

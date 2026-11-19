package stacksworker

import (
	"context"
	"testing"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestWorkerPlacementErrorClearsOnlyOnceThePodSchedules(t *testing.T) {
	for name, scheduled := range map[string]bool{"still unschedulable": false, "scheduled later": true} {
		t.Run(name, func(t *testing.T) {
			root, p, pod, _ := fixture(t)
			p.Status.Conditions = append(p.Status.Conditions, metav1.Condition{
				Type:               api.ConditionPlacementReady,
				Status:             metav1.ConditionFalse,
				Reason:             api.ReasonPlacementError,
				Message:            "Worker placement requirements cannot be satisfied",
				LastTransitionTime: metav1.Now(),
			})
			if !scheduled {
				pod.Spec.NodeName = ""
				pod.Status.Conditions = []corev1.PodCondition{{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionFalse,
					Reason: corev1.PodReasonUnschedulable,
				}}
			}
			c := fakeClient(t, root, p, pod)
			r := Reconciler{Client: c, Reader: c, Kind: p.Spec.Kind}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)}
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(p), p); err != nil {
				t.Fatal(err)
			}
			placement := meta.FindStatusCondition(p.Status.Conditions, api.ConditionPlacementReady)
			if placement == nil || (placement.Status == metav1.ConditionTrue) != scheduled ||
				!scheduled && placement.Reason != api.ReasonPlacementError {
				t.Fatalf("placement after the Pod is %s: %+v", name, placement)
			}
		})
	}
}

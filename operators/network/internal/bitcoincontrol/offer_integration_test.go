//go:build integration

package bitcoincontrol

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestAssignOfferToleratesStatusWritesAndRejectsStaleOffers verifies the API-server field precondition.
func TestAssignOfferToleratesStatusWritesAndRejectsStaleOffers(t *testing.T) {
	ctx := context.Background()
	c := controlIntegrationClient(t)
	f := newFixture(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}))
	f.offer(t, 7)
	execution := f.readRecord(t)
	first := execution.Spec.Offer.DeepCopy()
	execution.UID, execution.ResourceVersion = "", ""
	execution.Spec.Offer = nil
	must(c.Create(ctx, execution))
	stale := execution.DeepCopy()

	// A worker status write between read and assignment changes only resourceVersion.
	execution.Status.BlocksGenerated++
	must(c.Status().Update(ctx, execution))
	if err := assignOffer(ctx, c, stale.DeepCopy(), first); err != nil {
		t.Fatalf("status write blocked assignment: %v", err)
	}

	second := first.DeepCopy()
	second.Number++
	if err := assignOffer(ctx, c, stale.DeepCopy(), second); err == nil {
		t.Fatal("assignment replaced an offer it had not read")
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(execution), execution))
	if execution.Spec.Offer == nil || execution.Spec.Offer.Number != first.Number {
		t.Fatalf("stale assignment changed offer: %+v", execution.Spec.Offer)
	}
	if err := assignOffer(ctx, c, execution.DeepCopy(), second); err != nil {
		t.Fatalf("current assignment rejected: %v", err)
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(execution), execution))
	if execution.Spec.Offer.Number != second.Number {
		t.Fatalf("offer not replaced: %+v", execution.Spec.Offer)
	}
}

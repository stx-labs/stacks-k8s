package foundation

import (
	"context"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func cadence(interval string) bitcoin.Cadence {
	return bitcoin.Cadence{Mode: bitcoin.CadenceFixed, Interval: ptr.To(common.Duration(interval))}
}

func heightCadences(heights ...int64) bitcoin.BitcoinBlockScheduleSpec {
	var spec bitcoin.BitcoinBlockScheduleSpec
	for _, height := range heights {
		spec.Cadences = append(spec.Cadences, bitcoin.HeightCadence{FromHeight: height, Cadence: cadence("5s")})
	}
	return spec
}

func TestValidateHeightCadences(t *testing.T) {
	both := heightCadences(0)
	both.Cadence = ptr.To(cadence("5s"))
	invalidEntry := heightCadences(0)
	invalidEntry.Cadences = append(invalidEntry.Cadences, bitcoin.HeightCadence{FromHeight: 9, Cadence: cadence("0s")})
	tooMany := make([]int64, maxHeightCadences+1)
	for i := range tooMany {
		tooMany[i] = int64(i)
	}
	for name, spec := range map[string]bitcoin.BitcoinBlockScheduleSpec{
		"neither":   {},
		"both":      both,
		"no zero":   heightCadences(1, 282),
		"duplicate": heightCadences(0, 282, 282),
		"negative":  heightCadences(0, -1),
		"entry":     invalidEntry,
		"too many":  heightCadences(tooMany...),
	} {
		if validateSchedule(spec) == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if err := validateSchedule(heightCadences(282, 0)); err != nil {
		t.Fatalf("unordered entries rejected: %v", err)
	}
}

func TestCanonicalScheduleIdentityIgnoresEntryOrder(t *testing.T) {
	ordered, reversed := heightCadences(0, 100, 282), heightCadences(282, 0, 100)
	if Digest(CanonicalSchedule(ordered)) != Digest(CanonicalSchedule(reversed)) ||
		!equal(CanonicalSchedule(ordered), CanonicalSchedule(reversed)) {
		t.Fatal("equivalent height cadences have different identities")
	}
	if reversed.Cadences[0].FromHeight != 282 {
		t.Fatal("canonicalization mutated its input")
	}
	single := bitcoin.BitcoinBlockScheduleSpec{Cadence: ptr.To(cadence("60s"))}
	if Digest(CanonicalSchedule(single)) != Digest(single) {
		t.Fatal("single-cadence fingerprint changed")
	}
}

func TestScheduleDefinitionDigestIgnoresEntryOrder(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := bitcoin.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	schedule := func(name string, spec bitcoin.BitcoinBlockScheduleSpec) *bitcoin.BitcoinBlockSchedule {
		return &bitcoin.BitcoinBlockSchedule{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: name}, Spec: spec}
	}
	single := schedule("single", bitcoin.BitcoinBlockScheduleSpec{Cadence: ptr.To(cadence("60s"))})
	objects := []client.Object{
		schedule("ordered", heightCadences(0, 282)), schedule("reversed", heightCadences(282, 0)), single,
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).
		WithStatusSubresource(&bitcoin.BitcoinBlockSchedule{}).Build()
	r := &DefinitionReconciler{Client: c, Scheme: scheme, Prototype: &bitcoin.BitcoinBlockSchedule{}}
	digests := map[string]string{}
	for _, object := range objects {
		key := client.ObjectKeyFromObject(object)
		if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatal(err)
		}
		var current bitcoin.BitcoinBlockSchedule
		if err := c.Get(context.Background(), key, &current); err != nil {
			t.Fatal(err)
		}
		digests[key.Name] = current.Status.Digest
	}
	legacy, err := sourceSpec(single)
	if err != nil {
		t.Fatal(err)
	}
	if digests["ordered"] == "" || digests["ordered"] != digests["reversed"] || digests["single"] != Digest(legacy) {
		t.Fatalf("schedule definition digests depend on entry order or changed: %v", digests)
	}
}

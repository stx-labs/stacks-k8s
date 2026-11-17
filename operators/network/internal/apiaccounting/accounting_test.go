package apiaccounting_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/apiaccounting"
	"github.com/go-logr/logr/funcr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReaderAttributesParticipantReads(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	participant := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "n"}}
	recorder := apiaccounting.NewRecorder()
	reader := apiaccounting.Reader{
		Reader:   fake.NewClientBuilder().WithScheme(scheme).WithObjects(participant).Build(),
		Recorder: recorder,
	}
	key := client.ObjectKeyFromObject(participant)
	for range 2 {
		if err := reader.Get(t.Context(), key, &api.StacksNetworkParticipant{}); err != nil {
			t.Fatal(err)
		}
	}
	absent := client.ObjectKey{Namespace: "n", Name: "missing"}
	missing := reader.Get(t.Context(), absent, &api.StacksNetworkParticipant{})
	if client.IgnoreNotFound(missing) != nil || missing == nil {
		t.Fatal("expected not found", missing)
	}
	if err := reader.List(t.Context(), &api.StacksNetworkParticipantList{}); err != nil {
		t.Fatal(err)
	}

	series := recorder.Snapshot()
	got := map[string]int64{}
	for _, s := range series {
		if !strings.HasSuffix(s.Helper, "TestReaderAttributesParticipantReads") || s.Entry == "" {
			t.Fatalf("participant read lacks call site: %+v", s)
		}
		got[s.Verb+" "+s.Kind+" "+s.Outcome] = s.Count
	}
	want := map[string]int64{
		"GET StacksNetworkParticipant ok":       2,
		"GET StacksNetworkParticipant notfound": 1,
		"LIST StacksNetworkParticipantList ok":  1,
	}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatal(got)
		}
	}
}

// Consumers bracket a window with snapshots, so they must continue when counts stop changing.
func TestReportSnapshotsWithoutNewReads(t *testing.T) {
	var snapshots atomic.Int32
	log := funcr.New(func(_, args string) {
		if strings.Contains(args, `"series"`) {
			snapshots.Add(1)
		}
	}, funcr.Options{})
	ctx, cancel := context.WithTimeout(t.Context(), 55*time.Millisecond)
	defer cancel()
	apiaccounting.NewRecorder().Report(ctx, log, 10*time.Millisecond)
	// Startup, about five intervals and shutdown, with no reads recorded.
	if n := snapshots.Load(); n < 4 {
		t.Fatalf("got %d snapshots without new reads", n)
	}
}

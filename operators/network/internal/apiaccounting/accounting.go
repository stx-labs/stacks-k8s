// Package apiaccounting attributes direct (uncached) API reads for a performance
// experiment. It lives only on the k3s multi-runner experiment branch and is not meant to
// be merged.
//
// When STACKS_API_ACCOUNTING_INTERVAL is set, Wrap returns a manager whose GetAPIReader
// counts every Get and List by verb, kind and outcome, with total call duration. Duration
// covers the whole client call, including client-side waiting and decoding, not network
// latency alone. Participant reads also record their call site: the innermost operator
// frame (helper) and the outermost one (controller entry point). Cumulative totals are
// logged as "api-accounting" at startup, every interval (changed or not), and at shutdown,
// so a consumer can always bracket a time window with snapshots.
package apiaccounting

import (
	"context"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// IntervalVariable enables accounting with a Go duration, e.g. "5s".
const IntervalVariable = "STACKS_API_ACCOUNTING_INTERVAL"

// operatorFrames identifies operator code in call stacks.
const operatorFrames = "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/"

// Wrap returns m unchanged unless accounting is enabled.
func Wrap(m ctrl.Manager) (ctrl.Manager, error) {
	value := os.Getenv(IntervalVariable)
	if value == "" {
		return m, nil
	}
	interval, err := time.ParseDuration(value)
	if err != nil {
		return nil, err
	}
	recorder := NewRecorder()
	log := ctrl.Log.WithName("api-accounting")
	if err := m.Add(manager.RunnableFunc(func(ctx context.Context) error {
		recorder.Report(ctx, log, interval)
		return nil
	})); err != nil {
		return nil, err
	}
	return wrapped{Manager: m, reader: Reader{Reader: m.GetAPIReader(), Recorder: recorder}}, nil
}

type wrapped struct {
	ctrl.Manager
	reader client.Reader
}

func (w wrapped) GetAPIReader() client.Reader { return w.reader }

// Key identifies one accounted series.
type Key struct {
	Verb    string `json:"verb"`
	Kind    string `json:"kind"`
	Outcome string `json:"outcome"`
	Helper  string `json:"helper,omitempty"`
	Entry   string `json:"entry,omitempty"`
}

// Series is one cumulative total.
type Series struct {
	Key
	Count   int64   `json:"count"`
	Seconds float64 `json:"seconds"`
}

// Recorder holds concurrency-safe cumulative totals.
type Recorder struct {
	mu     sync.Mutex
	totals map[Key]*Series
}

// NewRecorder returns an empty recorder.
func NewRecorder() *Recorder { return &Recorder{totals: map[Key]*Series{}} }

func (r *Recorder) record(k Key, elapsed time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.totals[k]
	if s == nil {
		s = &Series{Key: k}
		r.totals[k] = s
	}
	s.Count++
	s.Seconds += elapsed.Seconds()
}

// Snapshot returns the totals, largest count first.
func (r *Recorder) Snapshot() []Series {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Series, 0, len(r.totals))
	for _, s := range r.totals {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

// Report logs cumulative totals at startup, every interval and at shutdown.
func (r *Recorder) Report(ctx context.Context, log logr.Logger, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	emit := func() { log.Info("cumulative", "series", r.Snapshot()) }
	emit()
	for {
		select {
		case <-ctx.Done():
			emit()
			return
		case <-ticker.C:
			emit()
		}
	}
}

// Reader counts the calls of the wrapped direct reader.
type Reader struct {
	client.Reader
	Recorder *Recorder
}

// Get implements client.Reader.
func (r Reader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	start := time.Now()
	err := r.Reader.Get(ctx, key, obj, opts...)
	elapsed := time.Since(start) // Before key: call-site inspection is not call time.
	r.Recorder.record(r.key("GET", obj, err), elapsed)
	return err
}

// List implements client.Reader.
func (r Reader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	start := time.Now()
	err := r.Reader.List(ctx, list, opts...)
	elapsed := time.Since(start) // Before key: call-site inspection is not call time.
	r.Recorder.record(r.key("LIST", list, err), elapsed)
	return err
}

func (r Reader) key(verb string, obj any, err error) Key {
	k := Key{Verb: verb, Kind: kind(obj), Outcome: outcome(err)}
	// Stack inspection only for the targeted participant reads.
	switch obj.(type) {
	case *api.StacksNetworkParticipant, *api.StacksNetworkParticipantList:
		k.Helper, k.Entry = callSite()
	}
	return k
}

// kind prefers declared metadata (e.g. Secret metadata-only reads), then the Go type name.
func kind(obj any) string {
	if o, ok := obj.(client.Object); ok {
		if gvk := o.GetObjectKind().GroupVersionKind(); gvk.Kind != "" {
			return gvk.Kind
		}
	}
	t := reflect.TypeOf(obj)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return "unknown"
	}
	return t.Name()
}

func outcome(err error) string {
	switch {
	case err == nil:
		return "ok"
	case client.IgnoreNotFound(err) == nil:
		return "notfound"
	default:
		return "error"
	}
}

// callSite returns the innermost and outermost operator frames calling the reader.
func callSite() (helper, entry string) {
	pcs := make([]uintptr, 48)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	for {
		frame, more := frames.Next()
		name, ok := strings.CutPrefix(frame.Function, operatorFrames)
		if ok && !strings.HasPrefix(name, "apiaccounting.") {
			if helper == "" {
				helper = name
			}
			entry = name
		}
		if !more {
			return helper, entry
		}
	}
}

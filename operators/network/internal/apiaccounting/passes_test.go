package apiaccounting_test

import (
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/apiaccounting"
	"github.com/prometheus/client_golang/prometheus"
)

func TestPassSnapshotReportsOnlyRecordedPassFamilies(t *testing.T) {
	registry := prometheus.NewRegistry()
	passes := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "stacks_network_actor_pass_seconds",
	}, []string{"kind", "path"})
	results := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "controller_runtime_reconcile_total",
	}, []string{"controller", "result"})
	unrelated := prometheus.NewCounter(prometheus.CounterOpts{Name: "unrelated_total"})
	registry.MustRegister(passes, results, unrelated)
	passes.WithLabelValues("StacksNode", "observe").Observe(0.25)
	passes.WithLabelValues("StacksNode", "observe").Observe(0.5)
	passes.WithLabelValues("StacksNode", "converge")
	results.WithLabelValues("scheduler", "success").Add(3)
	unrelated.Inc()
	series, err := apiaccounting.PassSnapshot(registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 {
		t.Fatalf("got %+v, want recorded pass series only", series)
	}
	results0, observe := series[0], series[1]
	if results0.Metric != "controller_runtime_reconcile_total" || results0.Count != 3 || results0.Seconds != 0 ||
		results0.Labels["controller"] != "scheduler" {
		t.Fatalf("reconcile results: %+v", results0)
	}
	if observe.Labels["path"] != "observe" || observe.Count != 2 || observe.Seconds != 0.75 {
		t.Fatalf("observation passes: %+v", observe)
	}
}

package apiaccounting

import (
	"context"
	"sort"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
)

// passMetrics are the reconcile count and duration families logged as pass accounting.
var passMetrics = map[string]bool{
	"controller_runtime_reconcile_total":        true,
	"controller_runtime_reconcile_time_seconds": true,
	"stacks_network_actor_pass_seconds":         true,
}

// PassSeries is one cumulative reconcile total; Seconds is set only for durations.
type PassSeries struct {
	Metric  string            `json:"metric"`
	Labels  map[string]string `json:"labels"`
	Count   float64           `json:"count"`
	Seconds float64           `json:"seconds,omitempty"`
}

// PassSnapshot returns the non-zero pass series in g.
func PassSnapshot(g prometheus.Gatherer) ([]PassSeries, error) {
	families, err := g.Gather()
	if err != nil {
		return nil, err
	}
	var out []PassSeries
	for _, family := range families {
		if !passMetrics[family.GetName()] {
			continue
		}
		for _, metric := range family.GetMetric() {
			series := PassSeries{Metric: family.GetName(), Labels: map[string]string{}}
			for _, label := range metric.GetLabel() {
				series.Labels[label.GetName()] = label.GetValue()
			}
			if h := metric.GetHistogram(); h != nil {
				series.Count, series.Seconds = float64(h.GetSampleCount()), h.GetSampleSum()
			} else {
				series.Count = metric.GetCounter().GetValue()
			}
			if series.Count > 0 {
				out = append(out, series)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out, nil
}

// ReportPasses logs cumulative pass series at startup, every interval and at shutdown.
func ReportPasses(ctx context.Context, log logr.Logger, g prometheus.Gatherer, interval time.Duration) {
	report(ctx, interval, func() {
		series, err := PassSnapshot(g)
		if err != nil {
			log.Error(err, "pass metrics unavailable")
			return
		}
		log.Info("cumulative", "series", series)
	})
}

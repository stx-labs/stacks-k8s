package bitcoincontrol

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
)

func TestPendingOfferRefusalsAreClassifiedByStage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testFixture)
		stage  refusalStage
	}{
		{"authorization", func(f *testFixture) {
			record := f.readRecord(t)
			record.Spec.Offer.Address = "different"
			if err := f.c.Update(context.Background(), record); err != nil {
				t.Fatal(err)
			}
		}, refusalAuthority},
		{"preflight", func(f *testFixture) { f.rpc.height-- }, refusalPreflight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.offer(t, 202)
			tc.mutate(f)
			err := f.worker.Step(context.Background())
			var refused *offerRefused
			if !errors.As(err, &refused) || refused.number != 1 || refused.stage != tc.stage {
				t.Fatalf("refusal = %v, want offer 1 at %s", err, tc.stage)
			}
			if f.rpc.count() != 0 {
				t.Fatal("a refused offer was sent")
			}
		})
	}
	f := newFixture(t)
	if err := f.worker.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.rpc.height--
	var refused *offerRefused
	if err := f.worker.Step(context.Background()); errors.As(err, &refused) {
		t.Fatalf("an error without a pending offer was classified as a refusal: %v", err)
	}
}

func TestRefusalLogReportsEachDistinctRefusalOnceAndItsClearance(t *testing.T) {
	var lines []string
	log := funcr.New(func(_, args string) { lines = append(lines, args) }, funcr.Options{})
	var l refusalLog
	start := time.Date(2026, 11, 20, 10, 38, 8, 0, time.UTC)
	refuse := func(number int64, stage refusalStage, message string) error {
		return &offerRefused{number: number, stage: stage, err: errors.New(message)}
	}
	steps := []struct {
		err   error
		after time.Duration
	}{
		{refuse(7, refusalAuthority, "current bootstrap advancement unavailable"), 0},
		{refuse(7, refusalAuthority, "current bootstrap advancement unavailable"), time.Second},
		{errors.New("execution record read failed"), 2 * time.Second},
		{refuse(7, refusalObservation, "native observation unavailable"), 3 * time.Second},
		{nil, 31 * time.Second},
		{nil, 32 * time.Second},
		{refuse(8, refusalPreflight, strings.Repeat("x", 2*refusalMessageLimit)), 40 * time.Second},
	}
	for _, step := range steps {
		l.observe(log, step.err, start.Add(step.after))
	}
	want := []string{
		`"msg"="generation offer refused" "offer"=7 "stage"="Authorization"`,
		`"msg"="generation offer refused" "offer"=7 "stage"="Observation"`,
		`"msg"="generation offer refusal cleared" "offer"=7 "stage"="Observation" "refusedFor"="31s"`,
		`"msg"="generation offer refused" "offer"=8 "stage"="Preflight"`,
	}
	if len(lines) != len(want) {
		t.Fatalf("logged %d lines, want %d: %q", len(lines), len(want), lines)
	}
	for i := range want {
		if !strings.HasPrefix(lines[i], `"level"=0 `+want[i]) {
			t.Fatalf("line %d = %s, want prefix %s", i, lines[i], want[i])
		}
	}
	if !strings.Contains(lines[3], `"error"="`+strings.Repeat("x", refusalMessageLimit)+`"`) {
		t.Fatalf("message not bounded: %s", lines[3])
	}
}

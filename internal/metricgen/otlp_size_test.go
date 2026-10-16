package metricgen

import (
	"slices"
	"testing"
	"time"

	"clickcannon/internal/otlpsize"

	"google.golang.org/protobuf/proto"
)

func TestSyntheticSize(t *testing.T) {
	breakdowns, err := SyntheticSize(Config{})
	if err != nil {
		t.Fatal(err)
	}
	var signals []string
	for _, bd := range breakdowns {
		signals = append(signals, bd.Signal)
		for _, size := range []otlpsize.AttrSize{bd.ResourceAttrs, bd.ScopeAttrs, bd.SignalAttrs} {
			if size.Min > size.Expected || size.Expected > size.Max {
				t.Errorf("%s attr size %+v: want min <= expected <= max", bd.Signal, size)
			}
		}
	}
	want := []string{"metrics/gauge", "metrics/sum", "metrics/histogram", "metrics/exponential_histogram", "metrics/summary"}
	if !slices.Equal(signals, want) {
		t.Fatalf("signals = %v, want %v", signals, want)
	}
}

func TestSyntheticSizeContainsGeneratedPoint(t *testing.T) {
	cfg := Config{MetricNames: []string{"a"}, MetricTypes: []string{"summary"}, MetricCardinalities: []int{1}}
	breakdowns, err := SyntheticSize(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(breakdowns) != 1 || breakdowns[0].Signal != "metrics/summary" {
		t.Fatalf("breakdowns = %+v, want one metrics/summary breakdown", breakdowns)
	}

	p, err := newPlan(cfg.withDefaults(), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b := newBuilder(p)
	b.addPoint(p.metrics[0], 0, 0)
	rm := b.build().ResourceMetrics[0]
	dp := rm.ScopeMetrics[0].Metrics[0].GetSummary().DataPoints[0]

	want := otlpsize.AttrSize{Expected: proto.Size(rm.Resource), Min: proto.Size(rm.Resource), Max: proto.Size(rm.Resource)}
	if got := breakdowns[0].ResourceAttrs; got != want {
		t.Errorf("resource attrs = %+v, want %+v", got, want)
	}
	pointBytes := otlpsize.AttrBytes(dp.Attributes...)
	if got := breakdowns[0].SignalAttrs; got != (otlpsize.AttrSize{Expected: pointBytes, Min: pointBytes, Max: pointBytes}) {
		t.Errorf("signal attrs = %+v, want %d bytes", got, pointBytes)
	}
}

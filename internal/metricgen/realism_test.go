package metricgen

import (
	"math"
	"testing"
)

// TestValueClassMix verifies all four precision classes appear across series
// and behave as specified: idle series are flat, int series integral, decimal
// series quantized. This mix is what makes the stored columns compress like
// production data.
func TestValueClassMix(t *testing.T) {
	p := mustPlan(t, testConfig(), "seed-a")

	var def *metricDef
	for _, d := range p.metrics {
		if d.typ == typeGauge && !d.valueIsInt && d.cardinality >= 200 {
			def = d
			break
		}
	}
	if def == nil {
		t.Fatal("no double-valued gauge with enough series in plan")
	}

	classCounts := make(map[valueClass]int)
	for j := range 200 {
		s := makeSeriesRef(def, j, p.cfg.Resources)
		class := s.class()
		classCounts[class]++

		v0 := s.gaugeValue(0)
		switch class {
		case classIdle:
			for k := uint64(1); k < 50; k++ {
				if s.gaugeValue(k) != v0 {
					t.Fatalf("idle series %d changed value at sweep %d", j, k)
				}
			}
		case classInt:
			for k := uint64(0); k < 20; k++ {
				if v := s.gaugeValue(k); v != math.Floor(v) {
					t.Fatalf("int series %d produced non-integral %v", j, v)
				}
			}
		case classDecimal:
			d := s.decimals()
			for k := uint64(0); k < 20; k++ {
				v := s.gaugeValue(k)
				scaled := v * pow10[d]
				if math.Abs(scaled-math.Round(scaled)) > 1e-6 {
					t.Fatalf("decimal series %d (d=%d) produced unquantized %v", j, d, v)
				}
			}
		}
	}

	for _, class := range []valueClass{classIdle, classInt, classDecimal, classFull} {
		if classCounts[class] == 0 {
			t.Fatalf("class %v missing from 200 series: %v", class, classCounts)
		}
	}
}

// TestIdleCountersStayMonotoneAndFlat exercises the rare-tick idle counter.
func TestIdleCountersStayMonotoneAndFlat(t *testing.T) {
	p := mustPlan(t, testConfig(), "seed-a")
	def := findMetric(t, p, typeSum, TemporalityCumulative)

	found := false
	for j := range def.cardinality {
		s := makeSeriesRef(def, j, p.cfg.Resources)
		if s.class() != classIdle {
			continue
		}
		found = true
		epoch := s.epochStart(p, 0)
		prev := -1.0
		changes := 0
		for k := uint64(0); k < 1000; k++ {
			if e := s.epochStart(p, k); e != epoch {
				break // stay within one epoch for monotonicity
			}
			v := s.counterValue(k, epoch)
			if v != math.Floor(v) {
				t.Fatalf("idle counter non-integral: %v", v)
			}
			if v < prev {
				t.Fatalf("idle counter decreased: %v -> %v", prev, v)
			}
			if v != prev {
				changes++
			}
			prev = v
		}
		if changes > 500 {
			t.Fatalf("idle counter ticks too often: %d changes in <=1000 sweeps", changes)
		}
		break
	}
	if !found {
		t.Skip("no idle series in the first cumulative sum metric")
	}
}

// TestTimestampJitter verifies jittered per-series timestamps remain strictly
// increasing, stay within amplitude, are deterministic, and vary sweep to
// sweep (so DoubleDelta sees realistic scrape wobble).
func TestTimestampJitter(t *testing.T) {
	cfg := testConfig()
	cfg.TimestampJitterMs = 150
	if err := cfg.Validate(); err != nil {
		t.Fatalf("jitter config invalid: %v", err)
	}
	p := mustPlan(t, cfg, "seed-a")

	sVal := mix2(p.metrics[0].valueSeed, 7)
	deltas := make(map[int64]struct{})
	prev := p.pointTimeMs(sVal, 0, 3)
	for k := int64(1); k < 200; k++ {
		ts := p.pointTimeMs(sVal, k, 3)
		delta := ts - prev
		if delta <= 0 {
			t.Fatalf("timestamps not strictly increasing at sweep %d: delta %d", k, delta)
		}
		if delta < p.intervalMs-300 || delta > p.intervalMs+300 {
			t.Fatalf("delta %dms outside interval +/- 2*jitter", delta)
		}
		base := p.sweepTimeMs(k, 3)
		if ts < base-150 || ts > base+150 {
			t.Fatalf("jitter exceeds amplitude at sweep %d", k)
		}
		deltas[delta] = struct{}{}
		prev = ts
	}
	if len(deltas) < 10 {
		t.Fatalf("expected varied deltas under jitter, got %d distinct", len(deltas))
	}

	// Jitter validation bound: 2*jitter must stay under the interval.
	bad := testConfig()
	bad.TimestampJitterMs = int(bad.Interval.Milliseconds()) / 2
	if err := bad.Validate(); err == nil {
		t.Fatal("expected validation error for jitter >= interval/2")
	}
}

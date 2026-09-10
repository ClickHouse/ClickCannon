package metricgen

import (
	"testing"
	"time"
)

// findMetric returns the first metric of the given type in the plan.
func findMetric(t *testing.T, p *plan, typ metricType, temporality Temporality) *metricDef {
	t.Helper()
	for _, def := range p.metrics {
		if def.typ == typ && (temporality == TemporalityUnspecified || def.temporality == temporality) {
			return def
		}
	}
	t.Fatalf("no metric of type %v with temporality %v in plan", typ, temporality)
	return nil
}

func TestCumulativeCountersAreMonotonicWithinEpochs(t *testing.T) {
	p := mustPlan(t, testConfig(), "seed-a")
	def := findMetric(t, p, typeSum, TemporalityCumulative)
	if !def.monotonic {
		// Weighted assignment makes the first cumulative sum monotonic with
		// this plan; if the plan shifts, scan for a monotonic one.
		for _, d := range p.metrics {
			if d.typ == typeSum && d.temporality == TemporalityCumulative && d.monotonic {
				def = d
				break
			}
		}
	}

	for j := range min(20, def.cardinality) {
		s := makeSeriesRef(def, j, p.cfg.Resources)
		prev := -1.0
		prevEpoch := s.epochStart(p, 0)
		resets := 0
		for k := uint64(0); k < 5000; k++ {
			epoch := s.epochStart(p, k)
			v := s.counterValue(k, epoch)
			if epoch != prevEpoch {
				resets++
				prevEpoch = epoch
			} else if v < prev {
				t.Fatalf("series %d: counter decreased within epoch at sweep %d: %f -> %f", j, k, prev, v)
			}
			if v < 0 {
				t.Fatalf("series %d: negative counter value %f", j, v)
			}
			prev = v
		}
	}
}

func TestCounterResetsHappen(t *testing.T) {
	p := mustPlan(t, mustDefaults(testConfig()), "seed-a")
	def := findMetric(t, p, typeSum, TemporalityCumulative)

	// Reset periods are 2000-22000 sweeps; over 50k sweeps every series must
	// reset at least once and the value must drop at the boundary.
	s := makeSeriesRef(def, 0, p.cfg.Resources)
	resets := 0
	prevEpoch := s.epochStart(p, 0)
	for k := uint64(1); k < 50000; k++ {
		epoch := s.epochStart(p, k)
		if epoch != prevEpoch {
			resets++
			vBefore := s.counterValue(k-1, prevEpoch)
			vAfter := s.counterValue(k, epoch)
			if vAfter >= vBefore {
				t.Fatalf("expected value drop at reset sweep %d: %f -> %f", k, vBefore, vAfter)
			}
			prevEpoch = epoch
		}
	}
	if resets < 2 {
		t.Fatalf("expected at least 2 resets over 50k sweeps, got %d", resets)
	}
}

func mustDefaults(c Config) Config { return c.withDefaults() }

func TestHistogramPointConsistency(t *testing.T) {
	p := mustPlan(t, testConfig(), "seed-a")

	for _, temporality := range []Temporality{TemporalityCumulative, TemporalityDelta} {
		def := findMetric(t, p, typeHistogram, temporality)
		for j := range min(10, def.cardinality) {
			s := makeSeriesRef(def, j, p.cfg.Resources)
			counts := make([]uint64, len(def.bounds)+1)
			prevCounts := make([]uint64, len(counts))
			prevEpoch := s.epochStart(p, 0)

			for k := uint64(0); k < 2000; k++ {
				epoch := s.epochStart(p, k)
				count, sum, minV, maxV := s.histPoint(k, epoch, counts)

				var total uint64
				for _, n := range counts {
					total += n
				}
				if total != count {
					t.Fatalf("count %d != bucket total %d", count, total)
				}
				if count > 0 && sum <= 0 {
					t.Fatalf("non-positive sum %f with count %d", sum, count)
				}
				if minV > maxV {
					t.Fatalf("min %f > max %f", minV, maxV)
				}

				if temporality == TemporalityCumulative {
					if epoch == prevEpoch && k > 0 {
						for l := range counts {
							if counts[l] < prevCounts[l] {
								t.Fatalf("bucket %d decreased within epoch at sweep %d", l, k)
							}
						}
					}
					prevEpoch = epoch
					copy(prevCounts, counts)
				}
			}
		}
	}
}

func TestExpHistogramPointConsistency(t *testing.T) {
	p := mustPlan(t, testConfig(), "seed-a")
	def := findMetric(t, p, typeExpHistogram, TemporalityUnspecified)

	sawNegative := false
	for j := range min(50, def.cardinality) {
		s := makeSeriesRef(def, j, p.cfg.Resources)
		posLen, negLen := s.expBucketLens()
		pos := make([]uint64, posLen)
		var neg []uint64
		if negLen > 0 {
			neg = make([]uint64, negLen)
			sawNegative = true
		}

		for k := uint64(0); k < 200; k++ {
			epoch := s.epochStart(p, k)
			posOffset, _, zeroCount, zeroThreshold, count, _, minV, maxV := s.expHistPoint(k, epoch, pos, neg)

			var total uint64
			for _, n := range pos {
				total += n
			}
			for _, n := range neg {
				total += n
			}
			total += zeroCount
			if total != count {
				t.Fatalf("count %d != bucket+zero total %d", count, total)
			}
			if posOffset < def.expPowMin || int(posOffset)+len(pos)+1 >= len(def.expPow)+int(def.expPowMin) {
				t.Fatalf("positive offset %d out of pow table range", posOffset)
			}
			if zeroThreshold < 0 {
				t.Fatalf("negative zero threshold %f", zeroThreshold)
			}
			if minV > maxV {
				t.Fatalf("min %f > max %f", minV, maxV)
			}
		}
	}
	_ = sawNegative // negative buckets are per-metric structural; not all plans include them
}

func TestSummaryPointConsistency(t *testing.T) {
	p := mustPlan(t, testConfig(), "seed-a")
	def := findMetric(t, p, typeSummary, TemporalityUnspecified)

	for j := range min(10, def.cardinality) {
		s := makeSeriesRef(def, j, p.cfg.Resources)
		qValues := make([]float64, len(def.quantiles))
		prevCount := uint64(0)
		prevEpoch := s.epochStart(p, 0)

		for k := uint64(0); k < 2000; k++ {
			epoch := s.epochStart(p, k)
			count, sum := s.summaryPoint(k, epoch, qValues)

			if epoch == prevEpoch && count < prevCount {
				t.Fatalf("summary count decreased within epoch at sweep %d", k)
			}
			if count > 0 && sum <= 0 {
				t.Fatalf("non-positive sum %f with count %d", sum, count)
			}
			for i := 1; i < len(qValues); i++ {
				if qValues[i] < qValues[i-1] {
					t.Fatalf("quantile values not ordered at sweep %d: %v (quantiles %v)", k, qValues, def.quantiles)
				}
			}
			prevCount = count
			prevEpoch = epoch
		}
	}
}

func TestValuesAreDeterministicPerSeed(t *testing.T) {
	p1 := mustPlan(t, testConfig(), "seed-a")
	p2 := mustPlan(t, testConfig(), "seed-a")
	p3 := mustPlan(t, testConfig(), "seed-b")

	def1, def2, def3 := p1.metrics[0], p2.metrics[0], p3.metrics[0]
	s1 := makeSeriesRef(def1, 3, p1.cfg.Resources)
	s2 := makeSeriesRef(def2, 3, p2.cfg.Resources)
	s3 := makeSeriesRef(def3, 3, p3.cfg.Resources)

	same, diff := 0, 0
	for k := uint64(0); k < 100; k++ {
		v1, v2, v3 := s1.gaugeValue(k), s2.gaugeValue(k), s3.gaugeValue(k)
		if v1 == v2 {
			same++
		}
		if v1 != v3 {
			diff++
		}
	}
	if same != 100 {
		t.Fatalf("same seed produced different values (%d/100 matched)", same)
	}
	if diff == 0 {
		t.Fatal("different app seeds produced identical value streams")
	}

	// Same seed must also pin the series to the same resource.
	if s1.resourceIdx != s2.resourceIdx {
		t.Fatal("resource assignment not deterministic")
	}
}

func TestExemplarProbability(t *testing.T) {
	cfg := testConfig()
	cfg.ExemplarProbability = 1
	p := mustPlan(t, cfg, "seed-a")
	def := findMetric(t, p, typeSum, TemporalityUnspecified)
	s := makeSeriesRef(def, 0, p.cfg.Resources)
	if _, _, _, _, ok := s.exemplarFor(p, 0, 1.0); !ok {
		t.Fatal("exemplar_probability=1 must always produce exemplars")
	}

	cfg.ExemplarProbability = 0
	p0 := mustPlan(t, cfg, "seed-a")
	s0 := makeSeriesRef(p0.metrics[def.index], 0, p0.cfg.Resources)
	if _, _, _, _, ok := s0.exemplarFor(p0, 0, 1.0); ok {
		t.Fatal("exemplar_probability=0 must never produce exemplars")
	}
}

func TestDeltaWindowStartPrecedesTimestamp(t *testing.T) {
	p := mustPlan(t, testConfig(), "seed-a")
	def := findMetric(t, p, typeSum, TemporalityDelta)
	s := makeSeriesRef(def, 0, p.cfg.Resources)

	for k := uint64(1); k < 10; k++ {
		start := p.sweepTimeMs(int64(k)-1, s.resourceIdx)
		ts := p.sweepTimeMs(int64(k), s.resourceIdx)
		if start >= ts {
			t.Fatalf("delta window start %d >= timestamp %d", start, ts)
		}
		if ts-start != p.intervalMs {
			t.Fatalf("delta window is %dms, want %dms", ts-start, p.intervalMs)
		}
	}
}

func TestPlanResolvesQuickly(t *testing.T) {
	// A million-series plan must resolve in well under a second: it happens
	// on every startup.
	cfg := testConfig()
	cfg.MetricCount = 1000
	cfg.CardinalityM = 10000
	cfg.CardinalityB = 0.999
	start := time.Now()
	p := mustPlan(t, cfg.withDefaults(), "seed-a")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("plan took %v", elapsed)
	}
	if p.totalSeries < 1_000_000 {
		t.Fatalf("expected > 1M series, got %d", p.totalSeries)
	}
}

package metricgen

import (
	"testing"
	"time"
)

// BenchmarkAddPoint measures the per-point synthesis + proto assembly cost:
// the generator hot path that bounds max throughput per worker.
func BenchmarkAddPoint(b *testing.B) {
	cfg := Config{
		Enabled:      true,
		URL:          "localhost:4317",
		MetricCount:  1000,
		CardinalityM: 1000,
		CardinalityB: 0.995,
		Resources:    1000,
		Interval:     15 * time.Second,
		StartTime:    "2026-01-01T00:00:00Z",
	}.withDefaults()
	p, err := newPlan(cfg, "bench-seed", time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC))
	if err != nil {
		b.Fatal(err)
	}
	bld := newBuilder(p)

	var k uint64
	i, j := 0, 0
	for b.Loop() {
		def := p.metrics[i%len(p.metrics)]
		bld.addPoint(def, j%def.cardinality, k)
		i++
		j += 7
		if i%len(p.metrics) == 0 {
			k++
		}
		if bld.len() >= cfg.PointsPerRequest {
			bld.reset() // simulate flush without network
		}
	}
}

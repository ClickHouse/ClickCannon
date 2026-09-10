package metricgen

import (
	"math"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	cfg := Config{
		Enabled:      true,
		URL:          "localhost:4317",
		MetricCount:  100,
		CardinalityM: 1000,
		CardinalityB: 0.95,
		Resources:    50,
		Interval:     15 * time.Second,
		StartTime:    "2026-01-01T00:00:00Z",
	}
	return cfg.withDefaults()
}

func mustPlan(t *testing.T, cfg Config, seed string) *plan {
	t.Helper()
	p, err := newPlan(cfg, seed, time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("newPlan: %v", err)
	}
	return p
}

func TestMetricNamesStableAndUnique(t *testing.T) {
	names := metricNames(1000)
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		if _, ok := seen[n]; ok {
			t.Fatalf("duplicate name %q", n)
		}
		seen[n] = struct{}{}
		parts := strings.Split(n, "_")
		if len(parts) != 3 {
			t.Fatalf("name %q is not adj_adj_noun", n)
		}
		if parts[0] == parts[1] {
			t.Fatalf("name %q has duplicate adjectives", n)
		}
	}

	// The table is a fixed lookup table: prefixes must be identical across
	// calls and shorter requests must be a prefix of longer ones.
	again := metricNames(10)
	for i, n := range again {
		if names[i] != n {
			t.Fatalf("name table not stable at %d: %q vs %q", i, names[i], n)
		}
	}
}

func TestMetricNamesOverride(t *testing.T) {
	names := []string{"http.server.requests", "system.cpu.utilization", "db.client.query.duration"}
	cfg := Config{
		Enabled:      true,
		URL:          "localhost:4317",
		MetricNames:  names,
		CardinalityM: 10,
		CardinalityB: 1,
		Resources:    50,
		Interval:     15 * time.Second,
		StartTime:    "2026-01-01T00:00:00Z",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid metric_names config rejected: %v", err)
	}

	d := cfg.withDefaults()
	if d.MetricCount != len(names) {
		t.Fatalf("MetricCount = %d, want %d (forced to len(metric_names))", d.MetricCount, len(names))
	}

	p := mustPlan(t, d, "seed-a")
	if len(p.metrics) != len(names) {
		t.Fatalf("plan has %d metrics, want %d", len(p.metrics), len(names))
	}
	for i, def := range p.metrics {
		if def.name != names[i] {
			t.Fatalf("metric %d name = %q, want %q", i, def.name, names[i])
		}
	}

	dup := cfg
	dup.MetricNames = []string{"a.metric", "b.metric", "a.metric"}
	if err := dup.Validate(); err == nil {
		t.Fatal("expected validation error for duplicate metric_names")
	}

	empty := cfg
	empty.MetricNames = []string{"a.metric", ""}
	if err := empty.Validate(); err == nil {
		t.Fatal("expected validation error for empty metric name")
	}
}

func TestMetricTypesOverride(t *testing.T) {
	names := []string{"a.metric", "b.metric", "c.metric", "d.metric", "e.metric"}
	// Deliberately not what the default type cycle would assign (it starts
	// with gauge), so a pass proves the round-robin was bypassed.
	types := []string{"summary", "exponential_histogram", "histogram", "sum", "gauge"}
	cfg := Config{
		Enabled:      true,
		URL:          "localhost:4317",
		MetricNames:  names,
		MetricTypes:  types,
		CardinalityM: 10,
		CardinalityB: 1,
		Resources:    50,
		Interval:     15 * time.Second,
		StartTime:    "2026-01-01T00:00:00Z",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid metric_types config rejected: %v", err)
	}

	p := mustPlan(t, cfg.withDefaults(), "seed-a")
	for i, def := range p.metrics {
		if def.typ.String() != types[i] {
			t.Fatalf("metric %d (%s) type = %s, want %s", i, def.name, def.typ, types[i])
		}
	}

	// The type-specific structure must match the overridden type.
	if len(p.metrics[0].quantiles) == 0 {
		t.Fatal("summary metric has no quantiles")
	}
	if len(p.metrics[1].expPow) == 0 {
		t.Fatal("exponential histogram metric has no exp power table")
	}
	if len(p.metrics[2].bounds) == 0 {
		t.Fatal("histogram metric has no bounds")
	}
}

func TestMetricCardinalitiesOverride(t *testing.T) {
	names := []string{"a.metric", "b.metric", "c.metric"}
	cards := []int{7, 100, 3}
	cfg := Config{
		Enabled:             true,
		URL:                 "localhost:4317",
		MetricNames:         names,
		MetricCardinalities: cards,
		// Decay curve deliberately yields different values (950, 902, 857),
		// so a pass proves the overrides bypass it.
		CardinalityM: 1000,
		CardinalityB: 0.95,
		Resources:    50,
		Interval:     15 * time.Second,
		StartTime:    "2026-01-01T00:00:00Z",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid metric_cardinalities config rejected: %v", err)
	}

	p := mustPlan(t, cfg.withDefaults(), "seed-a")
	var total uint64
	for i, def := range p.metrics {
		if def.cardinality != cards[i] {
			t.Fatalf("metric %q cardinality = %d, want %d", def.name, def.cardinality, cards[i])
		}
		// Count the actual distinct series the generator produces.
		seen := make(map[string]struct{}, def.cardinality)
		attrs := make([]attrKV, 0, len(def.dims))
		for j := range def.cardinality {
			s := makeSeriesRef(def, j, p.cfg.Resources)
			attrs = attrs[:0]
			attrs = s.appendAttrs(attrs)
			var sb strings.Builder
			for _, kv := range attrs {
				sb.WriteString(kv.Key)
				sb.WriteByte('=')
				sb.WriteString(kv.Value.GetStringValue())
				sb.WriteByte(',')
			}
			seen[sb.String()] = struct{}{}
		}
		if len(seen) != cards[i] {
			t.Fatalf("metric %q generated %d distinct series, want exactly %d", def.name, len(seen), cards[i])
		}
		total += uint64(len(seen))
	}
	if want := uint64(7 + 100 + 3); p.totalSeries != want || total != want {
		t.Fatalf("total series = %d (plan) / %d (counted), want %d", p.totalSeries, total, want)
	}

	// Same config + seed must produce the identical structure across runs.
	p2 := mustPlan(t, cfg.withDefaults(), "seed-a")
	for i := range p.metrics {
		a, b := p.metrics[i], p2.metrics[i]
		if a.name != b.name || a.typ != b.typ || a.cardinality != b.cardinality || len(a.dims) != len(b.dims) {
			t.Fatalf("metric %d structure differs across identical runs", i)
		}
	}

	// The two override lists are independent: setting both composes.
	both := cfg
	both.MetricTypes = []string{"histogram", "gauge", "summary"}
	if err := both.Validate(); err != nil {
		t.Fatalf("valid combined overrides rejected: %v", err)
	}
	pb := mustPlan(t, both.withDefaults(), "seed-a")
	for i, def := range pb.metrics {
		if def.typ.String() != both.MetricTypes[i] {
			t.Fatalf("metric %d type = %s, want %s", i, def.typ, both.MetricTypes[i])
		}
		if def.cardinality != cards[i] {
			t.Fatalf("metric %d cardinality = %d, want %d", i, def.cardinality, cards[i])
		}
	}
}

func TestCardinalityDecay(t *testing.T) {
	cfg := testConfig()
	for x := 1; x <= cfg.MetricCount; x++ {
		want := max(1, int(math.Round(cfg.CardinalityM*math.Pow(cfg.CardinalityB, float64(x)))))
		if got := cfg.cardinality(x); got != want {
			t.Fatalf("cardinality(%d) = %d, want %d", x, got, want)
		}
	}

	// b = 1 gives uniform cardinality m.
	uniform := cfg
	uniform.CardinalityB = 1
	for _, x := range []int{1, 50, 100} {
		if got := uniform.cardinality(x); got != 1000 {
			t.Fatalf("uniform cardinality(%d) = %d, want 1000", x, got)
		}
	}
}

func TestPlanSeriesAreUniqueAndExact(t *testing.T) {
	p := mustPlan(t, testConfig(), "seed-a")

	for _, def := range p.metrics[:10] {
		product := 1
		for _, d := range def.dims {
			product *= len(d.values)
		}
		if product < def.cardinality {
			t.Fatalf("metric %q: dim product %d < cardinality %d", def.name, product, def.cardinality)
		}

		// Every series index must produce a unique attribute vector.
		seen := make(map[string]struct{}, def.cardinality)
		attrs := make([]attrKV, 0, len(def.dims))
		for j := range def.cardinality {
			s := makeSeriesRef(def, j, p.cfg.Resources)
			attrs = attrs[:0]
			attrs = s.appendAttrs(attrs)
			var sb strings.Builder
			for _, kv := range attrs {
				sb.WriteString(kv.Key)
				sb.WriteByte('=')
				sb.WriteString(kv.Value.GetStringValue())
				sb.WriteByte(',')
			}
			key := sb.String()
			if _, ok := seen[key]; ok {
				t.Fatalf("metric %q: duplicate attr vector for j=%d: %s", def.name, j, key)
			}
			seen[key] = struct{}{}
		}
	}
}

func TestDimCountsWithinConfiguredRange(t *testing.T) {
	// Defaults: 2-5 dims per metric.
	p := mustPlan(t, testConfig(), "seed-a")
	for _, def := range p.metrics {
		if len(def.dims) < 2 || len(def.dims) > 5 {
			t.Fatalf("metric %q has %d dims, want 2-5 (defaults)", def.name, len(def.dims))
		}
	}

	// Custom range, like a real ~10-dimension fleet.
	cfg := testConfig()
	cfg.AttributesPerMetricMin = 8
	cfg.AttributesPerMetricMax = 12
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid attribute range rejected: %v", err)
	}
	p = mustPlan(t, cfg, "seed-a")
	counts := make(map[int]struct{})
	for _, def := range p.metrics {
		if len(def.dims) < 8 || len(def.dims) > 12 {
			t.Fatalf("metric %q has %d dims, want 8-12", def.name, len(def.dims))
		}
		counts[len(def.dims)] = struct{}{}
		product := 1
		for _, d := range def.dims {
			product *= len(d.values)
		}
		if product < def.cardinality {
			t.Fatalf("metric %q: dim product %d < cardinality %d", def.name, product, def.cardinality)
		}
	}
	if len(counts) < 2 {
		t.Fatalf("expected varied dim counts across %d metrics, got only %v", len(p.metrics), counts)
	}

	// min == max pins the count exactly.
	fixed := testConfig()
	fixed.AttributesPerMetricMin = 10
	fixed.AttributesPerMetricMax = 10
	p = mustPlan(t, fixed, "seed-a")
	for _, def := range p.metrics {
		if len(def.dims) != 10 {
			t.Fatalf("metric %q has %d dims, want exactly 10", def.name, len(def.dims))
		}
	}
}

func TestSweepRotationCoversAllMetricsOncePerSweep(t *testing.T) {
	p := mustPlan(t, testConfig(), "seed-a")
	n := len(p.metrics)
	for _, k := range []uint64{0, 1, 7, uint64(n) - 1, uint64(n), uint64(n) + 3, 1 << 40} {
		seen := make(map[int]int, n)
		for i := range n {
			seen[p.metricAt(i, k).index]++
		}
		if len(seen) != n {
			t.Fatalf("sweep %d visited %d distinct metrics, want %d", k, len(seen), n)
		}
		for idx, c := range seen {
			if c != 1 {
				t.Fatalf("sweep %d visited metric %d %d times, want exactly once", k, idx, c)
			}
		}
	}

	// The starting offset advances by one metric per sweep, wrapping around.
	if p.metricAt(0, 0) != p.metrics[0] || p.metricAt(0, 1) != p.metrics[1] {
		t.Fatal("rotation offset should advance by one metric per sweep")
	}
	if p.metricAt(0, uint64(n)) != p.metrics[0] {
		t.Fatal("rotation offset should wrap after len(metrics) sweeps")
	}
}

func TestPlanStructureIndependentOfAppSeed(t *testing.T) {
	p1 := mustPlan(t, testConfig(), "seed-a")
	p2 := mustPlan(t, testConfig(), "seed-b")

	for i := range p1.metrics {
		a, b := p1.metrics[i], p2.metrics[i]
		if a.name != b.name || a.typ != b.typ || a.temporality != b.temporality ||
			a.cardinality != b.cardinality || a.unit != b.unit || len(a.dims) != len(b.dims) {
			t.Fatalf("metric %d structure differs across app seeds", i)
		}
		if a.valueSeed == b.valueSeed {
			t.Fatalf("metric %d value seed identical across different app seeds", i)
		}
	}
}

func TestPlanCoversAllTypes(t *testing.T) {
	p := mustPlan(t, testConfig(), "seed-a")
	for _, typ := range []string{"gauge", "sum", "histogram", "exponential_histogram", "summary"} {
		if p.typeCounts[typ] == 0 {
			t.Fatalf("no metrics of type %s", typ)
		}
	}

	// Weighted round-robin: counts should match the weight proportions
	// exactly for a multiple of the total weight (100 metrics, weights sum 100).
	w := p.cfg.TypeWeights
	if p.typeCounts["gauge"] != w.Gauge || p.typeCounts["sum"] != w.Sum ||
		p.typeCounts["histogram"] != w.Histogram ||
		p.typeCounts["exponential_histogram"] != w.ExponentialHistogram ||
		p.typeCounts["summary"] != w.Summary {
		t.Fatalf("type counts %v do not match weights %+v", p.typeCounts, w)
	}
}

func TestChurnGenerations(t *testing.T) {
	cfg := testConfig()
	cfg.ResourceLifetime = 150 * time.Second // 10 sweeps
	p := mustPlan(t, cfg, "seed-a")

	for r := range 10 {
		prev := p.generation(r, 0)
		changes := 0
		for k := uint64(1); k < 100; k++ {
			g := p.generation(r, k)
			if g < prev {
				t.Fatalf("generation went backwards at resource %d sweep %d", r, k)
			}
			if g != prev {
				changes++
				if start := p.generationStart(r, k); start != k {
					t.Fatalf("generationStart(%d, %d) = %d, want %d", r, k, start, k)
				}
			}
			prev = g
		}
		// 100 sweeps / 10-sweep lifetime ≈ 9-10 generation changes.
		if changes < 9 || changes > 10 {
			t.Fatalf("resource %d: %d generation changes over 100 sweeps, want 9-10", r, changes)
		}
	}

	// Different generations must produce different pod identities.
	r0a := p.resources.build(0, 0)
	r0b := p.resources.build(0, 1)
	if attrByKey(t, r0a.Attributes, "k8s.pod.name") == attrByKey(t, r0b.Attributes, "k8s.pod.name") {
		t.Fatal("pod name did not change across generations")
	}
	if attrByKey(t, r0a.Attributes, "service.name") != attrByKey(t, r0b.Attributes, "service.name") {
		t.Fatal("service name should be stable across generations")
	}
}

func attrByKey(t *testing.T, attrs []attrKV, key string) string {
	t.Helper()
	for _, kv := range attrs {
		if kv.Key == key {
			return kv.Value.GetStringValue()
		}
	}
	t.Fatalf("attribute %q not found", key)
	return ""
}

func TestValidateRejectsBadConfigs(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"missing url", func(c *Config) { c.URL = "" }},
		{"bad compression", func(c *Config) { c.Compression = "zstd" }},
		{"metric count too large", func(c *Config) { c.MetricCount = MaxMetricCount + 1 }},
		{"cardinality b too large", func(c *Config) { c.CardinalityB = 2 }},
		{"delta ratio out of range", func(c *Config) { c.DeltaRatio = f64ptr(1.5) }},
		{"bad start time", func(c *Config) { c.StartTime = "yesterday" }},
		{"lifetime below interval", func(c *Config) { c.ResourceLifetime = time.Second }},
		{"sub-ms interval", func(c *Config) { c.Interval = 100 * time.Microsecond }},
		{"per-metric cardinality too large", func(c *Config) { c.CardinalityM = 1e9; c.CardinalityB = 1 }},
		{"attributes min negative", func(c *Config) { c.AttributesPerMetricMin = -1 }},
		{"attributes max below min", func(c *Config) { c.AttributesPerMetricMin = 6; c.AttributesPerMetricMax = 3 }},
		{"attributes max too large", func(c *Config) { c.AttributesPerMetricMax = maxAttrsPerMetric + 1 }},
		{"staleness markers without churn", func(c *Config) { c.StalenessMarkers = true; c.ResourceLifetime = 0 }},
		{"metric_types without metric_names", func(c *Config) { c.MetricTypes = []string{"gauge"} }},
		{"metric_types length mismatch", func(c *Config) {
			c.MetricNames = []string{"a.metric", "b.metric"}
			c.MetricTypes = []string{"gauge"}
		}},
		{"metric_types invalid type", func(c *Config) {
			c.MetricNames = []string{"a.metric"}
			c.MetricTypes = []string{"counter"}
		}},
		{"metric_cardinalities without metric_names", func(c *Config) { c.MetricCardinalities = []int{5} }},
		{"metric_cardinalities length mismatch", func(c *Config) {
			c.MetricNames = []string{"a.metric", "b.metric"}
			c.MetricCardinalities = []int{5}
		}},
		{"metric_cardinality zero", func(c *Config) {
			c.MetricNames = []string{"a.metric"}
			c.MetricCardinalities = []int{0}
		}},
		{"metric_cardinality too large", func(c *Config) {
			c.MetricNames = []string{"a.metric"}
			c.MetricCardinalities = []int{maxSeriesPerMetric + 1}
		}},
	}
	for _, tc := range cases {
		cfg := testConfig()
		tc.mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected validation error", tc.name)
		}
	}

	good := testConfig()
	if err := good.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestStartTimeResolution(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		in   string
		want time.Time
	}{
		{"", now},
		{"now", now},
		{"now-24h", now.Add(-24 * time.Hour)},
		{"2026-01-01T00:00:00Z", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	} {
		cfg := Config{StartTime: tc.in}
		got, err := cfg.resolveStartTime(now)
		if err != nil {
			t.Fatalf("resolveStartTime(%q): %v", tc.in, err)
		}
		if !got.Equal(tc.want) {
			t.Fatalf("resolveStartTime(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSweepTimestamps(t *testing.T) {
	p := mustPlan(t, testConfig(), "seed-a")
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()

	for r := range 5 {
		t0 := p.sweepTimeMs(0, r)
		t1 := p.sweepTimeMs(1, r)
		if t1-t0 != p.intervalMs {
			t.Fatalf("sweep spacing = %dms, want %dms", t1-t0, p.intervalMs)
		}
		phase := t0 - start
		if phase < 0 || phase >= p.intervalMs {
			t.Fatalf("resource %d phase %dms outside [0, interval)", r, phase)
		}
	}

	aligned := testConfig()
	aligned.AlignedTimestamps = true
	pa := mustPlan(t, aligned, "seed-a")
	for r := range 5 {
		if pa.sweepTimeMs(3, r) != start+3*pa.intervalMs {
			t.Fatal("aligned timestamps should have no per-resource phase")
		}
	}
}

func TestGoldenFirstNames(t *testing.T) {
	// Pin the head of the lookup table: these names are shared vocabulary with
	// dashboards and query workloads. If this test fails, the table changed,
	// which breaks every saved query that references a metric by name.
	golden := []string{
		"emerald_twinkling_phoenix",
		"frozen_spectral_citadel",
		"obsidian_twinkling_threshold",
		"celestial_golden_cavern",
		"ancient_frozen_horizon",
	}
	names := metricNames(len(golden))
	for i := range golden {
		if names[i] != golden[i] {
			t.Fatalf("name table changed at %d: got %q, want %q", i, names[i], golden[i])
		}
	}
}

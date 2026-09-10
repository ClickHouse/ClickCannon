package metricgen

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
)

type metricType int

const (
	typeGauge metricType = iota
	typeSum
	typeHistogram
	typeExpHistogram
	typeSummary
)

func (t metricType) String() string {
	switch t {
	case typeGauge:
		return "gauge"
	case typeSum:
		return "sum"
	case typeHistogram:
		return "histogram"
	case typeExpHistogram:
		return "exponential_histogram"
	case typeSummary:
		return "summary"
	}
	return "unknown"
}

// parseMetricType maps a config string (metric_types entries) to its
// metricType; ok is false for unknown strings.
func parseMetricType(s string) (metricType, bool) {
	switch s {
	case "gauge":
		return typeGauge, true
	case "sum":
		return typeSum, true
	case "histogram":
		return typeHistogram, true
	case "exponential_histogram":
		return typeExpHistogram, true
	case "summary":
		return typeSummary, true
	}
	return 0, false
}

// planSeed1/2 are fixed: the structural plan (types, attribute keys, bounds,
// quantiles, scopes) is stable across runs regardless of app.seed, so saved
// queries and dashboards keep working. app.seed only varies point values.
const (
	planSeed1 = 0x6d65747269637067 // "metricpg"
	planSeed2 = 0x706c616e5f763031 // "plan_v01"
)

// attrKV aliases the OTLP attribute proto for brevity in hot paths.
type attrKV = *commonpb.KeyValue

// dim is one data point attribute dimension. Series index j is decomposed into
// mixed-radix digits across a metric's dims; each digit selects a prebuilt
// KeyValue, so per-metric series cardinality is exactly the configured value
// and attribute protos are shared across all points that use them.
type dim struct {
	key    string
	values []*commonpb.KeyValue
}

// histShape is one precomputed bucket weight vector (gaussian over bucket
// index, normalized to sum 1). Series pick a shape from the metric's LUT so
// the expensive exp() work happens once per metric, not per point.
type histShape struct {
	weights        []float64
	minIdx, maxIdx int // first/last bucket with non-negligible weight
}

// buildHistShapes precomputes 24 shapes (8 centers x 3 spreads) over n buckets.
func buildHistShapes(n int) []histShape {
	shapes := make([]histShape, 0, 24)
	for ci := range 8 {
		center := float64(n) * (float64(ci) + 0.5) / 8
		for _, spread := range []float64{0.08, 0.15, 0.3} {
			sigma := math.Max(0.5, float64(n)*spread)
			weights := make([]float64, n)
			total := 0.0
			for l := range n {
				d := (float64(l) + 0.5 - center) / sigma
				weights[l] = math.Exp(-0.5 * d * d)
				total += weights[l]
			}
			maxW := 0.0
			for l := range n {
				weights[l] /= total
				maxW = math.Max(maxW, weights[l])
			}
			minIdx, maxIdx := 0, n-1
			for l := range n {
				if weights[l] >= maxW*0.05 {
					minIdx = l
					break
				}
			}
			for l := n - 1; l >= 0; l-- {
				if weights[l] >= maxW*0.05 {
					maxIdx = l
					break
				}
			}
			shapes = append(shapes, histShape{weights: weights, minIdx: minIdx, maxIdx: maxIdx})
		}
	}
	return shapes
}

type scopeDef struct {
	scope     *commonpb.InstrumentationScope
	schemaURL string
}

// metricDef is the full structural definition of one metric.
type metricDef struct {
	index       int // 0-based position in the lookup table
	name        string
	typ         metricType
	temporality Temporality
	monotonic   bool // sums only
	valueIsInt  bool // gauge/sum only
	unit        string
	description string
	scopeIdx    int
	cardinality int
	dims        []dim

	// Histogram: explicit bounds plus per-bucket midpoints used for Sum
	// synthesis (len(mids) == len(bounds)+1).
	bounds []float64
	mids   []float64

	// Precomputed bucket weight shapes (histogram + exponential histogram).
	histShapes []histShape

	// Exponential histogram.
	expScale    int32
	hasNegative bool
	// expPow[i] = base^(i+expPowMin) where base = 2^(2^-scale); midpoints for
	// Sum synthesis are derived from this table.
	expPow    []float64
	expPowMin int32

	// Summary quantile levels (sorted ascending).
	quantiles []float64

	// structSeed drives per-series structural derivation (resource pick,
	// histogram shape); valueSeed additionally folds in app.seed and drives
	// point values.
	structSeed uint64
	valueSeed  uint64
}

type Temporality int

const (
	TemporalityUnspecified Temporality = iota
	TemporalityDelta
	TemporalityCumulative
)

// plan is the fully-resolved, immutable description of a run: every worker
// shares one plan and derives all series and values from it deterministically.
type plan struct {
	cfg     Config
	metrics []*metricDef
	scopes  []scopeDef

	resources *resourcePool

	startMs      int64 // virtual clock anchor (unix ms)
	intervalMs   int64
	jitterMs     int64  // per-point timestamp wobble amplitude (0 = exact spacing)
	lifetimeSwps uint64 // resource lifetime in sweeps; 0 = no churn

	totalSeries    uint64
	typeCounts     map[string]int
	typeSeries     map[string]uint64
	exemplarThresh uint64 // frac(hash) < p as a uint64 threshold; 0 = disabled
	seedHash       uint64
}

var attrKeyPool = []string{
	"http.method", "http.status_code", "http.route", "rpc.method", "rpc.service",
	"db.operation", "db.table", "messaging.destination", "queue.name", "operation",
	"endpoint", "cache.result", "error.type", "priority", "shard",
	"partition", "tier", "protocol", "direction", "status",
}

var scopePool = []scopeDef{
	{scope: &commonpb.InstrumentationScope{Name: "go.opentelemetry.io/otel/metric", Version: "1.43.0"}, schemaURL: "https://opentelemetry.io/schemas/1.27.0"},
	{scope: &commonpb.InstrumentationScope{Name: "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp", Version: "0.61.0"}, schemaURL: "https://opentelemetry.io/schemas/1.27.0"},
	{scope: &commonpb.InstrumentationScope{Name: "io.opentelemetry.sdk.metrics", Version: "1.38.0"}, schemaURL: "https://opentelemetry.io/schemas/1.26.0"},
	{scope: &commonpb.InstrumentationScope{Name: "io.opentelemetry.micrometer-1.5", Version: "2.9.0"}, schemaURL: ""},
	{scope: &commonpb.InstrumentationScope{Name: "opentelemetry.instrumentation.system_metrics", Version: "0.48b0"}, schemaURL: ""},
	{scope: &commonpb.InstrumentationScope{Name: "@opentelemetry/sdk-metrics", Version: "1.28.0"}, schemaURL: ""},
	{scope: &commonpb.InstrumentationScope{Name: "prometheus-receiver", Version: "0.157.0"}, schemaURL: ""},
	{scope: &commonpb.InstrumentationScope{Name: "clickcannon.metricgen", Version: "1.0.0"}, schemaURL: ""},
}

var histogramBoundPool = [][]float64{
	// Prometheus default latency buckets (seconds).
	{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	// Exponential latency buckets (milliseconds).
	{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384},
	// Payload size buckets (bytes).
	{128, 512, 2048, 8192, 32768, 131072, 524288, 2097152, 8388608},
	// Small linear buckets (e.g. queue depth, batch size).
	{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000},
	// Coarse latency SLO buckets (seconds).
	{0.1, 0.5, 1, 5, 30, 60},
}

var summaryQuantilePool = [][]float64{
	{0.5, 0.9, 0.99},
	{0.5, 0.75, 0.9, 0.95, 0.99},
	{0.95, 0.99, 0.999},
	{0.99},
	{0.5, 0.99, 0.999, 0.9999},
}

var unitPools = map[metricType][]string{
	typeGauge:        {"1", "%", "By", "s", "{connections}", "{threads}"},
	typeSum:          {"1", "{requests}", "By", "{errors}", "{messages}"},
	typeHistogram:    {"s", "ms", "By"},
	typeExpHistogram: {"s", "ms", "By"},
	typeSummary:      {"s", "ms"},
}

// typeCycle produces one deterministic cycle of metric type assignments using
// smooth weighted round-robin, so types are interleaved across the cardinality
// decay curve (every type gets head and tail metrics) while matching the
// weight proportions exactly per cycle.
func typeCycle(w TypeWeights) []metricType {
	entries := []struct {
		t      metricType
		weight int
	}{
		{typeGauge, w.Gauge},
		{typeSum, w.Sum},
		{typeHistogram, w.Histogram},
		{typeExpHistogram, w.ExponentialHistogram},
		{typeSummary, w.Summary},
	}
	total := w.total()
	cur := make([]int, len(entries))
	cycle := make([]metricType, total)
	for i := range total {
		best := -1
		for e := range entries {
			if entries[e].weight == 0 {
				continue
			}
			cur[e] += entries[e].weight
			if best == -1 || cur[e] > cur[best] {
				best = e
			}
		}
		cycle[i] = entries[best].t
		cur[best] -= total
	}
	return cycle
}

// newPlan resolves the config into the immutable run plan. cfg must already
// have defaults applied. now anchors "now"-relative start times.
func newPlan(cfg Config, seed string, now time.Time) (*plan, error) {
	start, err := cfg.resolveStartTime(now)
	if err != nil {
		return nil, err
	}

	p := &plan{
		cfg:        cfg,
		scopes:     scopePool,
		startMs:    start.UnixMilli(),
		intervalMs: cfg.Interval.Milliseconds(),
		jitterMs:   int64(cfg.TimestampJitterMs),
		typeCounts: make(map[string]int),
		typeSeries: make(map[string]uint64),
		seedHash:   splitmix64(fnvHash(seed)),
	}
	if cfg.ResourceLifetime > 0 {
		p.lifetimeSwps = uint64(cfg.ResourceLifetime / cfg.Interval)
	}
	if cfg.ExemplarProbability >= 1 {
		// float64(MaxUint64) rounds to 2^64; converting that back to uint64
		// is architecture-dependent (saturates on arm64, wraps on amd64).
		p.exemplarThresh = math.MaxUint64
	} else if cfg.ExemplarProbability > 0 {
		p.exemplarThresh = uint64(cfg.ExemplarProbability * float64(math.MaxUint64))
	}

	names := cfg.MetricNames
	if len(names) == 0 {
		names = metricNames(cfg.MetricCount)
	}
	cycle := typeCycle(cfg.TypeWeights)
	p.metrics = make([]*metricDef, cfg.MetricCount)
	for i := range cfg.MetricCount {
		typ := cycle[i%len(cycle)]
		if i < len(cfg.MetricTypes) {
			// Explicit per-metric override (validated); bypasses the cycle.
			if t, ok := parseMetricType(cfg.MetricTypes[i]); ok {
				typ = t
			}
		}
		def := buildMetricDef(cfg, i, names[i], p.seedHash, typ)
		p.metrics[i] = def
		p.totalSeries += uint64(def.cardinality)
		p.typeCounts[def.typ.String()]++
		p.typeSeries[def.typ.String()] += uint64(def.cardinality)
	}

	p.resources = newResourcePool(cfg)
	return p, nil
}

func buildMetricDef(cfg Config, index int, name string, seedHash uint64, typ metricType) *metricDef {
	s1 := splitmix64(planSeed1 ^ uint64(index)*0x9E3779B97F4A7C15)
	rng := rand.New(rand.NewPCG(s1, splitmix64(s1^planSeed2)))

	def := &metricDef{
		index:       index,
		name:        name,
		typ:         typ,
		cardinality: cfg.cardinality(index + 1),
		scopeIdx:    rng.IntN(len(scopePool)),
		structSeed:  splitmix64(s1 ^ structDomain(uint64(index))),
		valueSeed:   splitmix64(seedHash ^ s1),
	}

	units := unitPools[def.typ]
	def.unit = units[rng.IntN(len(units))]
	def.description = fmt.Sprintf("Synthetic %s metric %q generated by ClickCannon.", def.typ, name)

	switch def.typ {
	case typeGauge:
		def.temporality = TemporalityUnspecified
		def.valueIsInt = def.unit == "By" || def.unit == "{connections}" || def.unit == "{threads}"
	case typeSum:
		def.temporality = pickTemporality(cfg, rng)
		def.monotonic = rng.Float64() < 0.85
		def.valueIsInt = def.unit != "By" && rng.Float64() < 0.5
	case typeHistogram:
		def.temporality = pickTemporality(cfg, rng)
		def.bounds = histogramBoundPool[rng.IntN(len(histogramBoundPool))]
		def.mids = bucketMids(def.bounds)
		def.histShapes = buildHistShapes(len(def.bounds) + 1)
	case typeExpHistogram:
		def.temporality = pickTemporality(cfg, rng)
		def.expScale = int32(rng.IntN(5) - 1) // [-1, 3]
		def.hasNegative = rng.Float64() < 0.1
		def.expPowMin = -20
		def.expPow = expPowTable(def.expScale, def.expPowMin, 80)
		def.histShapes = buildHistShapes(32) // exp windows index weights modulo their length
	case typeSummary:
		def.temporality = TemporalityUnspecified
		def.quantiles = summaryQuantilePool[rng.IntN(len(summaryQuantilePool))]
	}

	def.dims = buildDims(rng, def.cardinality, cfg.AttributesPerMetricMin, cfg.AttributesPerMetricMax)
	return def
}

// structDomain folds an index into a distinct seed domain for structural
// derivation so structural and value streams never overlap.
func structDomain(v uint64) uint64 { return v ^ 0x736572696573_5555 }

func pickTemporality(cfg Config, rng *rand.Rand) Temporality {
	if rng.Float64() < *cfg.DeltaRatio {
		return TemporalityDelta
	}
	return TemporalityCumulative
}

// bucketMids returns representative per-bucket values used to synthesize a
// consistent Sum from bucket counts: mid of each finite bucket, half of the
// first bound for the underflow bucket, 1.5x the last bound for overflow.
func bucketMids(bounds []float64) []float64 {
	mids := make([]float64, len(bounds)+1)
	mids[0] = bounds[0] / 2
	for i := 1; i < len(bounds); i++ {
		mids[i] = (bounds[i-1] + bounds[i]) / 2
	}
	mids[len(bounds)] = bounds[len(bounds)-1] * 1.5
	return mids
}

// expPowTable precomputes base^(min..min+n-1) for base = 2^(2^-scale).
func expPowTable(scale, minIdx int32, n int) []float64 {
	base := math.Pow(2, math.Pow(2, -float64(scale)))
	table := make([]float64, n)
	for i := range n {
		table[i] = math.Pow(base, float64(minIdx+int32(i)))
	}
	return table
}

// buildDims picks attribute keys and sizes such that the mixed-radix product
// over dims is >= cardinality, giving each series index a unique digit vector.
// The dim count is picked per metric within [minAttrs, maxAttrs].
func buildDims(rng *rand.Rand, cardinality, minAttrs, maxAttrs int) []dim {
	k := minAttrs + rng.IntN(maxAttrs-minAttrs+1)

	keys := make([]string, 0, k)
	seen := make(map[int]struct{}, k)
	for len(keys) < k {
		idx := rng.IntN(len(attrKeyPool))
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		keys = append(keys, attrKeyPool[idx])
	}

	sizes := dimSizes(rng, cardinality, k)
	dims := make([]dim, k)
	for d := range k {
		values := make([]*commonpb.KeyValue, sizes[d])
		for v := range sizes[d] {
			values[v] = &commonpb.KeyValue{
				Key:   keys[d],
				Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: attrValue(keys[d], v)}},
			}
		}
		dims[d] = dim{key: keys[d], values: values}
	}
	return dims
}

// dimSizes distributes cardinality across k dims: the first dim stays small
// and categorical (like http.method), the rest split the remainder evenly on
// a geometric scale. The product of sizes is always >= c.
func dimSizes(rng *rand.Rand, c, k int) []int {
	sizes := make([]int, k)
	remaining := c
	for d := range k {
		left := k - d
		if left == 1 {
			sizes[d] = max(1, remaining)
			break
		}
		t := int(math.Ceil(math.Pow(float64(remaining), 1/float64(left))))
		if d == 0 {
			t = min(t, 3+rng.IntN(6)) // small categorical head dim
		}
		t = max(1, t)
		sizes[d] = t
		remaining = (remaining + t - 1) / t
	}
	return sizes
}

// attrValue produces a stable, realistic value for an attribute key. Indexes
// beyond a key's natural pool get a numeric suffix so any dim size works.
func attrValue(key string, idx int) string {
	var pool []string
	switch key {
	case "http.method", "rpc.method":
		pool = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	case "http.status_code", "status":
		pool = []string{"200", "201", "204", "301", "304", "400", "401", "403", "404", "429", "500", "502", "503"}
	case "cache.result":
		pool = []string{"hit", "miss", "expired", "bypass"}
	case "direction":
		pool = []string{"inbound", "outbound"}
	case "protocol":
		pool = []string{"http/1.1", "http/2", "grpc", "amqp", "mqtt", "kafka"}
	case "error.type":
		pool = []string{"none", "timeout", "connection_reset", "dns", "throttled", "internal", "canceled"}
	case "priority", "tier":
		pool = []string{"p0", "p1", "p2", "p3"}
	case "db.operation":
		pool = []string{"select", "insert", "update", "delete", "batch"}
	case "http.route", "endpoint":
		return fmt.Sprintf("/api/v1/%s/%d", nouns[idx%len(nouns)], idx/len(nouns))
	case "rpc.service":
		return fmt.Sprintf("%s.%sService", nouns[idx%len(nouns)], adjectives[idx%len(adjectives)])
	case "db.table", "messaging.destination", "queue.name":
		return fmt.Sprintf("%s_%s_%d", adjectives[idx%len(adjectives)], nouns[(idx/3)%len(nouns)], idx/len(nouns))
	case "operation":
		return fmt.Sprintf("%s-%s", adjectives[idx%len(adjectives)], nouns[(idx/7)%len(nouns)])
	default:
		return fmt.Sprintf("%s-%d", nouns[idx%len(nouns)], idx)
	}
	if idx < len(pool) {
		return pool[idx]
	}
	return fmt.Sprintf("%s-%d", pool[idx%len(pool)], idx/len(pool))
}

// metricAt returns the metric at position i of sweep k. The start offset rotates one metric per sweep so saturation
// drops spread across metrics instead of starving the same ones; rotation only reorders emission within a sweep, so output stays deterministic.
func (p *plan) metricAt(i int, k uint64) *metricDef {
	n := len(p.metrics)
	return p.metrics[(i+int(k%uint64(n)))%n]
}

// sweepTimeMs returns the virtual timestamp for sweep k with an optional
// per-resource phase offset within the interval. k is signed so callers can
// reference the sweep before the run start (delta windows, pre-run epochs).
func (p *plan) sweepTimeMs(k int64, resourceIdx int) int64 {
	ts := p.startMs + k*p.intervalMs
	if !p.cfg.AlignedTimestamps {
		ts += int64(mix2(uint64(resourceIdx), 0x7068617365) % uint64(p.intervalMs))
	}
	return ts
}

// pointTimeMs is sweepTimeMs plus the per-point scrape jitter for one series.
// Jitter amplitude is < interval/2 (validated), so per-series timestamps stay
// strictly increasing; the same (series, sweep) always jitters identically,
// which keeps delta window boundaries and epoch start times consistent.
func (p *plan) pointTimeMs(sVal uint64, k int64, resourceIdx int) int64 {
	ts := p.sweepTimeMs(k, resourceIdx)
	if p.jitterMs > 0 {
		ts += int64(mix3(sVal, uint64(k), 0x6a697474)%uint64(2*p.jitterMs+1)) - p.jitterMs
	}
	return ts
}

// generation returns the churn generation of a resource at sweep k. Phase is
// staggered per resource so the whole fleet does not restart at once.
func (p *plan) generation(resourceIdx int, k uint64) uint64 {
	if p.lifetimeSwps == 0 {
		return 0
	}
	phase := mix2(uint64(resourceIdx), 0x6c696665) % p.lifetimeSwps
	return (k + phase) / p.lifetimeSwps
}

// generationChanged reports whether the resource churned between sweep k-1
// and k, i.e. sweep k is the first sweep of a new generation. Pure function
// of (resource, k) like everything else in the sweep model; false at k=0
// (nothing existed before the run) and when churn is disabled.
func (p *plan) generationChanged(resourceIdx int, k uint64) bool {
	if p.lifetimeSwps == 0 || k == 0 {
		return false
	}
	phase := mix2(uint64(resourceIdx), 0x6c696665) % p.lifetimeSwps
	return (k+phase)%p.lifetimeSwps == 0
}

// generationStart returns the first sweep of the resource's current
// generation at sweep k (0 if churn is disabled or the generation began
// before sweep 0).
func (p *plan) generationStart(resourceIdx int, k uint64) uint64 {
	if p.lifetimeSwps == 0 {
		return 0
	}
	phase := mix2(uint64(resourceIdx), 0x6c696665) % p.lifetimeSwps
	gen := (k + phase) / p.lifetimeSwps
	if gen == 0 {
		return 0
	}
	return gen*p.lifetimeSwps - phase
}

package metricgen

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// TypeWeights controls how metric types are distributed across the metric name
// table. Types are assigned by cycling through the weights (weighted
// round-robin on the metric index), so every type receives both
// high-cardinality head metrics and low-cardinality tail metrics.
type TypeWeights struct {
	Gauge                int `yaml:"gauge"`
	Sum                  int `yaml:"sum"`
	Histogram            int `yaml:"histogram"`
	ExponentialHistogram int `yaml:"exponential_histogram"`
	Summary              int `yaml:"summary"`
}

func (w TypeWeights) total() int {
	return w.Gauge + w.Sum + w.Histogram + w.ExponentialHistogram + w.Summary
}

// Config controls the OTLP metrics generator/exporter. Unlike disk/generate +
// insert/otel, this mode is fully self-contained: it synthesizes OTLP metrics
// directly (no block queue) and exports them to an OTLP/gRPC endpoint, e.g. an
// OTel Collector running the ClickHouse exporter with metrics_schema: v2.
type Config struct {
	Enabled bool `yaml:"enabled"`

	// URL is the OTLP/gRPC endpoint, e.g. "localhost:4317". A leading
	// "http://" / "https://" / "grpc://" scheme is accepted and stripped;
	// "http://" implies insecure.
	URL string `yaml:"url"`

	// Insecure disables transport security (plaintext gRPC).
	Insecure bool `yaml:"insecure"`

	// Compression is the gRPC compressor to use: "gzip" or "" / "none".
	Compression string `yaml:"compression"`

	// Headers are optional gRPC metadata sent with every export (e.g. auth tokens).
	Headers map[string]string `yaml:"headers"`

	// Timeout is the per-export-request deadline. Defaults to 30s.
	Timeout time.Duration `yaml:"timeout"`

	// Threads is the number of concurrent generator/exporter workers. Each
	// worker holds one gRPC connection and generates an interleaved shard of
	// the series space (series index striped by thread).
	Threads int `yaml:"threads"`

	// PointsPerRequest is the number of data points accumulated before an
	// export request is flushed. Not exact: a request flushes on the point
	// that crosses the threshold. Mind the receiving collector's gRPC message
	// limit (4 MiB by default): histogram-heavy payloads run ~500-700 bytes
	// per point uncompressed, so the 4000-point default stays safely under
	// it. Raise max_recv_msg_size_mib on the OTLP receiver to go bigger.
	PointsPerRequest int `yaml:"points_per_request"`

	// PointsPerSecond is the total data point rate across all workers.
	// 0 = unlimited.
	PointsPerSecond int `yaml:"points_per_second"`

	// FlushInterval bounds how long a partially-filled request waits before
	// being flushed when the configured rate is low. Defaults to 1s.
	FlushInterval time.Duration `yaml:"flush_interval"`

	// MetricCount is the number of unique metric names, taken from the fixed
	// lookup table (index 1..MetricCount).
	MetricCount int `yaml:"metric_count"`

	// MetricNames is an optional list of explicit metric names overriding the
	// built-in lookup table. When set, MetricCount is forced to
	// len(MetricNames). All other per-metric properties (type, unit,
	// cardinality, dims) remain derived by metric index as usual, unless
	// overridden by MetricTypes / MetricCardinalities below.
	MetricNames []string `yaml:"metric_names"`

	// MetricTypes is an optional per-metric type override, parallel to
	// MetricNames (requires metric_names and must have the same length). Each
	// entry is one of: gauge, sum, histogram, exponential_histogram, summary.
	// When set, these metrics bypass the type_weights round-robin assignment.
	// Independent of MetricCardinalities.
	MetricTypes []string `yaml:"metric_types"`

	// MetricCardinalities is an optional exact series count per metric,
	// parallel to MetricNames (requires metric_names and must have the same
	// length). When set, these metrics bypass the cardinality_m * b^x decay
	// curve; each entry must be >= 1. Independent of MetricTypes.
	MetricCardinalities []int `yaml:"metric_cardinalities"`

	// CardinalityM / CardinalityB define the expected series cardinality of
	// each metric via exponential decay: cardinality(x) = m * b^x, where x is
	// the 1-based index of the metric name in the lookup table. Results are
	// rounded and clamped to at least 1 series per metric. b = 1 gives every
	// metric the same cardinality m.
	CardinalityM float64 `yaml:"cardinality_m"`
	CardinalityB float64 `yaml:"cardinality_b"`

	// AttributesPerMetricMin / AttributesPerMetricMax bound how many data
	// point attribute dimensions each metric carries; the count is picked
	// deterministically per metric within [min, max]. Defaults 2 and 5.
	// Real fleets often run ~10 dimensions; raise these to match. Max 16.
	AttributesPerMetricMin int `yaml:"attributes_per_metric_min"`
	AttributesPerMetricMax int `yaml:"attributes_per_metric_max"`

	// Resources is the size of the simulated resource pool (think: pods).
	// Each series is pinned to one resource; resource attributes (service,
	// pod, node, region, etc.) ride along on every series the resource owns,
	// like a real fleet where one pod exports many series. Series cardinality
	// itself comes from data point attributes, so this does not need to scale
	// with total series. Prebuilt protos cost ~2 KiB each; keep this ≤ ~100k.
	Resources int `yaml:"resources"`

	// Services is the number of distinct service.name values spread across the
	// resource pool. 0 = derived (resources/20, min 1).
	Services int `yaml:"services"`

	// Interval is the virtual scrape interval: each sweep of the series space
	// advances every series' timestamp by this much. Virtual time is decoupled
	// from wall time: with a high rate limit, virtual time runs faster than
	// real time (backfill); with a low one, slower. Defaults to 15s.
	Interval time.Duration `yaml:"interval"`

	// StartTime anchors the virtual clock: "" or "now" = program start,
	// "now-<duration>" (e.g. "now-72h") = backfill from the past, or an
	// RFC3339 timestamp. To land a finite run in a known window, combine with
	// sweeps: the run covers [start_time, start_time + sweeps*interval].
	StartTime string `yaml:"start_time"`

	// Sweeps stops the generator after emitting this many sweeps (points per
	// series). 0 = run until the program is stopped.
	Sweeps int `yaml:"sweeps"`

	// AlignedTimestamps forces every series in a sweep to share one timestamp.
	// Default (false) staggers timestamps per resource within the interval,
	// like real scrape phase offsets.
	AlignedTimestamps bool `yaml:"aligned_timestamps"`

	// TimestampJitterMs adds a deterministic per-point wobble of up to +/- this
	// many milliseconds to every timestamp, like real scrape-duration jitter.
	// Without it, points are spaced exactly interval apart and the DoubleDelta
	// timestamp codec compresses unrealistically well. Must be less than half
	// the interval; 0 disables (exact spacing).
	TimestampJitterMs int `yaml:"timestamp_jitter_ms"`

	// ResourceLifetime enables series churn: after this much virtual time, a
	// resource "restarts" (new pod name + instance id), which makes every
	// series it owns a brand-new series (new SeriesHash) and resets its
	// cumulative counters. 0 = no churn. Must be >= interval when set.
	ResourceLifetime time.Duration `yaml:"resource_lifetime"`

	// StalenessMarkers emits one final NoRecordedValue marker point
	// (FLAG_NO_RECORDED_VALUE set, values zeroed) for every series of a
	// resource generation that churns out, timestamped at the first sweep
	// after the churn boundary and carrying the OUTGOING generation's
	// resource identity, exactly what the collector's prometheusreceiver
	// emits when a scrape target disappears. Purely a function of the sweep
	// index (the model stays stateless), so it is fully deterministic.
	// Covers all five point types. Requires resource_lifetime; off by default.
	StalenessMarkers bool `yaml:"staleness_markers"`

	// DeltaRatio is the fraction of sum/histogram/exponential-histogram
	// metrics that use delta temporality; the rest are cumulative. A pointer
	// so that an explicit 0 (all cumulative) is distinguishable from unset
	// (defaults to 0.25).
	DeltaRatio *float64 `yaml:"delta_ratio"`

	// ExemplarProbability is the per-point probability of attaching one
	// exemplar (sum/histogram/exponential-histogram points only).
	ExemplarProbability float64 `yaml:"exemplar_probability"`

	// TypeWeights distributes metric types across the name table.
	// Defaults to gauge:30 sum:30 histogram:20 exponential_histogram:10 summary:10.
	TypeWeights TypeWeights `yaml:"type_weights"`
}

const (
	defaultTimeout          = 30 * time.Second
	defaultFlushInterval    = time.Second
	defaultThreads          = 4
	defaultPointsPerRequest = 4000
	defaultInterval         = 15 * time.Second
	defaultMetricCount      = 1000
	defaultCardinalityM     = 1000
	defaultCardinalityB     = 0.99
	defaultResources        = 1000
	defaultDeltaRatio       = 0.25

	maxResources        = 1000000
	maxSeriesPerMetric  = 100_000_000
	maxTotalSeries      = 2_000_000_000
	warnTotalSeries     = 100_000_000
	maxHeadersPerExport = 64

	defaultAttrsPerMetricMin = 2
	defaultAttrsPerMetricMax = 5
	maxAttrsPerMetric        = 16 // must stay <= len(attrKeyPool); Validate guards both
)

var defaultTypeWeights = TypeWeights{Gauge: 30, Sum: 30, Histogram: 20, ExponentialHistogram: 10, Summary: 10}

// withDefaults returns a copy of the config with zero-valued tunables filled in.
func (c Config) withDefaults() Config {
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = defaultFlushInterval
	}
	if c.Threads <= 0 {
		c.Threads = defaultThreads
	}
	if c.PointsPerRequest <= 0 {
		c.PointsPerRequest = defaultPointsPerRequest
	}
	if c.Interval <= 0 {
		c.Interval = defaultInterval
	}
	if len(c.MetricNames) > 0 {
		c.MetricCount = len(c.MetricNames)
	}
	if c.MetricCount <= 0 {
		c.MetricCount = defaultMetricCount
	}
	if c.CardinalityM <= 0 {
		c.CardinalityM = defaultCardinalityM
	}
	if c.CardinalityB <= 0 {
		c.CardinalityB = defaultCardinalityB
	}
	if c.Resources <= 0 {
		c.Resources = defaultResources
	}
	if c.AttributesPerMetricMin == 0 {
		c.AttributesPerMetricMin = defaultAttrsPerMetricMin
	}
	if c.AttributesPerMetricMax == 0 {
		c.AttributesPerMetricMax = defaultAttrsPerMetricMax
	}
	if c.Services <= 0 {
		c.Services = max(1, c.Resources/20)
	}
	if c.DeltaRatio == nil {
		v := defaultDeltaRatio
		c.DeltaRatio = &v
	}
	if c.TypeWeights.total() <= 0 {
		c.TypeWeights = defaultTypeWeights
	}
	return c
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.URL == "" {
		return errors.New("must set url")
	}
	switch c.Compression {
	case "", "none", "gzip":
	default:
		return errors.New("compression must be one of: none, gzip")
	}
	if c.Threads < 0 {
		return errors.New("threads must be >= 0")
	}
	if c.MetricCount > MaxMetricCount {
		return fmt.Errorf("metric_count must be <= %d", MaxMetricCount)
	}
	if len(c.MetricNames) > MaxMetricCount {
		return fmt.Errorf("metric_names must have <= %d entries", MaxMetricCount)
	}
	seenNames := make(map[string]struct{}, len(c.MetricNames))
	for i, name := range c.MetricNames {
		if name == "" {
			return fmt.Errorf("metric_names[%d] must not be empty", i)
		}
		if _, ok := seenNames[name]; ok {
			return fmt.Errorf("metric_names contains duplicate name %q", name)
		}
		seenNames[name] = struct{}{}
	}
	if len(c.MetricTypes) > 0 {
		if len(c.MetricNames) == 0 {
			return errors.New("metric_types requires metric_names")
		}
		if len(c.MetricTypes) != len(c.MetricNames) {
			return fmt.Errorf("metric_types must have the same length as metric_names (%d vs %d)", len(c.MetricTypes), len(c.MetricNames))
		}
		for i, s := range c.MetricTypes {
			if _, ok := parseMetricType(s); !ok {
				return fmt.Errorf("metric_types[%d]: invalid type %q; must be one of: gauge, sum, histogram, exponential_histogram, summary", i, s)
			}
		}
	}
	if len(c.MetricCardinalities) > 0 {
		if len(c.MetricNames) == 0 {
			return errors.New("metric_cardinalities requires metric_names")
		}
		if len(c.MetricCardinalities) != len(c.MetricNames) {
			return fmt.Errorf("metric_cardinalities must have the same length as metric_names (%d vs %d)", len(c.MetricCardinalities), len(c.MetricNames))
		}
		for i, card := range c.MetricCardinalities {
			if card < 1 {
				return fmt.Errorf("metric_cardinalities[%d] must be >= 1 (got %d)", i, card)
			}
			if card > maxSeriesPerMetric {
				return fmt.Errorf("metric_cardinalities[%d] must be <= %d", i, maxSeriesPerMetric)
			}
		}
	}
	if c.CardinalityB > 1.5 {
		return errors.New("cardinality_b must be <= 1.5 (use b <= 1 for decay)")
	}
	if c.Resources > maxResources {
		return fmt.Errorf("resources must be <= %d", maxResources)
	}
	if c.DeltaRatio != nil && (*c.DeltaRatio < 0 || *c.DeltaRatio > 1) {
		return errors.New("delta_ratio must be within [0, 1]")
	}
	if c.ExemplarProbability < 0 || c.ExemplarProbability > 1 {
		return errors.New("exemplar_probability must be within [0, 1]")
	}
	if c.TypeWeights.Gauge < 0 || c.TypeWeights.Sum < 0 || c.TypeWeights.Histogram < 0 ||
		c.TypeWeights.ExponentialHistogram < 0 || c.TypeWeights.Summary < 0 {
		return errors.New("type_weights must be non-negative")
	}
	if c.Sweeps < 0 {
		return errors.New("sweeps must be >= 0")
	}
	if len(c.Headers) > maxHeadersPerExport {
		return fmt.Errorf("headers must have <= %d entries", maxHeadersPerExport)
	}

	d := c.withDefaults()
	if d.Interval < time.Millisecond {
		return errors.New("interval must be >= 1ms")
	}
	if d.AttributesPerMetricMin < 1 {
		return errors.New("attributes_per_metric_min must be >= 1")
	}
	if lim := min(maxAttrsPerMetric, len(attrKeyPool)); d.AttributesPerMetricMax > lim {
		return fmt.Errorf("attributes_per_metric_max must be <= %d", lim)
	}
	if d.AttributesPerMetricMax < d.AttributesPerMetricMin {
		return errors.New("attributes_per_metric_max must be >= attributes_per_metric_min")
	}
	if c.TimestampJitterMs < 0 || int64(c.TimestampJitterMs)*2 >= d.Interval.Milliseconds() {
		return errors.New("timestamp_jitter_ms must be >= 0 and less than half the interval")
	}
	if d.ResourceLifetime != 0 && d.ResourceLifetime < d.Interval {
		return errors.New("resource_lifetime must be >= interval (or 0 to disable churn)")
	}
	if c.StalenessMarkers && d.ResourceLifetime == 0 {
		return errors.New("staleness_markers requires resource_lifetime (markers are emitted at churn boundaries)")
	}
	if _, err := d.resolveStartTime(time.Now()); err != nil {
		return fmt.Errorf("start_time: %w", err)
	}

	total, largest := d.totalSeries()
	if largest > maxSeriesPerMetric {
		return fmt.Errorf("cardinality_m * cardinality_b^x yields %d series for one metric; must be <= %d", largest, maxSeriesPerMetric)
	}
	if total > maxTotalSeries {
		return fmt.Errorf("configuration yields %d total series; must be <= %d", total, maxTotalSeries)
	}

	return nil
}

// cardinality returns the series count for the 1-based metric index x: the
// explicit metric_cardinalities entry when set (exact, validated >= 1),
// otherwise round(m * b^x), clamped to at least 1.
func (c Config) cardinality(x int) int {
	if x-1 < len(c.MetricCardinalities) {
		return c.MetricCardinalities[x-1]
	}
	y := c.CardinalityM * math.Pow(c.CardinalityB, float64(x))
	if y > float64(maxSeriesPerMetric)*2 {
		return maxSeriesPerMetric * 2 // sentinel over the per-metric cap; Validate rejects it
	}
	return max(1, int(math.Round(y)))
}

// totalSeries returns the summed cardinality across all metrics and the
// largest single-metric cardinality.
func (c Config) totalSeries() (total uint64, largest int) {
	for x := 1; x <= c.MetricCount; x++ {
		card := c.cardinality(x)
		total += uint64(card)
		largest = max(largest, card)
	}
	return total, largest
}

// resolveStartTime parses StartTime relative to now: "" / "now" = now,
// "now-<duration>" = backfill, otherwise RFC3339.
func (c Config) resolveStartTime(now time.Time) (time.Time, error) {
	s := strings.TrimSpace(c.StartTime)
	switch {
	case s == "" || s == "now":
		return now, nil
	case strings.HasPrefix(s, "now-"):
		d, err := time.ParseDuration(strings.TrimPrefix(s, "now-"))
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid now- offset %q: %w", s, err)
		}
		return now.Add(-d), nil
	default:
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}, fmt.Errorf("must be \"now\", \"now-<duration>\", or RFC3339: %w", err)
		}
		return t, nil
	}
}

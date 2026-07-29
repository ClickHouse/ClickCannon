package generate

import "errors"

// Config drives the synthetic data generator.
//
// The data shape comes from a code-defined Profile, picked by name via the
// Profile field. Profiles register themselves at init() time — see
// profile_otel_demo.go and profile_high_cardinality.go for examples.
type Config struct {
	Enabled       bool   `yaml:"enabled"`
	Threads       int    `yaml:"threads"`
	RowsPerBlock  int    `yaml:"rows_per_block"`
	RowsPerSecond uint64 `yaml:"rows_per_second"` // 0 = unlimited

	// Profile selects which code-defined profile to use (e.g. "otel_demo",
	// "high_cardinality"). If empty, defaults to "otel_demo".
	Profile string `yaml:"profile"`

	// Block pool settings
	ReuseBlocks         bool `yaml:"reuse_blocks"`
	BlockRetirementUses int  `yaml:"block_retirement_uses"`

	Traces   TracesConfig   `yaml:"traces"`
	Profiles ProfilesConfig `yaml:"profiles"`
	Metrics  MetricsConfig  `yaml:"metrics"`
}

// TracesConfig holds trace-tree shape parameters used by the traces filler.
type TracesConfig struct {
	SpansPerTraceMin int    `yaml:"spans_per_trace_min"`
	SpansPerTraceMax int    `yaml:"spans_per_trace_max"`
	MaxDepth         int    `yaml:"max_depth"`
	DurationMinUs    uint64 `yaml:"duration_min_us"`
	DurationMaxUs    uint64 `yaml:"duration_max_us"`
}

// MetricsConfig holds datapoint-shape parameters used by the metrics filler.
type MetricsConfig struct {
	// Number of data points emitted per series (uniform random between min and max).
	// All points in a series share MetricName, ServiceName, resource/scope/datapoint
	// attributes, and StartTimeUnix; TimeUnix advances by PointIntervalSeconds.
	PointsPerSeriesMin int `yaml:"points_per_series_min"`
	PointsPerSeriesMax int `yaml:"points_per_series_max"`
	// Seconds between consecutive points in a series (OTel collection interval).
	PointIntervalSeconds int `yaml:"point_interval_seconds"`
	// Number of explicit bucket bounds per histogram series.
	HistogramBuckets int `yaml:"histogram_buckets"`
	// Number of positive buckets per exponential histogram series.
	ExpHistogramBuckets int `yaml:"exp_histogram_buckets"`
	// Scale for exponential histograms.
	ExpHistogramScale int `yaml:"exp_histogram_scale"`
}

// ProfilesConfig holds sample-shape parameters used by the profiles filler.
type ProfilesConfig struct {
	SamplesPerProfileMin int    `yaml:"samples_per_profile_min"`
	SamplesPerProfileMax int    `yaml:"samples_per_profile_max"`
	StackDepthMin        int    `yaml:"stack_depth_min"`
	StackDepthMax        int    `yaml:"stack_depth_max"`
	DurationMinMs        uint64 `yaml:"duration_min_ms"`
	DurationMaxMs        uint64 `yaml:"duration_max_ms"`
	PeriodNs             int64  `yaml:"period_ns"`
}

func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Threads < 1 {
		return errors.New("must set threads to a value greater than zero")
	}

	if c.RowsPerBlock < 1 {
		return errors.New("must set rows_per_block to a value greater than zero")
	}

	if c.Traces.SpansPerTraceMin < 1 {
		c.Traces.SpansPerTraceMin = 1
	}
	if c.Traces.SpansPerTraceMax < c.Traces.SpansPerTraceMin {
		c.Traces.SpansPerTraceMax = c.Traces.SpansPerTraceMin
	}
	if c.Traces.MaxDepth < 1 {
		c.Traces.MaxDepth = 5
	}
	if c.Traces.DurationMinUs == 0 {
		c.Traces.DurationMinUs = 1000
	}
	if c.Traces.DurationMaxUs == 0 || c.Traces.DurationMaxUs < c.Traces.DurationMinUs {
		c.Traces.DurationMaxUs = 5000000
	}

	if c.Profiles.SamplesPerProfileMin < 1 {
		c.Profiles.SamplesPerProfileMin = 1
	}
	if c.Profiles.SamplesPerProfileMax < c.Profiles.SamplesPerProfileMin {
		c.Profiles.SamplesPerProfileMax = c.Profiles.SamplesPerProfileMin
	}
	if c.Profiles.StackDepthMin < 1 {
		c.Profiles.StackDepthMin = 1
	}
	if c.Profiles.StackDepthMax < c.Profiles.StackDepthMin {
		c.Profiles.StackDepthMax = c.Profiles.StackDepthMin
	}
	if c.Profiles.DurationMinMs == 0 {
		c.Profiles.DurationMinMs = 1000
	}
	if c.Profiles.DurationMaxMs == 0 || c.Profiles.DurationMaxMs < c.Profiles.DurationMinMs {
		c.Profiles.DurationMaxMs = 60000
	}
	if c.Profiles.PeriodNs == 0 {
		c.Profiles.PeriodNs = 10000000
	}

	if c.Metrics.PointsPerSeriesMin < 1 {
		c.Metrics.PointsPerSeriesMin = 10
	}
	if c.Metrics.PointsPerSeriesMax == 0 {
		c.Metrics.PointsPerSeriesMax = 120
	}
	if c.Metrics.PointsPerSeriesMax < c.Metrics.PointsPerSeriesMin {
		c.Metrics.PointsPerSeriesMax = c.Metrics.PointsPerSeriesMin
	}
	if c.Metrics.PointIntervalSeconds < 1 {
		c.Metrics.PointIntervalSeconds = 15
	}
	if c.Metrics.HistogramBuckets < 1 {
		c.Metrics.HistogramBuckets = 18
	}
	if c.Metrics.ExpHistogramBuckets < 1 {
		c.Metrics.ExpHistogramBuckets = 40
	}
	if c.Metrics.ExpHistogramScale == 0 {
		c.Metrics.ExpHistogramScale = 3
	}

	return nil
}

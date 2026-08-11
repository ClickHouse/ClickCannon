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

	// Custom fields configuration
	EnableCustomFields  bool   `yaml:"enable_custom_fields"`
	ProfileConfigFile   string `yaml:"profile_config_file"`

	// Block pool settings
	ReuseBlocks         bool `yaml:"reuse_blocks"`
	BlockRetirementUses int  `yaml:"block_retirement_uses"`

	Traces   TracesConfig   `yaml:"traces"`
	Profiles ProfilesConfig `yaml:"profiles"`
}

// TracesConfig holds trace-tree shape parameters used by the traces filler.
type TracesConfig struct {
	SpansPerTraceMin int    `yaml:"spans_per_trace_min"`
	SpansPerTraceMax int    `yaml:"spans_per_trace_max"`
	MaxDepth         int    `yaml:"max_depth"`
	DurationMinUs    uint64 `yaml:"duration_min_us"`
	DurationMaxUs    uint64 `yaml:"duration_max_us"`
}

// ProfilesConfig holds sample-shape parameters used by the profiles filler.
type ProfilesConfig struct {
	StackDepthMin int    `yaml:"stack_depth_min"`
	StackDepthMax int    `yaml:"stack_depth_max"`
	DurationMinMs uint64 `yaml:"duration_min_ms"`
	DurationMaxMs uint64 `yaml:"duration_max_ms"`
	PeriodNs      int64  `yaml:"period_ns"`
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

	return nil
}

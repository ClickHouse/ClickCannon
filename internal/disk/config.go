package disk

import "errors"

type Config struct {
	Enabled bool `yaml:"enabled"`
	// How many read threads
	Threads int `yaml:"threads"`

	LogsPath     string `yaml:"logs_path"`
	TracesPath   string `yaml:"traces_path"`
	ProfilesPath string `yaml:"profiles_path"`

	// Metrics paths, one per metric type. Default to metrics_<type>_data
	// when unset so existing configs keep working.
	MetricsGaugePath        string `yaml:"metrics_gauge_path"`
	MetricsSumPath          string `yaml:"metrics_sum_path"`
	MetricsHistogramPath    string `yaml:"metrics_histogram_path"`
	MetricsExpHistogramPath string `yaml:"metrics_exp_histogram_path"`
	MetricsSummaryPath      string `yaml:"metrics_summary_path"`

	ReuseBlocks bool `yaml:"reuse_blocks"`

	// BlockRetirementUses is the number of times a block can be acquired before it is retired
	// and replaced with a fresh allocation. This bounds the memory consumed by column slice
	// backing arrays that grow over time as blocks are filled and reset repeatedly.
	// Set to 0 to disable retirement (blocks live for the program's lifetime).
	// Only applies when reuse_blocks is true.
	BlockRetirementUses int `yaml:"block_retirement_uses"`

	Loop bool `yaml:"loop"`

	// How many MiB decompressed bytes per second can be read from disk
	MiBytesPerSecondLimit uint64 `yaml:"mb_per_second_limit"`

	ShiftTimestamp string `yaml:"shift_timestamp"`

	// Whether the disk files contain the TimestampTime column.
	// TimestampTime is derived from Timestamp on insert, so it is never inserted.
	// Set to true if the files on disk were exported with TimestampTime present.
	HasTimestampTime bool `yaml:"has_timestamp_time"`
}

func (c *Config) applyDefaults() {
	if c.MetricsGaugePath == "" {
		c.MetricsGaugePath = "metrics_gauge_data"
	}
	if c.MetricsSumPath == "" {
		c.MetricsSumPath = "metrics_sum_data"
	}
	if c.MetricsHistogramPath == "" {
		c.MetricsHistogramPath = "metrics_histogram_data"
	}
	if c.MetricsExpHistogramPath == "" {
		c.MetricsExpHistogramPath = "metrics_exp_histogram_data"
	}
	if c.MetricsSummaryPath == "" {
		c.MetricsSummaryPath = "metrics_summary_data"
	}
}

func (c *Config) Validate() error {
	c.applyDefaults()

	if !c.Enabled {
		return nil
	}

	if c.Threads < 1 {
		return errors.New("must set threads to a value greater than zero")
	}

	if c.LogsPath == "" {
		return errors.New("logs_path is empty")
	}

	if c.TracesPath == "" {
		return errors.New("traces_path is empty")
	}

	if c.ProfilesPath == "" {
		return errors.New("profiles_path is empty")
	}

	if c.MiBytesPerSecondLimit < 1 {
		return errors.New("must set mb_per_second_limit to a value greater than zero")
	}

	if c.ShiftTimestamp == "" {
		c.ShiftTimestamp = ShiftTimestampNone
	} else if c.ShiftTimestamp != ShiftTimestampNone && c.ShiftTimestamp != ShiftTimestampDate && c.ShiftTimestamp != ShiftTimestampAll && c.ShiftTimestamp != ShiftTimestampNow && c.ShiftTimestamp != ShiftTimestampMinute {
		return errors.New("must set shift_timestamp to one of: <empty>, none, date, all, now")
	}

	return nil
}

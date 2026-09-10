package user

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Config struct {
	Enabled              bool                 `yaml:"enabled"`
	Duration             time.Duration        `yaml:"duration"`
	Threads              int                  `yaml:"threads"`
	RampDuration         time.Duration        `yaml:"ramp_duration"`
	ClickHouseDSN        string               `yaml:"clickhouse_dsn"`
	ConnectionsPerThread int                  `yaml:"connections_per_thread"`
	Database             string               `yaml:"database"`
	Table                string               `yaml:"table"`
	DatasetUnixStart     uint64               `yaml:"dataset_unix_start"`
	DatasetUnixEnd       uint64               `yaml:"dataset_unix_end"`
	Workflows            []WorkflowBaseConfig `yaml:"workflows"`
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Threads < 1 {
		return errors.New("must set threads to a value greater than zero")
	}

	if c.ConnectionsPerThread < 1 {
		return errors.New("must set connections_per_thread to a value greater than zero")
	}

	if c.ClickHouseDSN == "" {
		return errors.New("must set clickhouse_dsn")
	}

	if c.Database == "" {
		return errors.New("must set database")
	}

	if c.Table == "" {
		return errors.New("must set table")
	}

	for len(c.Workflows) == 0 {
		return errors.New("must configure at least one user workflow")
	}

	for i, workflow := range c.Workflows {
		if workflow.Config == nil {
			return fmt.Errorf("workflows[%d]: unknown type %q must set type to one of: query, har", i, workflow.Type)
		}

		if workflow.Name == "" {
			return fmt.Errorf("workflows[%d] (type: %q): must set name", i, workflow.Type)
		}

		if err := workflow.Config.Validate(c); err != nil {
			return fmt.Errorf("workflows[%d] (type: %q, name: %q): %w", i, workflow.Type, workflow.Name, err)
		}
	}

	return nil
}

type WorkflowBaseConfig struct {
	Type   string `yaml:"type"`
	Name   string `yaml:"name"`
	Config WorkflowConfig
}

type WorkflowConfig interface {
	workflowConfig()
	Validate(userCfg Config) error
}

type QueriesWorkflowConfig struct {
	Random           bool                     `yaml:"random"`
	ThinkTime        ThinkTimeConfig          `yaml:"think_time"`
	TimeAnchor       TimeAnchor               `yaml:"time_anchor"`
	DefaultTimeRange *TimeRangeConfig         `yaml:"default_time_range"`
	DefaultSettings  map[string]string        `yaml:"default_settings"`
	TimeRangeCadence TimeRangeCadence         `yaml:"time_range_cadence"`
	Vars             map[string]string        `yaml:"vars"`
	PreflightQueries []PreflightQueryConfig   `yaml:"preflight_queries"`
	PreflightCadence WorkflowPreflightCadence `yaml:"preflight_cadence"`
	Queries          []QueryConfig            `yaml:"queries"`
}

func (QueriesWorkflowConfig) workflowConfig() {}

func (c QueriesWorkflowConfig) Validate(userCfg Config) error {
	if c.TimeAnchor == TimeAnchorDatasetEnd && userCfg.DatasetUnixEnd == 0 {
		return fmt.Errorf("dataset_unix_end must be set when time_anchor is set to %q", c.TimeAnchor)
	} else if c.TimeAnchor == TimeAnchorDatasetRandom && (userCfg.DatasetUnixStart == 0 || userCfg.DatasetUnixEnd == 0) {
		return fmt.Errorf("dataset_unix_start and dataset_unix_end must be set when time_anchor is set to %q", c.TimeAnchor)
	}

	if c.DefaultTimeRange != nil {
		if err := c.DefaultTimeRange.Validate(); err != nil {
			return fmt.Errorf("default_time_range: %w", err)
		}
	}

	if c.TimeRangeCadence == "" {
		c.TimeRangeCadence = TimeRangeCadencePerQuery
	}
	if c.TimeRangeCadence != TimeRangeCadencePerQuery && c.TimeRangeCadence != TimeRangeCadencePerLoop {
		return fmt.Errorf("time_range_cadence must be one of: %s, %s", TimeRangeCadencePerQuery, TimeRangeCadencePerLoop)
	}
	if c.TimeRangeCadence == TimeRangeCadencePerLoop && c.Random {
		return fmt.Errorf("time_range_cadence cannot be %q when random is %t", c.TimeRangeCadence, c.Random)
	}

	if c.PreflightCadence == "" {
		c.PreflightCadence = WorkflowPreflightCadenceOnce
	}
	if c.PreflightCadence != WorkflowPreflightCadenceOnce &&
		c.PreflightCadence != WorkflowPreflightCadencePerLoop &&
		c.PreflightCadence != WorkflowPreflightCadencePerQuery {
		return fmt.Errorf("preflight_cadence must be one of: %s, %s, %s",
			WorkflowPreflightCadenceOnce, WorkflowPreflightCadencePerLoop, WorkflowPreflightCadencePerQuery)
	}
	if c.PreflightCadence == WorkflowPreflightCadencePerLoop && c.Random {
		return fmt.Errorf("preflight_cadence cannot be %q when random is %t", c.PreflightCadence, c.Random)
	}

	if err := validateVars(c.Vars); err != nil {
		return fmt.Errorf("vars: %w", err)
	}

	for i, pf := range c.PreflightQueries {
		if err := pf.Validate(); err != nil {
			return fmt.Errorf("preflight_queries[%d]: %w", i, err)
		}
	}

	if len(c.Queries) == 0 {
		return errors.New("must have at least one query in the queries array")
	}

	for i, q := range c.Queries {
		if err := q.Validate(); err != nil {
			return fmt.Errorf("queries[%d] (name: %q): %w", i, q.Name, err)
		}
	}

	return nil
}

type HARWorkflowConfig struct {
	File      string          `yaml:"file"`
	ThinkTime ThinkTimeConfig `yaml:"think_time"`
}

func (HARWorkflowConfig) workflowConfig() {}

func (c HARWorkflowConfig) Validate(_ Config) error {
	if c.File == "" {
		return errors.New("har workflow requires a file path")
	}

	return nil
}

type ThinkTimeConfig struct {
	Min time.Duration `yaml:"min"`
	Max time.Duration `yaml:"max"`
}

func (c ThinkTimeConfig) Validate() error {
	if c.Min < 0 || c.Max < 0 {
		return errors.New("think_time min and max must be non-negative")
	}

	if c.Max < c.Min {
		return errors.New("think_time max must be >= min")
	}

	return nil
}

type QueryConfig struct {
	Name             string                 `yaml:"name"`
	SQL              string                 `yaml:"sql"`
	Perf             *PerfConfig            `yaml:"perf"`
	TimeRange        *TimeRangeConfig       `yaml:"time_range"`
	Vars             map[string]string      `yaml:"vars"`
	PreflightQueries []PreflightQueryConfig `yaml:"preflight_queries"`
	Settings         map[string]string      `yaml:"settings"`
}

func (c QueryConfig) Validate() error {
	if strings.TrimSpace(c.SQL) == "" {
		return errors.New("sql must not be empty")
	}

	if err := validateFormatNull(c.SQL); err != nil {
		return err
	}

	if err := validateVars(c.Vars); err != nil {
		return fmt.Errorf("vars: %w", err)
	}

	for i, pf := range c.PreflightQueries {
		if err := pf.Validate(); err != nil {
			return fmt.Errorf("preflight_queries[%d]: %w", i, err)
		}
	}

	if c.TimeRange != nil {
		if err := c.TimeRange.Validate(); err != nil {
			return fmt.Errorf("time_range: %w", err)
		}
	}

	return nil
}

func validateVars(vars map[string]string) error {
	for k := range vars {
		if strings.TrimSpace(k) == "" {
			return errors.New("var key must not be empty")
		}
	}
	return nil
}

func validateFormatNull(sql string) error {
	upper := strings.ToUpper(sql)
	if strings.Contains(upper, "SETTINGS") && !strings.Contains(upper, "FORMAT NULL") {
		return errors.New("the query contains a SETTINGS clause, you must add FORMAT Null")
	}

	return nil
}

type PreflightQueryConfig struct {
	SQL      string `yaml:"sql"`
	Settings map[string]string
	Binds    []string `yaml:"binds"`
}

func (c PreflightQueryConfig) Validate() error {
	if strings.TrimSpace(c.SQL) == "" {
		return errors.New("sql must not be empty")
	}

	if len(c.Binds) == 0 {
		return errors.New("binds array cannot be empty")
	}

	for i, bindName := range c.Binds {
		if strings.TrimSpace(bindName) == "" {
			return fmt.Errorf("binds[%d]: bind name cannot be empty", i)
		}
	}

	return nil
}

type PerfConfig struct {
	P50 time.Duration `yaml:"p50"`
	P90 time.Duration `yaml:"p90"`
	P95 time.Duration `yaml:"p95"`
	P99 time.Duration `yaml:"p99"`
}

type TimeAnchor string

const (
	TimeAnchorNow           TimeAnchor = "now"
	TimeAnchorDatasetEnd    TimeAnchor = "dataset_end"
	TimeAnchorDatasetRandom TimeAnchor = "dataset_random"
)

type TimeRangeType string

const (
	TimeRangeNone        TimeRangeType = "none"
	TimeRangeFixed       TimeRangeType = "fixed"
	TimeRangeUniform     TimeRangeType = "uniform"
	TimeRangeExponential TimeRangeType = "exponential"
	TimeRangeLogNormal   TimeRangeType = "log_normal"
)

type TimeRangeConfig struct {
	Type  TimeRangeType `yaml:"type"`
	Round time.Duration `yaml:"round"`

	// Fixed
	Lookback time.Duration `yaml:"lookback"`

	// Uniform
	Min time.Duration `yaml:"min"`
	Max time.Duration `yaml:"max"`

	// Exponential / LogNormal
	Mean time.Duration `yaml:"mean"`

	// LogNormal only. Controls spread and tail weight.
	// Higher values produce a heavier right tail and more variance.
	// Defaults to 0.5 when unset. Typical range: 0.3–1.5.
	Sigma float64 `yaml:"sigma"`
}

func (c TimeRangeConfig) Validate() error {
	if c.Round < 0 {
		return errors.New("time_range round duration must be >= 0")
	}

	switch c.Type {
	case TimeRangeNone:
		return nil
	case TimeRangeFixed:
		if c.Lookback <= 0 {
			return errors.New("fixed time_range requires a positive lookback")
		}
	case TimeRangeUniform:
		if c.Min <= 0 || c.Max <= 0 {
			return errors.New("uniform time_range requires positive min and max")
		}

		if c.Max < c.Min {
			return errors.New("uniform time_range max must be >= min")
		}
	case TimeRangeExponential:
		if c.Mean <= 0 {
			return fmt.Errorf("%s time_range requires a positive mean", c.Type)
		}

		if c.Min > 0 && c.Max > 0 && c.Max < c.Min {
			return fmt.Errorf("%s time_range max must be >= min", c.Type)
		}
	case TimeRangeLogNormal:
		if c.Mean <= 0 {
			return fmt.Errorf("%s time_range requires a positive mean", c.Type)
		}

		if c.Min > 0 && c.Max > 0 && c.Max < c.Min {
			return fmt.Errorf("%s time_range max must be >= min", c.Type)
		}

		if c.Sigma < 0 {
			return errors.New("log_normal time_range sigma must be >= 0 (0 uses default of 0.5)")
		}
	default:
		return fmt.Errorf("unknown time_range type %q", c.Type)
	}

	return nil
}

type TimeRangeCadence string

const (
	TimeRangeCadencePerQuery TimeRangeCadence = "per_query"
	TimeRangeCadencePerLoop  TimeRangeCadence = "per_loop"
)

type WorkflowPreflightCadence string

const (
	// WorkflowPreflightCadenceOnce runs workload-level preflight queries once at startup and caches the result.
	WorkflowPreflightCadenceOnce WorkflowPreflightCadence = "once"
	// WorkflowPreflightCadencePerLoop re-runs at the start of each pass through the query list (random=false only).
	WorkflowPreflightCadencePerLoop WorkflowPreflightCadence = "per_loop"
	// WorkflowPreflightCadencePerQuery re-runs before every query execution.
	WorkflowPreflightCadencePerQuery WorkflowPreflightCadence = "per_query"
)

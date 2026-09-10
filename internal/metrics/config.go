package metrics

import (
	"errors"
	"fmt"
	"time"
)

// OTLPConfig controls exporting the self-metrics over OTLP/gRPC in addition to
// (or instead of) the ClickHouse perf sink. Every interval the worker's current
// metric state is snapshotted and exported as one OTLP metrics request.
type OTLPConfig struct {
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

	// Interval is how often the metric state is exported. Defaults to 15s,
	// minimum 1s.
	Interval time.Duration `yaml:"interval"`
}

const defaultOTLPInterval = 15 * time.Second

func (c OTLPConfig) withDefaults() OTLPConfig {
	if c.Interval <= 0 {
		c.Interval = defaultOTLPInterval
	}
	return c
}

func (c OTLPConfig) Validate() error {
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

	if c.Interval != 0 && c.Interval < time.Second {
		return errors.New("interval must be >= 1s")
	}

	return nil
}

type Config struct {
	Enabled       bool              `yaml:"enabled"`
	ClickHouseDSN string            `yaml:"clickhouse_dsn"`
	Database      string            `yaml:"database"`
	RunTable      string            `yaml:"run_table"`
	MetricsTable  string            `yaml:"metrics_table"`
	CreateSchema  bool              `yaml:"create_schema"`
	Attributes    map[string]string `yaml:"attributes"`
	OTLP          OTLPConfig        `yaml:"otlp"`
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if err := c.OTLP.Validate(); err != nil {
		return fmt.Errorf("otlp: %w", err)
	}

	// The ClickHouse perf sink is optional when the OTLP exporter is enabled:
	// an empty clickhouse_dsn runs the metrics worker with OTLP export only.
	if c.ClickHouseDSN == "" {
		if !c.OTLP.Enabled {
			return errors.New("must set clickhouse_dsn (or enable the otlp exporter)")
		}
		return nil
	}

	if c.Database == "" {
		return errors.New("must set database")
	}

	if c.RunTable == "" {
		return errors.New("must set run_table")
	}

	if c.MetricsTable == "" {
		return errors.New("must set metrics_table")
	}

	return nil
}

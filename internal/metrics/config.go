package metrics

import (
	"errors"
	"fmt"
	"time"
)

// OTLPConfig controls exporting the self-metrics over OTLP in addition to
// (or instead of) the ClickHouse perf sink. Every interval the worker's current
// metric state is snapshotted and exported as one OTLP metrics request.
type OTLPConfig struct {
	Enabled bool `yaml:"enabled"`

	// Protocol selects the OTLP transport: "grpc" (default) or "http" (protobuf payload).
	Protocol string `yaml:"protocol"`

	// URL is the OTLP endpoint, e.g. "localhost:4317" (grpc) or "localhost:4318" (http; "/v1/metrics" is appended).
	URL string `yaml:"url"`

	// Insecure disables transport security; an explicit scheme in URL takes precedence.
	Insecure bool `yaml:"insecure"`

	// Compression is the compressor to use: "gzip" or "" / "none".
	Compression string `yaml:"compression"`

	// Headers are optional headers sent with every export (e.g. auth tokens).
	Headers map[string]string `yaml:"headers"`

	// Interval is how often the metric state is exported. Defaults to 15s,
	// minimum 1s.
	Interval time.Duration `yaml:"interval"`
}

const (
	otlpProtocolGRPC = "grpc"
	otlpProtocolHTTP = "http"

	defaultOTLPInterval = 15 * time.Second
)

func (c OTLPConfig) protocol() string {
	if c.Protocol == "" {
		return otlpProtocolGRPC
	}
	return c.Protocol
}

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

	switch c.Protocol {
	case "", otlpProtocolGRPC, otlpProtocolHTTP:
	default:
		return errors.New("protocol must be one of: grpc, http")
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

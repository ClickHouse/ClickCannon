package otel

import (
	"errors"
	"time"
)

// Config controls the OTLP exporter. When enabled, the exporter consumes
// blocks from the same queue the insert workers use and exports them as OTLP to
// an OpenTelemetry endpoint instead of inserting into ClickHouse. Only one sink
// (insert or otel) can consume the queue at a time.
type Config struct {
	Enabled bool `yaml:"enabled"`

	// Protocol selects the OTLP transport: "grpc" (default, port 4317) or "http" (protobuf payload, port 4318).
	Protocol string `yaml:"protocol"`

	// URL is the OTLP endpoint, e.g. "localhost:4317" (grpc) or "https://localhost:4318" (http).
	URL string `yaml:"url"`

	// Insecure disables TLS. For gRPC it overrides the URL scheme; for HTTP an explicit scheme wins.
	Insecure bool `yaml:"insecure"`

	// Threads is the number of concurrent exporter workers. Each worker holds one
	// connection and consumes blocks independently.
	Threads int `yaml:"threads"`

	// BatchSize is the number of rows accumulated (across one or more blocks)
	// before an export request is flushed. Larger batches mean fewer, larger RPCs.
	BatchSize int `yaml:"batch_size"`

	// FlushInterval bounds how long a partially-filled batch waits before being
	// flushed, so low-throughput sources still export promptly. Defaults to 1s.
	FlushInterval time.Duration `yaml:"flush_interval"`

	// Timeout is the per-export-request deadline. Defaults to 30s.
	Timeout time.Duration `yaml:"timeout"`

	// Compression is the compressor to use: "gzip" or "" / "none".
	Compression string `yaml:"compression"`

	// Headers are sent with every export (e.g. auth tokens).
	Headers map[string]string `yaml:"headers"`
}

const (
	protocolGRPC = "grpc"
	protocolHTTP = "http"

	defaultFlushInterval = time.Second
	defaultTimeout       = 30 * time.Second
)

func (c Config) protocol() string {
	if c.Protocol == "" {
		return protocolGRPC
	}
	return c.Protocol
}

// withDefaults returns a copy of the config with zero-valued tunables filled in.
func (c Config) withDefaults() Config {
	if c.FlushInterval <= 0 {
		c.FlushInterval = defaultFlushInterval
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	return c
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	switch c.Protocol {
	case "", protocolGRPC, protocolHTTP:
	default:
		return errors.New("protocol must be one of: grpc, http")
	}
	if c.URL == "" {
		return errors.New("must set url")
	}
	if c.Threads < 1 {
		return errors.New("must set threads to a value greater than zero")
	}
	if c.BatchSize < 1 {
		return errors.New("must set batch_size to a value greater than zero")
	}
	switch c.Compression {
	case "", "none", "gzip":
	default:
		return errors.New("compression must be one of: none, gzip")
	}

	return nil
}

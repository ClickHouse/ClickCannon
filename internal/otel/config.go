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

	// Protocol selects the OTLP transport: "grpc" (default) or "http". "http" is
	// OTLP/HTTP with a protobuf payload (typically port 4318), "grpc" is
	// OTLP/gRPC (typically port 4317).
	Protocol string `yaml:"protocol"`

	// URL is the OTLP endpoint. For gRPC this is a host:port, e.g.
	// "localhost:4317"; a leading "http://" / "https://" / "grpc://" scheme is
	// accepted and stripped. For HTTP this is the base endpoint, e.g.
	// "https://localhost:4318"; the per-signal path ("/v1/logs" or "/v1/traces")
	// is appended automatically unless the URL already ends in one.
	URL string `yaml:"url"`

	// Insecure disables transport security (plaintext gRPC, or http:// for the
	// HTTP transport). When false, TLS is used. A "http://" scheme in URL also
	// implies insecure; a scheme in URL takes precedence over this flag.
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

	// Compression is the compressor to use: "gzip" or "" / "none". For gRPC this
	// selects the gRPC compressor; for HTTP it gzips the request body.
	Compression string `yaml:"compression"`

	// Headers are optional headers sent with every export (gRPC metadata or HTTP
	// request headers, e.g. auth tokens).
	Headers map[string]string `yaml:"headers"`
}

const (
	protocolGRPC = "grpc"
	protocolHTTP = "http"

	defaultFlushInterval = time.Second
	defaultTimeout       = 30 * time.Second
)

// protocol returns the configured transport, defaulting to gRPC when unset.
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

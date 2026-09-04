package otel

import (
	"errors"
	"strings"
	"time"
)

// Protocol values accepted by Config.Protocol.
const (
	// ProtocolGRPC exports over OTLP/gRPC (default).
	ProtocolGRPC = "grpc"
	// ProtocolHTTP exports over OTLP/HTTP with a protobuf-encoded body
	// ("http/protobuf" in the OpenTelemetry specification).
	ProtocolHTTP = "http"
)

// Config controls the OTLP exporter. When enabled, the exporter consumes blocks
// from the same queue the insert workers use and exports them as OTLP to an
// OpenTelemetry endpoint instead of inserting into ClickHouse. Only one sink
// (insert or otel) can consume the queue at a time.
type Config struct {
	Enabled bool `yaml:"enabled"`

	// Protocol selects the OTLP transport: "grpc" (default) or "http". The http
	// protocol sends protobuf-encoded bodies (OTLP "http/protobuf").
	Protocol string `yaml:"protocol"`

	// URL is the OTLP endpoint.
	//
	// For protocol "grpc" this is a host:port target, e.g. "localhost:4317". A
	// leading "http://" / "https://" / "grpc://" scheme is accepted and stripped.
	//
	// For protocol "http" this is a base URL, e.g. "http://localhost:4318". When
	// no path is set, the per-signal path ("/v1/logs" or "/v1/traces") is
	// appended automatically; a URL that already carries a path is used verbatim.
	// A missing scheme defaults to https:// (or http:// when Insecure is set).
	URL string `yaml:"url"`

	// Insecure disables transport security: plaintext gRPC, or a http:// default
	// scheme for the http protocol. When false, TLS is used. A "http://" scheme
	// in URL also implies insecure.
	Insecure bool `yaml:"insecure"`

	// Threads is the number of concurrent exporter workers. Each worker holds one
	// connection (gRPC) or HTTP client and consumes blocks independently.
	Threads int `yaml:"threads"`

	// BatchSize is the number of rows accumulated (across one or more blocks)
	// before an export request is flushed. Larger batches mean fewer, larger RPCs.
	BatchSize int `yaml:"batch_size"`

	// FlushInterval bounds how long a partially-filled batch waits before being
	// flushed, so low-throughput sources still export promptly. Defaults to 1s.
	FlushInterval time.Duration `yaml:"flush_interval"`

	// Timeout is the per-export-request deadline. Defaults to 30s.
	Timeout time.Duration `yaml:"timeout"`

	// Compression is the payload compression to use: "gzip" or "" / "none". For
	// the grpc protocol this selects the gRPC compressor; for http it gzips the
	// request body and sets Content-Encoding: gzip.
	Compression string `yaml:"compression"`

	// Headers are optional headers sent with every export (e.g. auth tokens),
	// carried as gRPC metadata or HTTP request headers depending on Protocol.
	Headers map[string]string `yaml:"headers"`
}

const (
	defaultFlushInterval = time.Second
	defaultTimeout       = 30 * time.Second
)

// withDefaults returns a copy of the config with zero-valued tunables filled in.
func (c Config) withDefaults() Config {
	if c.Protocol == "" {
		c.Protocol = ProtocolGRPC
	}
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

	if c.URL == "" {
		return errors.New("must set url")
	}
	switch c.Protocol {
	case "", ProtocolGRPC:
	case ProtocolHTTP:
		if strings.HasPrefix(c.URL, "grpc://") {
			return errors.New(`url must not use the grpc:// scheme when protocol is "http"`)
		}
	default:
		return errors.New("protocol must be one of: grpc, http")
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

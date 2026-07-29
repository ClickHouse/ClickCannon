package otel

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
)

// client is a thin OTLP/gRPC export client. It holds one gRPC connection and the
// logs, traces, and metrics service clients (only one is used per exporter run).
type client struct {
	conn    *grpc.ClientConn
	logs    collogspb.LogsServiceClient
	traces  coltracepb.TraceServiceClient
	metrics colmetricspb.MetricsServiceClient
	md      metadata.MD
	timeout time.Duration
}

// dial creates a lazy gRPC client. grpc.NewClient does not open a connection
// until the first RPC, so this only fails on invalid configuration.
func dial(cfg *Config) (*client, error) {
	target, plaintext := parseTarget(cfg.URL)
	if cfg.Insecure {
		plaintext = true
	}

	opts := []grpc.DialOption{}
	if plaintext {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})))
	}
	if cfg.Compression == "gzip" {
		opts = append(opts, grpc.WithDefaultCallOptions(grpc.UseCompressor(gzip.Name)))
	}

	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create grpc client for %q: %w", target, err)
	}

	c := &client{
		conn:    conn,
		logs:    collogspb.NewLogsServiceClient(conn),
		traces:  coltracepb.NewTraceServiceClient(conn),
		metrics: colmetricspb.NewMetricsServiceClient(conn),
		timeout: cfg.Timeout,
	}
	if len(cfg.Headers) > 0 {
		c.md = metadata.New(cfg.Headers)
	}
	return c, nil
}

func (c *client) exportLogs(ctx context.Context, req *collogspb.ExportLogsServiceRequest) error {
	ctx, cancel := c.callContext(ctx)
	defer cancel()
	_, err := c.logs.Export(ctx, req)
	return err
}

func (c *client) exportTraces(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) error {
	ctx, cancel := c.callContext(ctx)
	defer cancel()
	_, err := c.traces.Export(ctx, req)
	return err
}

func (c *client) exportMetrics(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest) error {
	ctx, cancel := c.callContext(ctx)
	defer cancel()
	_, err := c.metrics.Export(ctx, req)
	return err
}

func (c *client) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.md != nil {
		ctx = metadata.NewOutgoingContext(ctx, c.md)
	}
	if c.timeout > 0 {
		return context.WithTimeout(ctx, c.timeout)
	}
	return context.WithCancel(ctx)
}

func (c *client) close() error {
	return c.conn.Close()
}

// parseTarget strips an optional scheme from the configured URL and reports
// whether the scheme implies plaintext (http://).
func parseTarget(url string) (target string, plaintext bool) {
	switch {
	case strings.HasPrefix(url, "http://"):
		return strings.TrimPrefix(url, "http://"), true
	case strings.HasPrefix(url, "https://"):
		return strings.TrimPrefix(url, "https://"), false
	case strings.HasPrefix(url, "grpc://"):
		return strings.TrimPrefix(url, "grpc://"), false
	default:
		return url, false
	}
}

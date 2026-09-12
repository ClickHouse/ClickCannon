package metrics

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
)

// otlpClient abstracts the OTLP export transport (gRPC or HTTP); both send the same request message.
type otlpClient interface {
	export(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest) error
	close() error
}

func dialOTLP(cfg OTLPConfig, timeout time.Duration) (otlpClient, error) {
	switch cfg.protocol() {
	case otlpProtocolHTTP:
		return dialOTLPHTTP(cfg, timeout)
	default:
		return dialOTLPGRPC(cfg, timeout)
	}
}

// otlpGRPCClient is a thin OTLP/gRPC metrics export client for the self-metrics
// pipeline, mirroring the client in internal/metricgen. It is kept local so
// internal/metrics stays import-free of the generator packages (which import
// this package).
type otlpGRPCClient struct {
	conn    *grpc.ClientConn
	metrics colmetricspb.MetricsServiceClient
	md      metadata.MD
	timeout time.Duration
}

// dialOTLPGRPC creates a lazy gRPC client. grpc.NewClient does not open a
// connection until the first RPC, so this only fails on invalid configuration.
func dialOTLPGRPC(cfg OTLPConfig, timeout time.Duration) (*otlpGRPCClient, error) {
	target, plaintext := parseOTLPTarget(cfg.URL)
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

	c := &otlpGRPCClient{
		conn:    conn,
		metrics: colmetricspb.NewMetricsServiceClient(conn),
		timeout: timeout,
	}
	if len(cfg.Headers) > 0 {
		c.md = metadata.New(cfg.Headers)
	}
	return c, nil
}

func (c *otlpGRPCClient) export(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest) error {
	if c.md != nil {
		ctx = metadata.NewOutgoingContext(ctx, c.md)
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	resp, err := c.metrics.Export(ctx, req)
	if err != nil {
		return err
	}
	if ps := resp.GetPartialSuccess(); ps != nil && ps.GetRejectedDataPoints() > 0 {
		return fmt.Errorf("endpoint rejected %d data points: %s", ps.GetRejectedDataPoints(), ps.GetErrorMessage())
	}
	return nil
}

func (c *otlpGRPCClient) close() error {
	return c.conn.Close()
}

// parseOTLPTarget strips an optional scheme from the configured URL and
// reports whether the scheme implies plaintext (http://).
func parseOTLPTarget(url string) (target string, plaintext bool) {
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

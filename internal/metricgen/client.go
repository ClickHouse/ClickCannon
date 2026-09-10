package metricgen

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/encoding/gzip"
	grpcproto "google.golang.org/grpc/encoding/proto"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// exportFullMethod is the OTLP metrics export RPC path (the generated proto
// package does not export a FullMethodName constant at this version).
const exportFullMethod = "/opentelemetry.proto.collector.metrics.v1.MetricsService/Export"

// preMarshaled is an already-encoded ExportMetricsServiceRequest payload.
// Workers marshal each request exactly once and reuse the byte length for
// their wire-size metrics; rawRequestCodec ships it as-is.
type preMarshaled []byte

// rawRequestCodec passes preMarshaled request bytes through unchanged and
// defers to the standard proto codec for everything else (responses). Without
// it the gRPC proto codec re-walks the whole request tree twice more per
// export (a size pass and a marshal pass), measured at ~23% of generator CPU
// at stress rates.
type rawRequestCodec struct {
	base encoding.CodecV2
}

func (c rawRequestCodec) Marshal(v any) (mem.BufferSlice, error) {
	if data, ok := v.(preMarshaled); ok {
		return mem.BufferSlice{mem.SliceBuffer(data)}, nil
	}
	return c.base.Marshal(v)
}

func (c rawRequestCodec) Unmarshal(data mem.BufferSlice, v any) error {
	return c.base.Unmarshal(data, v)
}

func (c rawRequestCodec) Name() string { return c.base.Name() }

// client is a thin OTLP/gRPC metrics export client, mirroring the logs/traces
// client in internal/otel.
type client struct {
	conn    *grpc.ClientConn
	codec   rawRequestCodec
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
		codec:   rawRequestCodec{base: encoding.GetCodecV2(grpcproto.Name)},
		timeout: cfg.Timeout,
	}
	if len(cfg.Headers) > 0 {
		c.md = metadata.New(cfg.Headers)
	}
	return c, nil
}

// export sends one pre-marshaled ExportMetricsServiceRequest.
func (c *client) export(ctx context.Context, data preMarshaled) error {
	if c.md != nil {
		ctx = metadata.NewOutgoingContext(ctx, c.md)
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	resp := &colmetricspb.ExportMetricsServiceResponse{}
	if err := c.conn.Invoke(ctx, exportFullMethod, data, resp, grpc.ForceCodecV2(c.codec)); err != nil {
		return err
	}
	if ps := resp.GetPartialSuccess(); ps != nil && ps.GetRejectedDataPoints() > 0 {
		return fmt.Errorf("endpoint rejected %d data points: %s", ps.GetRejectedDataPoints(), ps.GetErrorMessage())
	}
	return nil
}

func (c *client) close() error {
	return c.conn.Close()
}

// isMessageTooLarge reports whether an export failed because the marshaled
// request exceeds the receiver's gRPC message size limit: a permanent
// failure for that request that must not be retried.
func isMessageTooLarge(err error) bool {
	if err == nil {
		return false
	}
	s, ok := status.FromError(err)
	if !ok {
		return false
	}
	return s.Code() == codes.ResourceExhausted && strings.Contains(s.Message(), "larger than max")
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

package otel

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

// Per-signal request paths defined by the OTLP/HTTP specification.
const (
	httpPathLogs   = "/v1/logs"
	httpPathTraces = "/v1/traces"

	// protobufContentType is the OTLP/HTTP content type for protobuf bodies.
	protobufContentType = "application/x-protobuf"

	// maxErrorBodyBytes bounds how much of a non-2xx response body is read into
	// the returned error, so a misconfigured endpoint returning a large HTML
	// page cannot blow up log lines.
	maxErrorBodyBytes = 512
)

// retryAfterError wraps an export failure that carried a Retry-After header, so
// the worker's retry loop can respect the server's requested delay instead of
// its own backoff schedule.
type retryAfterError struct {
	after time.Duration
	err   error
}

func (e *retryAfterError) Error() string { return e.err.Error() }
func (e *retryAfterError) Unwrap() error { return e.err }

// httpClient is a thin OTLP/HTTP export client sending protobuf-encoded bodies
// ("http/protobuf"). One instance is used per worker, so the marshal and gzip
// buffers below are reused without synchronization.
type httpClient struct {
	hc       *http.Client
	endpoint string
	headers  map[string]string
	gzip     bool
	timeout  time.Duration

	// Scratch space for compression, reused across exports so gzip does not
	// repeatedly grow a fresh buffer. A worker exports serially, so no locking is
	// needed. See encode for why the compressed bytes are copied out.
	scratch bytes.Buffer
	gz      *gzip.Writer
}

func newHTTPClient(cfg *Config, dataType string) (*httpClient, error) {
	endpoint, err := resolveHTTPEndpoint(cfg.URL, cfg.Insecure, dataType)
	if err != nil {
		return nil, err
	}

	// A dedicated transport per worker keeps one warm connection per exporter
	// goroutine rather than sharing http.DefaultTransport's pool.
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        2,
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	c := &httpClient{
		hc:       &http.Client{Transport: tr},
		endpoint: endpoint,
		headers:  cfg.Headers,
		gzip:     cfg.Compression == "gzip",
		timeout:  cfg.Timeout,
	}
	if c.gzip {
		c.gz = gzip.NewWriter(&c.scratch)
	}
	return c, nil
}

func (c *httpClient) exportLogs(ctx context.Context, req *collogspb.ExportLogsServiceRequest) error {
	return c.post(ctx, req)
}

func (c *httpClient) exportTraces(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) error {
	return c.post(ctx, req)
}

// post marshals req, optionally gzips it, and POSTs it to the configured
// endpoint. A non-2xx response is returned as an error; 429/503 responses
// carrying a Retry-After header are wrapped in *retryAfterError.
func (c *httpClient) post(ctx context.Context, msg proto.Message) error {
	payload, err := c.encode(msg)
	if err != nil {
		return err
	}

	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to build otlp http request: %w", err)
	}
	// User headers first, then the protocol-required ones, so a stray
	// content-type in config cannot break the request encoding.
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Content-Type", protobufContentType)
	if c.gzip {
		httpReq.Header.Set("Content-Encoding", "gzip")
	}
	// Set explicitly so the server sees a length rather than a chunked body.
	httpReq.ContentLength = int64(len(payload))

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return fmt.Errorf("otlp http export to %s failed: %w", c.endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Drain so the connection can be reused. Partial-success payloads in the
		// body are intentionally ignored: rejected-record counts are advisory and
		// this exporter has no way to act on them.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil
	}

	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	exportErr := fmt.Errorf("otlp http export to %s returned %s: %s",
		c.endpoint, resp.Status, strings.TrimSpace(string(snippet)))

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		if after, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
			return &retryAfterError{after: after, err: exportErr}
		}
	}
	return exportErr
}

// encode marshals msg, gzipping when enabled. The returned slice is owned by the
// caller and must not alias state that a later export could mutate: http.Client
// .Do may return while the transport is still writing the request body (a server
// can answer before consuming it), so a buffer reused by the next export could be
// rewritten mid-flight. proto.Marshal already returns a fresh slice; the gzip
// path compresses into reused scratch space and then copies the result out.
func (c *httpClient) encode(msg proto.Message) ([]byte, error) {
	raw, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal otlp request: %w", err)
	}
	if !c.gzip {
		return raw, nil
	}

	c.scratch.Reset()
	c.gz.Reset(&c.scratch)
	if _, err := c.gz.Write(raw); err != nil {
		return nil, fmt.Errorf("failed to gzip otlp request: %w", err)
	}
	if err := c.gz.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize gzip otlp request: %w", err)
	}

	out := make([]byte, c.scratch.Len())
	copy(out, c.scratch.Bytes())
	return out, nil
}

func (c *httpClient) close() error {
	c.hc.CloseIdleConnections()
	return nil
}

// resolveHTTPEndpoint turns the configured URL into a full per-signal endpoint.
// A URL without a scheme gets https:// (or http:// when insecure). A URL with no
// path (or just "/") gets the signal path appended, matching OTel SDK behavior
// for OTEL_EXPORTER_OTLP_ENDPOINT; a URL that already carries a path is used
// verbatim, which is how a per-signal endpoint override is expressed.
func resolveHTTPEndpoint(rawURL string, insecure bool, dataType string) (string, error) {
	if rawURL == "" {
		return "", errors.New("must set url")
	}
	if strings.HasPrefix(rawURL, "grpc://") {
		return "", fmt.Errorf(`url %q uses the grpc:// scheme, which is invalid for protocol "http"`, rawURL)
	}

	if !strings.Contains(rawURL, "://") {
		scheme := "https://"
		if insecure {
			scheme = "http://"
		}
		rawURL = scheme + rawURL
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid otel url %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid otel url scheme %q: want http or https", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid otel url %q: missing host", rawURL)
	}

	if u.Path == "" || u.Path == "/" {
		signalPath, err := httpSignalPath(dataType)
		if err != nil {
			return "", err
		}
		u.Path = signalPath
	}
	return u.String(), nil
}

func httpSignalPath(dataType string) (string, error) {
	switch dataType {
	case "logs":
		return httpPathLogs, nil
	case "traces":
		return httpPathTraces, nil
	default:
		return "", fmt.Errorf("otel http export does not support data_type %q", dataType)
	}
}

// parseRetryAfter interprets a Retry-After header, which is either a delay in
// seconds or an HTTP-date. Non-positive or unparseable values report false.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d, true
		}
	}
	return 0, false
}

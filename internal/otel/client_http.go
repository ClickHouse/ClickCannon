package otel

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

// httpClient is an OTLP/HTTP (protobuf) export client.
type httpClient struct {
	http      *http.Client
	logsURL   string
	tracesURL string
	headers   map[string]string
	gzip      bool
	timeout   time.Duration
}

func dialHTTP(cfg *Config) (*httpClient, error) {
	base, err := httpBaseURL(cfg.URL, cfg.Insecure)
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if base.Scheme == "https" {
		transport.TLSClientConfig = &tls.Config{}
	}

	return &httpClient{
		http:      &http.Client{Transport: transport},
		logsURL:   signalURL(base, "/v1/logs"),
		tracesURL: signalURL(base, "/v1/traces"),
		headers:   cfg.Headers,
		gzip:      cfg.Compression == "gzip",
		timeout:   cfg.Timeout,
	}, nil
}

func (c *httpClient) exportLogs(ctx context.Context, req *collogspb.ExportLogsServiceRequest) error {
	return c.post(ctx, c.logsURL, req)
}

func (c *httpClient) exportTraces(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) error {
	return c.post(ctx, c.tracesURL, req)
}

// post returns non-2xx responses as errors so the worker's retry/backoff loop treats them like gRPC failures.
func (c *httpClient) post(ctx context.Context, endpoint string, msg proto.Message) error {
	body, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal otlp request: %w", err)
	}

	encoding := ""
	if c.gzip {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(body); err != nil {
			return fmt.Errorf("gzip otlp request: %w", err)
		}
		if err := zw.Close(); err != nil {
			return fmt.Errorf("gzip otlp request: %w", err)
		}
		body = buf.Bytes()
		encoding = "gzip"
	}

	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	// Protocol-required headers are set last so user config cannot clobber them.
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("otlp/http export to %s failed: %s: %s", endpoint, resp.Status, strings.TrimSpace(string(snippet)))
	}
	return nil
}

func (c *httpClient) close() error {
	c.http.CloseIdleConnections()
	return nil
}

// httpBaseURL infers the scheme for a bare host:port from the insecure flag; an explicit scheme wins.
func httpBaseURL(raw string, insecure bool) (*url.URL, error) {
	if !strings.Contains(raw, "://") {
		scheme := "https"
		if insecure {
			scheme = "http"
		}
		raw = scheme + "://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid otel url %q: %w", raw, err)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("invalid otel url scheme %q (want http or https)", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid otel url %q: missing host", raw)
	}
	return u, nil
}

// signalURL appends signalPath to base unless the path already ends with it.
func signalURL(base *url.URL, signalPath string) string {
	u := *base
	p := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(p, signalPath) {
		p += signalPath
	}
	u.Path = p
	return u.String()
}

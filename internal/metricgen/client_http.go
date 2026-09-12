package metricgen

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// errHTTPTooLarge marks a 413 response: a permanent, non-retryable failure for that request.
var errHTTPTooLarge = errors.New("request body too large")

// httpClient is an OTLP/HTTP metrics export client, mirroring the logs/traces httpClient in internal/otel.
type httpClient struct {
	http       *http.Client
	metricsURL string
	headers    map[string]string
	gzip       bool
	timeout    time.Duration
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
		http:       &http.Client{Transport: transport},
		metricsURL: signalURL(base, "/v1/metrics"),
		headers:    cfg.Headers,
		gzip:       cfg.Compression == "gzip",
		timeout:    cfg.Timeout,
	}, nil
}

// export POSTs one pre-marshaled request; an OTLP partial success in a 2xx response is a success and must not be retried.
func (c *httpClient) export(ctx context.Context, data preMarshaled) (rejected int64, rejectMsg string, err error) {
	body := []byte(data)
	encoding := ""
	if c.gzip {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(body); err != nil {
			return 0, "", fmt.Errorf("gzip otlp request: %w", err)
		}
		if err := zw.Close(); err != nil {
			return 0, "", fmt.Errorf("gzip otlp request: %w", err)
		}
		body = buf.Bytes()
		encoding = "gzip"
	}

	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.metricsURL, bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	// Protocol-required headers are set after custom ones so user config cannot clobber them.
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		return 0, "", fmt.Errorf("otlp/http export to %s failed: %s: %w", c.metricsURL, resp.Status, errHTTPTooLarge)
	}
	if resp.StatusCode/100 != 2 {
		snippet := respBody
		if len(snippet) > 512 {
			snippet = snippet[:512]
		}
		return 0, "", fmt.Errorf("otlp/http export to %s failed: %s: %s", c.metricsURL, resp.Status, strings.TrimSpace(string(snippet)))
	}
	if len(respBody) > 0 {
		r := &colmetricspb.ExportMetricsServiceResponse{}
		// A malformed 2xx body is still a success; partial success detection is best effort.
		if err := proto.Unmarshal(respBody, r); err == nil {
			if ps := r.GetPartialSuccess(); ps != nil && ps.GetRejectedDataPoints() > 0 {
				return ps.GetRejectedDataPoints(), ps.GetErrorMessage(), nil
			}
		}
	}
	return 0, "", nil
}

func (c *httpClient) close() error {
	c.http.CloseIdleConnections()
	return nil
}

// httpBaseURL normalizes to an absolute http(s) URL; a bare host:port infers its scheme from insecure, an explicit scheme wins.
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
		return nil, fmt.Errorf("invalid metric_gen url %q: %w", raw, err)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("invalid metric_gen url scheme %q (want http or https)", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid metric_gen url %q: missing host", raw)
	}
	return u, nil
}

// signalURL appends the per-signal path to the base URL unless it already ends with it.
func signalURL(base *url.URL, signalPath string) string {
	u := *base
	p := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(p, signalPath) {
		p += signalPath
	}
	u.Path = p
	return u.String()
}

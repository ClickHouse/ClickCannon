package metrics

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

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// otlpHTTPClient exports OTLP metrics as protobuf POSTs to "/v1/metrics".
type otlpHTTPClient struct {
	http    *http.Client
	url     string
	headers map[string]string
	gzip    bool
	timeout time.Duration
}

func dialOTLPHTTP(cfg OTLPConfig, timeout time.Duration) (*otlpHTTPClient, error) {
	base, err := otlpBaseURL(cfg.URL, cfg.Insecure)
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if base.Scheme == "https" {
		transport.TLSClientConfig = &tls.Config{}
	}

	return &otlpHTTPClient{
		http:    &http.Client{Transport: transport},
		url:     otlpMetricsURL(base),
		headers: cfg.Headers,
		gzip:    cfg.Compression == "gzip",
		timeout: timeout,
	}, nil
}

// export POSTs the request; a 2xx body is still checked for rejected data points.
func (c *otlpHTTPClient) export(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest) error {
	body, err := proto.Marshal(req)
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	// Protocol-required headers are set last so user config cannot clobber them.
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	if encoding != "" {
		httpReq.Header.Set("Content-Encoding", encoding)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// The limit only guards against a misbehaving endpoint; a real body is tiny.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode/100 != 2 {
		snippet := respBody
		if len(snippet) > 512 {
			snippet = snippet[:512]
		}
		return fmt.Errorf("otlp/http export to %s failed: %s: %s", c.url, resp.Status, strings.TrimSpace(string(snippet)))
	}

	var exportResp colmetricspb.ExportMetricsServiceResponse
	if proto.Unmarshal(respBody, &exportResp) == nil {
		if ps := exportResp.GetPartialSuccess(); ps != nil && ps.GetRejectedDataPoints() > 0 {
			return fmt.Errorf("endpoint rejected %d data points: %s", ps.GetRejectedDataPoints(), ps.GetErrorMessage())
		}
	}
	return nil
}

func (c *otlpHTTPClient) close() error {
	c.http.CloseIdleConnections()
	return nil
}

// otlpBaseURL normalizes raw to an http(s) URL; a bare host:port gets its scheme from the insecure flag, an explicit scheme wins.
func otlpBaseURL(raw string, insecure bool) (*url.URL, error) {
	if !strings.Contains(raw, "://") {
		scheme := "https"
		if insecure {
			scheme = "http"
		}
		raw = scheme + "://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid otlp url %q: %w", raw, err)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("invalid otlp url scheme %q (want http or https)", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid otlp url %q: missing host", raw)
	}
	return u, nil
}

func otlpMetricsURL(base *url.URL) string {
	u := *base
	p := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(p, "/v1/metrics") {
		p += "/v1/metrics"
	}
	u.Path = p
	return u.String()
}

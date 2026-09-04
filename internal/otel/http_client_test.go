package otel

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestResolveHTTPEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		insecure bool
		dataType string
		want     string
		wantErr  bool
	}{
		{
			name: "bare host defaults to https", url: "collector:4318", dataType: "logs",
			want: "https://collector:4318/v1/logs",
		},
		{
			name: "bare host insecure defaults to http", url: "localhost:4318", insecure: true, dataType: "logs",
			want: "http://localhost:4318/v1/logs",
		},
		{
			name: "http scheme preserved", url: "http://localhost:4318", dataType: "traces",
			want: "http://localhost:4318/v1/traces",
		},
		{
			name: "https scheme preserved", url: "https://otlp.example.com", dataType: "traces",
			want: "https://otlp.example.com/v1/traces",
		},
		{
			name: "trailing slash gets signal path", url: "http://localhost:4318/", dataType: "logs",
			want: "http://localhost:4318/v1/logs",
		},
		{
			name: "explicit path used verbatim", url: "https://vendor.example.com/otlp/v1/logs", dataType: "logs",
			want: "https://vendor.example.com/otlp/v1/logs",
		},
		{
			// A path override wins even when it does not match the signal, since
			// that is how per-signal vendor endpoints are configured.
			name: "explicit path not overridden by data type", url: "https://vendor.example.com/ingest", dataType: "traces",
			want: "https://vendor.example.com/ingest",
		},
		{
			name: "insecure flag does not downgrade explicit https", url: "https://localhost:4318", insecure: true, dataType: "logs",
			want: "https://localhost:4318/v1/logs",
		},
		{name: "grpc scheme rejected", url: "grpc://localhost:4317", dataType: "logs", wantErr: true},
		{name: "empty url rejected", url: "", dataType: "logs", wantErr: true},
		{name: "unsupported scheme rejected", url: "ftp://localhost:4318", dataType: "logs", wantErr: true},
		{name: "unsupported data type rejected", url: "http://localhost:4318", dataType: "profiles", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveHTTPEndpoint(tt.url, tt.insecure, tt.dataType)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveHTTPEndpoint(%q) = %q, want error", tt.url, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveHTTPEndpoint(%q): %v", tt.url, err)
			}
			if got != tt.want {
				t.Errorf("resolveHTTPEndpoint(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// capturedRequest holds what the fake collector saw, so assertions can run after
// the handler returns.
type capturedRequest struct {
	method          string
	path            string
	contentType     string
	contentEncoding string
	authHeader      string
	body            []byte
}

// newFakeCollector serves an OTLP/HTTP endpoint that records one request and
// replies with status. It transparently gunzips a gzip-encoded body so tests can
// unmarshal the protobuf directly.
func newFakeCollector(t *testing.T, status int, headers map[string]string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	got := &capturedRequest{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path
		got.contentType = r.Header.Get("Content-Type")
		got.contentEncoding = r.Header.Get("Content-Encoding")
		got.authHeader = r.Header.Get("Authorization")

		var reader io.Reader = r.Body
		if got.contentEncoding == "gzip" {
			gz, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Errorf("gzip.NewReader: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer gz.Close()
			reader = gz
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Errorf("read body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		got.body = body

		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func testLogsRequest(records int) *collogspb.ExportLogsServiceRequest {
	recs := make([]*logspb.LogRecord, 0, records)
	for i := 0; i < records; i++ {
		recs = append(recs, &logspb.LogRecord{
			TimeUnixNano: uint64(1700000000_000000000 + i),
			Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "hello"}},
		})
	}
	return &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{{
			ScopeLogs: []*logspb.ScopeLogs{{LogRecords: recs}},
		}},
	}
}

func testTracesRequest(spans int) *coltracepb.ExportTraceServiceRequest {
	ss := make([]*tracepb.Span, 0, spans)
	for i := 0; i < spans; i++ {
		ss = append(ss, &tracepb.Span{Name: "GET /api"})
	}
	return &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{Spans: ss}},
		}},
	}
}

func TestHTTPClientExportLogs(t *testing.T) {
	for _, compression := range []string{"gzip", "none"} {
		t.Run(compression, func(t *testing.T) {
			srv, got := newFakeCollector(t, http.StatusOK, nil)

			c, err := newHTTPClient(&Config{
				URL:         srv.URL,
				Insecure:    true,
				Compression: compression,
				Timeout:     5 * time.Second,
				Headers:     map[string]string{"Authorization": "Bearer tok"},
			}, "logs")
			if err != nil {
				t.Fatalf("newHTTPClient: %v", err)
			}
			defer c.close()

			const records = 3
			if err := c.exportLogs(context.Background(), testLogsRequest(records)); err != nil {
				t.Fatalf("exportLogs: %v", err)
			}

			if got.method != http.MethodPost {
				t.Errorf("method = %q, want POST", got.method)
			}
			if got.path != httpPathLogs {
				t.Errorf("path = %q, want %q", got.path, httpPathLogs)
			}
			if got.contentType != protobufContentType {
				t.Errorf("content-type = %q, want %q", got.contentType, protobufContentType)
			}
			wantEncoding := ""
			if compression == "gzip" {
				wantEncoding = "gzip"
			}
			if got.contentEncoding != wantEncoding {
				t.Errorf("content-encoding = %q, want %q", got.contentEncoding, wantEncoding)
			}
			if got.authHeader != "Bearer tok" {
				t.Errorf("authorization = %q, want %q", got.authHeader, "Bearer tok")
			}

			var decoded collogspb.ExportLogsServiceRequest
			if err := proto.Unmarshal(got.body, &decoded); err != nil {
				t.Fatalf("unmarshal received body: %v", err)
			}
			if n := len(decoded.ResourceLogs[0].ScopeLogs[0].LogRecords); n != records {
				t.Errorf("received %d log records, want %d", n, records)
			}
		})
	}
}

func TestHTTPClientExportTraces(t *testing.T) {
	srv, got := newFakeCollector(t, http.StatusOK, nil)

	c, err := newHTTPClient(&Config{
		URL: srv.URL, Insecure: true, Compression: "gzip", Timeout: 5 * time.Second,
	}, "traces")
	if err != nil {
		t.Fatalf("newHTTPClient: %v", err)
	}
	defer c.close()

	const spans = 4
	if err := c.exportTraces(context.Background(), testTracesRequest(spans)); err != nil {
		t.Fatalf("exportTraces: %v", err)
	}

	if got.path != httpPathTraces {
		t.Errorf("path = %q, want %q", got.path, httpPathTraces)
	}
	var decoded coltracepb.ExportTraceServiceRequest
	if err := proto.Unmarshal(got.body, &decoded); err != nil {
		t.Fatalf("unmarshal received body: %v", err)
	}
	if n := len(decoded.ResourceSpans[0].ScopeSpans[0].Spans); n != spans {
		t.Errorf("received %d spans, want %d", n, spans)
	}
}

// TestHTTPClientRepeatedExports exports twice on the same client to make sure the
// reused gzip scratch buffer does not leak bytes between requests.
func TestHTTPClientRepeatedExports(t *testing.T) {
	srv, got := newFakeCollector(t, http.StatusOK, nil)

	c, err := newHTTPClient(&Config{
		URL: srv.URL, Insecure: true, Compression: "gzip", Timeout: 5 * time.Second,
	}, "logs")
	if err != nil {
		t.Fatalf("newHTTPClient: %v", err)
	}
	defer c.close()

	if err := c.exportLogs(context.Background(), testLogsRequest(10)); err != nil {
		t.Fatalf("first exportLogs: %v", err)
	}
	if err := c.exportLogs(context.Background(), testLogsRequest(2)); err != nil {
		t.Fatalf("second exportLogs: %v", err)
	}

	var decoded collogspb.ExportLogsServiceRequest
	if err := proto.Unmarshal(got.body, &decoded); err != nil {
		t.Fatalf("unmarshal second body: %v", err)
	}
	if n := len(decoded.ResourceLogs[0].ScopeLogs[0].LogRecords); n != 2 {
		t.Errorf("second export carried %d records, want 2", n)
	}
}

func TestHTTPClientErrorResponses(t *testing.T) {
	t.Run("server error", func(t *testing.T) {
		srv, _ := newFakeCollector(t, http.StatusInternalServerError, nil)
		c, err := newHTTPClient(&Config{URL: srv.URL, Insecure: true, Timeout: 5 * time.Second}, "logs")
		if err != nil {
			t.Fatalf("newHTTPClient: %v", err)
		}
		defer c.close()

		err = c.exportLogs(context.Background(), testLogsRequest(1))
		if err == nil {
			t.Fatal("exportLogs succeeded, want error")
		}
		var ra *retryAfterError
		if errors.As(err, &ra) {
			t.Errorf("500 should not carry Retry-After, got %v", ra.after)
		}
	})

	t.Run("429 with retry-after seconds", func(t *testing.T) {
		srv, _ := newFakeCollector(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "2"})
		c, err := newHTTPClient(&Config{URL: srv.URL, Insecure: true, Timeout: 5 * time.Second}, "logs")
		if err != nil {
			t.Fatalf("newHTTPClient: %v", err)
		}
		defer c.close()

		err = c.exportLogs(context.Background(), testLogsRequest(1))
		var ra *retryAfterError
		if !errors.As(err, &ra) {
			t.Fatalf("err = %v, want *retryAfterError", err)
		}
		if ra.after != 2*time.Second {
			t.Errorf("retry after = %v, want 2s", ra.after)
		}
	})

	t.Run("503 without retry-after", func(t *testing.T) {
		srv, _ := newFakeCollector(t, http.StatusServiceUnavailable, nil)
		c, err := newHTTPClient(&Config{URL: srv.URL, Insecure: true, Timeout: 5 * time.Second}, "logs")
		if err != nil {
			t.Fatalf("newHTTPClient: %v", err)
		}
		defer c.close()

		err = c.exportLogs(context.Background(), testLogsRequest(1))
		if err == nil {
			t.Fatal("exportLogs succeeded, want error")
		}
		var ra *retryAfterError
		if errors.As(err, &ra) {
			t.Error("503 without Retry-After should not be a retryAfterError")
		}
	})
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value string
		want  time.Duration
		ok    bool
	}{
		{name: "seconds", value: "5", want: 5 * time.Second, ok: true},
		{name: "seconds with spaces", value: "  7 ", want: 7 * time.Second, ok: true},
		{name: "zero seconds", value: "0"},
		{name: "negative seconds", value: "-3"},
		{name: "empty", value: ""},
		{name: "garbage", value: "soon"},
		{
			name:  "http date in the future",
			value: now.Add(30 * time.Second).Format(http.TimeFormat),
			want:  30 * time.Second, ok: true,
		},
		{name: "http date in the past", value: now.Add(-30 * time.Second).Format(http.TimeFormat)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tt.value, now)
			if ok != tt.ok {
				t.Fatalf("parseRetryAfter(%q) ok = %v, want %v", tt.value, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// TestDialSelectsProtocol checks the factory returns the right transport and
// rejects unknown protocols.
func TestDialSelectsProtocol(t *testing.T) {
	httpExp, err := dial(&Config{Protocol: ProtocolHTTP, URL: "http://localhost:4318", Insecure: true}, "logs")
	if err != nil {
		t.Fatalf("dial http: %v", err)
	}
	defer httpExp.close()
	if _, ok := httpExp.(*httpClient); !ok {
		t.Errorf("protocol http produced %T, want *httpClient", httpExp)
	}

	for _, p := range []string{ProtocolGRPC, ""} {
		grpcExp, err := dial(&Config{Protocol: p, URL: "localhost:4317", Insecure: true}, "logs")
		if err != nil {
			t.Fatalf("dial %q: %v", p, err)
		}
		if _, ok := grpcExp.(*grpcClient); !ok {
			t.Errorf("protocol %q produced %T, want *grpcClient", p, grpcExp)
		}
		grpcExp.close()
	}

	if _, err := dial(&Config{Protocol: "carrier-pigeon", URL: "localhost:4317"}, "logs"); err == nil {
		t.Error("dial with unknown protocol succeeded, want error")
	}
}

// TestHTTPClientHeadersCannotClobberProtocolHeaders makes sure a stray
// content-type in user config cannot break the request encoding.
func TestHTTPClientHeadersCannotClobberProtocolHeaders(t *testing.T) {
	srv, got := newFakeCollector(t, http.StatusOK, nil)

	c, err := newHTTPClient(&Config{
		URL: srv.URL, Insecure: true, Compression: "gzip", Timeout: 5 * time.Second,
		Headers: map[string]string{"Content-Type": "text/plain", "Content-Encoding": "identity"},
	}, "logs")
	if err != nil {
		t.Fatalf("newHTTPClient: %v", err)
	}
	defer c.close()

	if err := c.exportLogs(context.Background(), testLogsRequest(1)); err != nil {
		t.Fatalf("exportLogs: %v", err)
	}
	if got.contentType != protobufContentType {
		t.Errorf("content-type = %q, want %q", got.contentType, protobufContentType)
	}
	if got.contentEncoding != "gzip" {
		t.Errorf("content-encoding = %q, want gzip", got.contentEncoding)
	}
}

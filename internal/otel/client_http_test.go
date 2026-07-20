package otel

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"
)

func TestSignalURL(t *testing.T) {
	cases := []struct {
		raw       string
		insecure  bool
		wantLogs  string
		wantTrace string
	}{
		{"https://localhost:4318", false, "https://localhost:4318/v1/logs", "https://localhost:4318/v1/traces"},
		{"localhost:4318", true, "http://localhost:4318/v1/logs", "http://localhost:4318/v1/traces"},
		{"localhost:4318", false, "https://localhost:4318/v1/logs", "https://localhost:4318/v1/traces"},
		{"http://localhost:4318/", false, "http://localhost:4318/v1/logs", "http://localhost:4318/v1/traces"},
		{"https://collector.example.com/otlp", false, "https://collector.example.com/otlp/v1/logs", "https://collector.example.com/otlp/v1/traces"},
		// An explicit scheme wins over the insecure flag.
		{"https://localhost:4318", true, "https://localhost:4318/v1/logs", "https://localhost:4318/v1/traces"},
	}
	for _, c := range cases {
		base, err := httpBaseURL(c.raw, c.insecure)
		if err != nil {
			t.Fatalf("httpBaseURL(%q): %v", c.raw, err)
		}
		if got := signalURL(base, "/v1/logs"); got != c.wantLogs {
			t.Errorf("signalURL(%q) logs = %q, want %q", c.raw, got, c.wantLogs)
		}
		if got := signalURL(base, "/v1/traces"); got != c.wantTrace {
			t.Errorf("signalURL(%q) traces = %q, want %q", c.raw, got, c.wantTrace)
		}
	}
}

func TestHTTPExportLogsRoundTrip(t *testing.T) {
	for _, useGzip := range []bool{false, true} {
		var gotPath, gotContentType, gotEncoding, gotAuth string
		var gotReq collogspb.ExportLogsServiceRequest

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotContentType = r.Header.Get("Content-Type")
			gotEncoding = r.Header.Get("Content-Encoding")
			gotAuth = r.Header.Get("Authorization")

			var body io.Reader = r.Body
			if gotEncoding == "gzip" {
				zr, err := gzip.NewReader(r.Body)
				if err != nil {
					t.Errorf("gzip.NewReader: %v", err)
					http.Error(w, "bad gzip", http.StatusBadRequest)
					return
				}
				body = zr
			}
			raw, _ := io.ReadAll(body)
			if err := proto.Unmarshal(raw, &gotReq); err != nil {
				t.Errorf("proto.Unmarshal: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		compression := ""
		if useGzip {
			compression = "gzip"
		}
		cfg := &Config{
			Protocol:    protocolHTTP,
			URL:         srv.URL,
			Compression: compression,
			Headers:     map[string]string{"Authorization": "Bearer tok"},
		}
		c, err := dialHTTP(cfg)
		if err != nil {
			t.Fatalf("dialHTTP: %v", err)
		}

		req := &collogspb.ExportLogsServiceRequest{
			ResourceLogs: []*logspb.ResourceLogs{{
				ScopeLogs: []*logspb.ScopeLogs{{
					LogRecords: []*logspb.LogRecord{{TimeUnixNano: 42}},
				}},
			}},
		}
		if err := c.exportLogs(context.Background(), req); err != nil {
			t.Fatalf("exportLogs (gzip=%v): %v", useGzip, err)
		}

		if gotPath != "/v1/logs" {
			t.Errorf("path = %q, want /v1/logs", gotPath)
		}
		if gotContentType != "application/x-protobuf" {
			t.Errorf("content-type = %q", gotContentType)
		}
		if useGzip && gotEncoding != "gzip" {
			t.Errorf("content-encoding = %q, want gzip", gotEncoding)
		}
		if !useGzip && gotEncoding != "" {
			t.Errorf("content-encoding = %q, want empty", gotEncoding)
		}
		if gotAuth != "Bearer tok" {
			t.Errorf("authorization = %q", gotAuth)
		}
		if len(gotReq.ResourceLogs) != 1 ||
			gotReq.ResourceLogs[0].ScopeLogs[0].LogRecords[0].TimeUnixNano != 42 {
			t.Errorf("decoded request mismatch: %+v", &gotReq)
		}
	}
}

func TestHTTPExportNon2xxErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c, err := dialHTTP(&Config{Protocol: protocolHTTP, URL: srv.URL})
	if err != nil {
		t.Fatalf("dialHTTP: %v", err)
	}
	err = c.exportLogs(context.Background(), &collogspb.ExportLogsServiceRequest{})
	if err == nil {
		t.Fatal("expected error on 503 response, got nil")
	}
}

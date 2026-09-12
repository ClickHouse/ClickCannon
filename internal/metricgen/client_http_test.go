package metricgen

import (
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"clickcannon/internal/metrics"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// httpCaptureServer records every export request; set status or rejectPerRequest before traffic starts.
type httpCaptureServer struct {
	status           int // 0 = 200 with an OTLP response body
	rejectPerRequest int64
	rejectMessage    string

	mu           sync.Mutex
	requests     []*colmetricspb.ExportMetricsServiceRequest
	bodies       map[string]int // times seen per raw decoded body
	paths        []string
	contentTypes []string
	encodings    []string
	auths        []string
}

func (s *httpCaptureServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths = append(s.paths, r.URL.Path)
	s.contentTypes = append(s.contentTypes, r.Header.Get("Content-Type"))
	s.auths = append(s.auths, r.Header.Get("Authorization"))
	enc := r.Header.Get("Content-Encoding")
	s.encodings = append(s.encodings, enc)

	var body io.Reader = r.Body
	if enc == "gzip" {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "bad gzip", http.StatusBadRequest)
			return
		}
		body = zr
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	req := &colmetricspb.ExportMetricsServiceRequest{}
	if err := proto.Unmarshal(raw, req); err != nil {
		http.Error(w, "bad proto", http.StatusBadRequest)
		return
	}
	s.requests = append(s.requests, req)
	if s.bodies == nil {
		s.bodies = make(map[string]int)
	}
	s.bodies[string(raw)]++

	if s.status != 0 {
		http.Error(w, http.StatusText(s.status), s.status)
		return
	}
	resp := &colmetricspb.ExportMetricsServiceResponse{}
	if s.rejectPerRequest > 0 {
		resp.PartialSuccess = &colmetricspb.ExportMetricsPartialSuccess{
			RejectedDataPoints: s.rejectPerRequest,
			ErrorMessage:       s.rejectMessage,
		}
	}
	out, _ := proto.Marshal(resp)
	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(out)
}

func startHTTPCaptureServer(t *testing.T) (*httpCaptureServer, string) {
	t.Helper()
	capture := &httpCaptureServer{}
	srv := httptest.NewServer(capture)
	t.Cleanup(srv.Close)
	return capture, srv.URL
}

func countRequestPoints(requests []*colmetricspb.ExportMetricsServiceRequest) uint64 {
	var n uint64
	for _, req := range requests {
		for _, rm := range req.ResourceMetrics {
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					if g := m.GetGauge(); g != nil {
						n += uint64(len(g.DataPoints))
					}
					if s := m.GetSum(); s != nil {
						n += uint64(len(s.DataPoints))
					}
					if h := m.GetHistogram(); h != nil {
						n += uint64(len(h.DataPoints))
					}
					if e := m.GetExponentialHistogram(); e != nil {
						n += uint64(len(e.DataPoints))
					}
					if s := m.GetSummary(); s != nil {
						n += uint64(len(s.DataPoints))
					}
				}
			}
		}
	}
	return n
}

func TestMetricsSignalURL(t *testing.T) {
	cases := []struct {
		raw      string
		insecure bool
		want     string
	}{
		{"https://localhost:4318", false, "https://localhost:4318/v1/metrics"},
		{"localhost:4318", true, "http://localhost:4318/v1/metrics"},
		{"localhost:4318", false, "https://localhost:4318/v1/metrics"},
		{"http://localhost:4318/", false, "http://localhost:4318/v1/metrics"},
		{"https://collector.example.com/otlp", false, "https://collector.example.com/otlp/v1/metrics"},
		{"http://localhost:4318/v1/metrics", false, "http://localhost:4318/v1/metrics"},
		{"https://localhost:4318", true, "https://localhost:4318/v1/metrics"},
	}
	for _, c := range cases {
		base, err := httpBaseURL(c.raw, c.insecure)
		if err != nil {
			t.Fatalf("httpBaseURL(%q): %v", c.raw, err)
		}
		if got := signalURL(base, "/v1/metrics"); got != c.want {
			t.Errorf("signalURL(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestConfigProtocolValidation(t *testing.T) {
	for _, p := range []string{"", "grpc", "http"} {
		cfg := exportTestConfig("localhost:4318")
		cfg.Protocol = p
		if err := cfg.Validate(); err != nil {
			t.Fatalf("protocol %q: %v", p, err)
		}
	}
	cfg := exportTestConfig("localhost:4318")
	cfg.Protocol = "ftp"
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid protocol accepted")
	}
}

func TestHTTPExportRoundTrip(t *testing.T) {
	for _, useGzip := range []bool{false, true} {
		capture, url := startHTTPCaptureServer(t)
		cfg := exportTestConfig(url)
		cfg.Protocol = protocolHTTP
		cfg.Headers = map[string]string{"Authorization": "Bearer tok"}
		if useGzip {
			cfg.Compression = "gzip"
		}

		s := NewScheduler(slog.New(slog.DiscardHandler), &cfg, "test-seed", metrics.NewDisabledStore())
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		if err := s.Run(ctx); err != nil {
			cancel()
			t.Fatalf("scheduler (gzip=%v): %v", useGzip, err)
		}
		cancel()

		capture.mu.Lock()
		if len(capture.requests) == 0 {
			t.Fatal("no requests captured")
		}
		for i := range capture.paths {
			if capture.paths[i] != "/v1/metrics" {
				t.Fatalf("path = %q, want /v1/metrics", capture.paths[i])
			}
			if capture.contentTypes[i] != "application/x-protobuf" {
				t.Fatalf("content-type = %q", capture.contentTypes[i])
			}
			if capture.auths[i] != "Bearer tok" {
				t.Fatalf("authorization = %q", capture.auths[i])
			}
			if useGzip && capture.encodings[i] != "gzip" {
				t.Fatalf("content-encoding = %q, want gzip", capture.encodings[i])
			}
			if !useGzip && capture.encodings[i] != "" {
				t.Fatalf("content-encoding = %q, want empty", capture.encodings[i])
			}
		}
		gotPoints := countRequestPoints(capture.requests)
		capture.mu.Unlock()

		total, _ := cfg.totalSeries()
		if want := total * uint64(cfg.Sweeps); gotPoints != want {
			t.Fatalf("received %d points (gzip=%v), want %d", gotPoints, useGzip, want)
		}
	}
}

func TestHTTPExportPartialSuccessNotRetried(t *testing.T) {
	capture, url := startHTTPCaptureServer(t)
	const rejectedPerRequest = 3
	capture.rejectPerRequest = rejectedPerRequest
	capture.rejectMessage = "attribute limit exceeded"

	cfg := exportTestConfig(url)
	cfg.Protocol = protocolHTTP
	store := newCountingStore()
	s := NewScheduler(slog.New(slog.DiscardHandler), &cfg, "test-seed", store)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := s.Run(ctx); err != nil {
		t.Fatalf("scheduler: %v", err)
	}

	capture.mu.Lock()
	requests := uint64(len(capture.requests))
	capture.mu.Unlock()
	if requests == 0 {
		t.Fatal("no requests captured")
	}

	if got := store.GetMetric(metrics.MetricGenExportsFailedTotal); got != 0 {
		t.Fatalf("partial success counted as %d failed exports, want 0", got)
	}
	if got := store.GetMetric(metrics.MetricGenRequestsTotal); got != requests {
		t.Fatalf("requests counter %d != %d requests received by server", got, requests)
	}
	total, _ := cfg.totalSeries()
	if got, want := store.GetMetric(metrics.MetricGenPointsTotal), total*uint64(cfg.Sweeps); got != want {
		t.Fatalf("points counter %d, want %d", got, want)
	}
	if got, want := store.GetMetric(metrics.MetricGenPointsRejectedTotal), requests*rejectedPerRequest; got != want {
		t.Fatalf("rejected counter %d, want %d", got, want)
	}
}

func TestHTTPExportTooLargeNotRetried(t *testing.T) {
	capture, url := startHTTPCaptureServer(t)
	capture.status = http.StatusRequestEntityTooLarge

	cfg := exportTestConfig(url)
	cfg.Protocol = protocolHTTP
	cfg.Sweeps = 1
	store := newCountingStore()
	s := NewScheduler(slog.New(slog.DiscardHandler), &cfg, "test-seed", store)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := s.Run(ctx); err != nil {
		t.Fatalf("scheduler: %v", err)
	}

	capture.mu.Lock()
	requests := uint64(len(capture.requests))
	for body, seen := range capture.bodies {
		if seen != 1 {
			t.Fatalf("payload of %d bytes delivered %d times: 413 must not be retried", len(body), seen)
		}
	}
	capture.mu.Unlock()
	if requests == 0 {
		t.Fatal("no requests captured")
	}

	if got := store.GetMetric(metrics.MetricGenRequestsTotal); got != 0 {
		t.Fatalf("413 counted as %d delivered requests, want 0", got)
	}
	if got := store.GetMetric(metrics.MetricGenExportsFailedTotal); got != requests {
		t.Fatalf("failed counter %d != %d requests received by server", got, requests)
	}
}

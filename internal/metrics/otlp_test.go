package metrics

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"clickcannon/internal/block"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	mpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// captureServer is an in-process OTLP/gRPC metrics endpoint that records every
// export request.
type captureServer struct {
	colmetricspb.UnimplementedMetricsServiceServer
	mu       sync.Mutex
	requests []*colmetricspb.ExportMetricsServiceRequest
}

func (s *captureServer) Export(_ context.Context, req *colmetricspb.ExportMetricsServiceRequest) (*colmetricspb.ExportMetricsServiceResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	return &colmetricspb.ExportMetricsServiceResponse{}, nil
}

func (s *captureServer) snapshot() []*colmetricspb.ExportMetricsServiceRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*colmetricspb.ExportMetricsServiceRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

func startCaptureServer(t *testing.T) (*captureServer, string) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	capture := &captureServer{}
	colmetricspb.RegisterMetricsServiceServer(srv, capture)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return capture, lis.Addr().String()
}

// httpCaptureServer records OTLP/HTTP exports; violations are collected, not failed inline, since an export can outlive the test body.
type httpCaptureServer struct {
	mu       sync.Mutex
	requests []*colmetricspb.ExportMetricsServiceRequest
	bad      []string
}

func (s *httpCaptureServer) snapshot() []*colmetricspb.ExportMetricsServiceRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*colmetricspb.ExportMetricsServiceRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

func (s *httpCaptureServer) violations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bad...)
}

func (s *httpCaptureServer) reject(w http.ResponseWriter, format string, args ...any) {
	s.mu.Lock()
	s.bad = append(s.bad, fmt.Sprintf(format, args...))
	s.mu.Unlock()
	http.Error(w, "bad request", http.StatusBadRequest)
}

func startHTTPCaptureServer(t *testing.T) (*httpCaptureServer, string) {
	t.Helper()
	capture := &httpCaptureServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/metrics" {
			capture.reject(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-protobuf" {
			capture.reject(w, "unexpected content type %q", ct)
			return
		}
		body := io.Reader(r.Body)
		if r.Header.Get("Content-Encoding") == "gzip" {
			zr, err := gzip.NewReader(body)
			if err != nil {
				capture.reject(w, "bad gzip body: %v", err)
				return
			}
			defer zr.Close()
			body = zr
		}
		data, err := io.ReadAll(body)
		if err != nil {
			capture.reject(w, "read body: %v", err)
			return
		}
		req := &colmetricspb.ExportMetricsServiceRequest{}
		if err := proto.Unmarshal(data, req); err != nil {
			capture.reject(w, "unmarshal request: %v", err)
			return
		}
		capture.mu.Lock()
		capture.requests = append(capture.requests, req)
		capture.mu.Unlock()
		respBody, _ := proto.Marshal(&colmetricspb.ExportMetricsServiceResponse{})
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(respBody)
	}))
	t.Cleanup(srv.Close)
	return capture, srv.URL
}

func attrValue(attrs []*commonpb.KeyValue, key string) (string, bool) {
	for _, kv := range attrs {
		if kv.Key == key {
			return kv.Value.GetStringValue(), true
		}
	}
	return "", false
}

func TestOTLPExportEndToEnd(t *testing.T) {
	capture, addr := startCaptureServer(t)

	cfg := Config{
		Enabled:    true,
		Attributes: map[string]string{"env": "test", "size": "L"},
		OTLP: OTLPConfig{
			Enabled:  true,
			URL:      addr,
			Insecure: true,
			Interval: time.Second,
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config with otlp-only sink should validate: %v", err)
	}

	pool := block.NewGarbageBlockPool(func() block.SharedColumns { return nil })
	queue := make(chan block.SharedColumns, 1)

	w, err := NewWorker(slog.New(slog.DiscardHandler), "run-123", "test-config", "logs", 0, 0, cfg.Attributes, &cfg, pool, queue)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	beforeStart := time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		_ = w.Run(ctx)
	}()

	// One counter, one per-attribute counter variant, one gauge, two samples.
	w.IncrementMetric(InsertRowsTotal, 5)
	w.IncrementMetricWithAttr(InsertRowsWorkerTotal, 7, "worker_id", "3")
	w.SetMetric(ActiveInserters, 3)
	sampleTime := time.Now()
	w.AddMetricPointWithAttributes(QueryLatencyMicros, 1234, map[string]string{"query_index": "0", "workflow": "wf_a"})
	w.AddMetricPointWithAttributes(QueryLatencyMicros, 5678, map[string]string{"query_index": "1", "workflow": "wf_a"})

	// Wait for at least one export (interval is 1s).
	deadline := time.Now().Add(10 * time.Second)
	var requests []*colmetricspb.ExportMetricsServiceRequest
	for time.Now().Before(deadline) {
		requests = capture.snapshot()
		if len(requests) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(requests) == 0 {
		t.Fatal("no otlp export received within deadline")
	}

	cancel()
	<-workerDone

	type gaugePoint struct {
		value int64
		ts    uint64
		attrs []*commonpb.KeyValue
	}
	var (
		sumValue        int64
		sumSeen         bool
		sumStart, sumTs uint64
		workerSumValue  int64
		workerSumAttr   string
		gaugeValue      int64
		gaugeSeen       bool
		samples         []gaugePoint
	)

	requests = capture.snapshot()
	for _, req := range requests {
		for _, rm := range req.ResourceMetrics {
			for key, want := range map[string]string{
				"service.name":         "clickcannon",
				"service.instance.id":  "run-123",
				"clickcannon.run_name": "test-config",
				"env":                  "test",
				"size":                 "L",
			} {
				got, ok := attrValue(rm.Resource.GetAttributes(), key)
				if !ok || got != want {
					t.Fatalf("resource attribute %q = %q (present=%v), want %q", key, got, ok, want)
				}
			}
			for _, sm := range rm.ScopeMetrics {
				if sm.Scope.GetName() != "clickcannon.metrics" || sm.Scope.GetVersion() != "1.0.0" {
					t.Fatalf("unexpected scope %q/%q", sm.Scope.GetName(), sm.Scope.GetVersion())
				}
				for _, m := range sm.Metrics {
					switch m.Name {
					case string(InsertRowsTotal):
						sum := m.GetSum()
						if sum == nil {
							t.Fatalf("%s must be a Sum, got %T", m.Name, m.Data)
						}
						if sum.AggregationTemporality != mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE {
							t.Fatalf("%s temporality = %v, want cumulative", m.Name, sum.AggregationTemporality)
						}
						if !sum.IsMonotonic {
							t.Fatalf("%s must be monotonic", m.Name)
						}
						for _, dp := range sum.DataPoints {
							sumSeen = true
							sumValue = dp.GetAsInt()
							sumStart = dp.StartTimeUnixNano
							sumTs = dp.TimeUnixNano
						}
					case string(InsertRowsWorkerTotal):
						sum := m.GetSum()
						if sum == nil || !sum.IsMonotonic || sum.AggregationTemporality != mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE {
							t.Fatalf("%s must be a cumulative monotonic Sum", m.Name)
						}
						for _, dp := range sum.DataPoints {
							workerSumValue = dp.GetAsInt()
							workerSumAttr, _ = attrValue(dp.Attributes, "worker_id")
						}
					case string(ActiveInserters):
						g := m.GetGauge()
						if g == nil {
							t.Fatalf("%s must be a Gauge, got %T", m.Name, m.Data)
						}
						for _, dp := range g.DataPoints {
							gaugeSeen = true
							gaugeValue = dp.GetAsInt()
						}
					case string(QueryLatencyMicros):
						g := m.GetGauge()
						if g == nil {
							t.Fatalf("%s must be a Gauge, got %T", m.Name, m.Data)
						}
						for _, dp := range g.DataPoints {
							samples = append(samples, gaugePoint{value: dp.GetAsInt(), ts: dp.TimeUnixNano, attrs: dp.Attributes})
						}
					}
				}
			}
		}
	}

	if !sumSeen || sumValue != 5 {
		t.Fatalf("insert_rows_total = %d (seen=%v), want 5", sumValue, sumSeen)
	}
	if sumStart == 0 || sumStart > sumTs {
		t.Fatalf("insert_rows_total start time %d must be nonzero and <= point time %d", sumStart, sumTs)
	}
	if workerSumValue != 7 || workerSumAttr != "3" {
		t.Fatalf("insert_rows_worker_total = %d with worker_id=%q, want 7 with worker_id=\"3\"", workerSumValue, workerSumAttr)
	}
	if !gaugeSeen || gaugeValue != 3 {
		t.Fatalf("active_inserters = %d (seen=%v), want 3", gaugeValue, gaugeSeen)
	}
	if len(samples) != 2 {
		t.Fatalf("query_latency_micros: got %d sample points, want 2", len(samples))
	}
	wantSamples := map[int64]string{1234: "0", 5678: "1"}
	for _, s := range samples {
		wantIdx, ok := wantSamples[s.value]
		if !ok {
			t.Fatalf("unexpected sample value %d", s.value)
		}
		gotIdx, _ := attrValue(s.attrs, "query_index")
		if gotIdx != wantIdx {
			t.Fatalf("sample %d has query_index=%q, want %q", s.value, gotIdx, wantIdx)
		}
		if wf, _ := attrValue(s.attrs, "workflow"); wf != "wf_a" {
			t.Fatalf("sample %d has workflow=%q, want wf_a", s.value, wf)
		}
		if s.ts < uint64(beforeStart.UnixNano()) || s.ts > uint64(sampleTime.Add(5*time.Second).UnixNano()) {
			t.Fatalf("sample %d timestamp %d outside expected range", s.value, s.ts)
		}
		delete(wantSamples, s.value)
	}
}

func TestOTLPExportSkipPreservesSamples(t *testing.T) {
	capture, addr := startCaptureServer(t)

	e, err := newOTLPEmitter(slog.New(slog.DiscardHandler), OTLPConfig{Enabled: true, URL: addr, Insecure: true}, "run-1", "cfg", nil, time.Now())
	if err != nil {
		t.Fatalf("newOTLPEmitter: %v", err)
	}
	defer e.close()

	e.addSample(Entry{Mode: EntryModePoint, Name: QueryLatencyMicros, Value: 42, Timestamp: time.Now()})

	// Previous export still in flight: the interval is skipped and the sample
	// must stay buffered.
	e.inFlight.Store(true)
	e.export(context.Background(), nil)
	if got := capture.snapshot(); len(got) != 0 {
		t.Fatalf("skipped interval exported %d requests, want 0", len(got))
	}
	e.mu.Lock()
	buffered := len(e.samples)
	e.mu.Unlock()
	if buffered != 1 {
		t.Fatalf("buffered samples after skipped interval = %d, want 1", buffered)
	}

	// Next interval exports the preserved sample.
	e.inFlight.Store(false)
	e.export(context.Background(), nil)
	deadline := time.Now().Add(10 * time.Second)
	var requests []*colmetricspb.ExportMetricsServiceRequest
	for time.Now().Before(deadline) {
		requests = capture.snapshot()
		if len(requests) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(requests) != 1 {
		t.Fatalf("got %d export requests, want 1", len(requests))
	}
	if got := findGaugeValues(requests, QueryLatencyMicros); len(got) != 1 || got[0] != 42 {
		t.Fatalf("exported sample values = %v, want [42]", got)
	}

	// An empty export must release the in-flight guard.
	e.export(context.Background(), nil)
	if e.inFlight.Load() {
		t.Fatal("empty export must not leave inFlight set")
	}
}

func TestOTLPShutdownFlush(t *testing.T) {
	capture, addr := startCaptureServer(t)

	cfg := Config{
		Enabled: true,
		OTLP: OTLPConfig{
			Enabled:  true,
			URL:      addr,
			Insecure: true,
			// The ticker never fires; only the shutdown flush exports.
			Interval: time.Minute,
		},
	}

	pool := block.NewGarbageBlockPool(func() block.SharedColumns { return nil })
	queue := make(chan block.SharedColumns, 1)

	w, err := NewWorker(slog.New(slog.DiscardHandler), "run-flush", "test-config", "logs", 0, 0, nil, &cfg, pool, queue)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		_ = w.Run(ctx)
	}()

	w.IncrementMetric(InsertRowsTotal, 9)
	w.AddMetricPoint(QueryLatencyMicros, 4321)

	cancel()
	<-workerDone

	requests := capture.snapshot()
	if len(requests) != 1 {
		t.Fatalf("got %d export requests, want 1 shutdown flush", len(requests))
	}

	var sumValue int64
	var sumSeen bool
	for _, req := range requests {
		for _, rm := range req.ResourceMetrics {
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					if m.Name != string(InsertRowsTotal) {
						continue
					}
					sum := m.GetSum()
					if sum == nil {
						t.Fatalf("%s must be a Sum, got %T", m.Name, m.Data)
					}
					for _, dp := range sum.DataPoints {
						sumSeen = true
						sumValue = dp.GetAsInt()
					}
				}
			}
		}
	}
	if !sumSeen || sumValue != 9 {
		t.Fatalf("flushed insert_rows_total = %d (seen=%v), want 9", sumValue, sumSeen)
	}
	if got := findGaugeValues(requests, QueryLatencyMicros); len(got) != 1 || got[0] != 4321 {
		t.Fatalf("flushed sample values = %v, want [4321]", got)
	}
}

func TestOTLPExportHTTP(t *testing.T) {
	capture, url := startHTTPCaptureServer(t)

	cfg := Config{
		Enabled: true,
		OTLP: OTLPConfig{
			Enabled:  true,
			Protocol: "http",
			URL:      url,
			Interval: time.Second,
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config with http otlp sink should validate: %v", err)
	}

	pool := block.NewGarbageBlockPool(func() block.SharedColumns { return nil })
	queue := make(chan block.SharedColumns, 1)

	w, err := NewWorker(slog.New(slog.DiscardHandler), "run-http", "test-config", "logs", 0, 0, nil, &cfg, pool, queue)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		_ = w.Run(ctx)
	}()

	w.IncrementMetric(InsertRowsTotal, 5)

	deadline := time.Now().Add(10 * time.Second)
	var requests []*colmetricspb.ExportMetricsServiceRequest
	for time.Now().Before(deadline) {
		requests = capture.snapshot()
		if len(requests) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(requests) == 0 {
		t.Fatal("no otlp/http export received within deadline")
	}

	cancel()
	<-workerDone

	if bad := capture.violations(); len(bad) > 0 {
		t.Fatalf("otlp/http protocol violations: %v", bad)
	}
	if got, ok := attrValue(requests[0].ResourceMetrics[0].Resource.GetAttributes(), "service.name"); !ok || got != "clickcannon" {
		t.Fatalf("service.name = %q (present=%v), want clickcannon", got, ok)
	}
	if got := findSumValues(requests[:1], InsertRowsTotal); len(got) != 1 || got[0] != 5 {
		t.Fatalf("insert_rows_total over http = %v, want [5]", got)
	}
}

func TestOTLPShutdownFlushHTTP(t *testing.T) {
	capture, url := startHTTPCaptureServer(t)

	cfg := Config{
		Enabled: true,
		OTLP: OTLPConfig{
			Enabled:     true,
			Protocol:    "http",
			URL:         url,
			Compression: "gzip",
			// The ticker never fires; only the shutdown flush exports.
			Interval: time.Minute,
		},
	}

	pool := block.NewGarbageBlockPool(func() block.SharedColumns { return nil })
	queue := make(chan block.SharedColumns, 1)

	w, err := NewWorker(slog.New(slog.DiscardHandler), "run-http-flush", "test-config", "logs", 0, 0, nil, &cfg, pool, queue)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		_ = w.Run(ctx)
	}()

	w.IncrementMetric(InsertRowsTotal, 9)
	w.AddMetricPoint(QueryLatencyMicros, 4321)

	cancel()
	<-workerDone

	if bad := capture.violations(); len(bad) > 0 {
		t.Fatalf("otlp/http protocol violations: %v", bad)
	}
	requests := capture.snapshot()
	if len(requests) != 1 {
		t.Fatalf("got %d export requests, want 1 shutdown flush", len(requests))
	}
	if got := findSumValues(requests, InsertRowsTotal); len(got) != 1 || got[0] != 9 {
		t.Fatalf("flushed insert_rows_total = %v, want [9]", got)
	}
	if got := findGaugeValues(requests, QueryLatencyMicros); len(got) != 1 || got[0] != 4321 {
		t.Fatalf("flushed sample values = %v, want [4321]", got)
	}
}

func TestOTLPHTTPURL(t *testing.T) {
	cases := []struct {
		raw      string
		insecure bool
		want     string
		wantErr  bool
	}{
		{raw: "localhost:4318", insecure: true, want: "http://localhost:4318/v1/metrics"},
		{raw: "localhost:4318", insecure: false, want: "https://localhost:4318/v1/metrics"},
		{raw: "http://localhost:4318", insecure: false, want: "http://localhost:4318/v1/metrics"},
		{raw: "https://collector/base/", insecure: true, want: "https://collector/base/v1/metrics"},
		{raw: "https://collector/v1/metrics", insecure: false, want: "https://collector/v1/metrics"},
		{raw: "grpc://localhost:4317", wantErr: true},
	}
	for _, tc := range cases {
		base, err := otlpBaseURL(tc.raw, tc.insecure)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("otlpBaseURL(%q) must fail", tc.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("otlpBaseURL(%q): %v", tc.raw, err)
		}
		if got := otlpMetricsURL(base); got != tc.want {
			t.Fatalf("metrics url for (%q, insecure=%v) = %q, want %q", tc.raw, tc.insecure, got, tc.want)
		}
	}
}

func findSumValues(requests []*colmetricspb.ExportMetricsServiceRequest, name Name) []int64 {
	var values []int64
	for _, req := range requests {
		for _, rm := range req.ResourceMetrics {
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					if m.Name != string(name) {
						continue
					}
					for _, dp := range m.GetSum().GetDataPoints() {
						values = append(values, dp.GetAsInt())
					}
				}
			}
		}
	}
	return values
}

func findGaugeValues(requests []*colmetricspb.ExportMetricsServiceRequest, name Name) []int64 {
	var values []int64
	for _, req := range requests {
		for _, rm := range req.ResourceMetrics {
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					if m.Name != string(name) {
						continue
					}
					for _, dp := range m.GetGauge().GetDataPoints() {
						values = append(values, dp.GetAsInt())
					}
				}
			}
		}
	}
	return values
}

func TestOTLPConfigValidate(t *testing.T) {
	base := OTLPConfig{Enabled: true, URL: "localhost:4317"}

	if err := (OTLPConfig{Enabled: false}).Validate(); err != nil {
		t.Fatalf("disabled otlp config must validate: %v", err)
	}
	if err := (OTLPConfig{Enabled: true}).Validate(); err == nil {
		t.Fatal("enabled otlp config without url must fail validation")
	}

	bad := base
	bad.Protocol = "tcp"
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown protocol must fail validation")
	}
	for _, p := range []string{"", "grpc", "http"} {
		ok := base
		ok.Protocol = p
		if err := ok.Validate(); err != nil {
			t.Fatalf("protocol %q must validate: %v", p, err)
		}
	}
	if got := (OTLPConfig{}).protocol(); got != "grpc" {
		t.Fatalf("default protocol = %q, want grpc", got)
	}

	bad = base
	bad.Compression = "zstd"
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown compression must fail validation")
	}
	for _, c := range []string{"", "none", "gzip"} {
		ok := base
		ok.Compression = c
		if err := ok.Validate(); err != nil {
			t.Fatalf("compression %q must validate: %v", c, err)
		}
	}

	bad = base
	bad.Interval = 500 * time.Millisecond
	if err := bad.Validate(); err == nil {
		t.Fatal("interval below 1s must fail validation")
	}
	if got := (OTLPConfig{}).withDefaults().Interval; got != 15*time.Second {
		t.Fatalf("default interval = %v, want 15s", got)
	}

	// The ClickHouse DSN becomes optional when the OTLP exporter is enabled.
	if err := (Config{Enabled: true, OTLP: base}).Validate(); err != nil {
		t.Fatalf("metrics config with otlp and no clickhouse_dsn must validate: %v", err)
	}
	if err := (Config{Enabled: true}).Validate(); err == nil {
		t.Fatal("metrics config without clickhouse_dsn or otlp must fail validation")
	}
}

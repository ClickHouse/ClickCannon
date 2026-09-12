package metricgen

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"clickcannon/internal/metrics"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	mpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/grpc"
)

// captureServer is an in-process OTLP/gRPC metrics endpoint that records every
// export request. Set rejectPerRequest before traffic starts to answer every
// request with an OTLP partial success.
type captureServer struct {
	colmetricspb.UnimplementedMetricsServiceServer
	rejectPerRequest int64
	rejectMessage    string

	mu       sync.Mutex
	requests []*colmetricspb.ExportMetricsServiceRequest
}

func (s *captureServer) Export(_ context.Context, req *colmetricspb.ExportMetricsServiceRequest) (*colmetricspb.ExportMetricsServiceResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	resp := &colmetricspb.ExportMetricsServiceResponse{}
	if s.rejectPerRequest > 0 {
		resp.PartialSuccess = &colmetricspb.ExportMetricsPartialSuccess{
			RejectedDataPoints: s.rejectPerRequest,
			ErrorMessage:       s.rejectMessage,
		}
	}
	return resp, nil
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

func exportTestConfig(url string) Config {
	cfg := Config{
		Enabled:             true,
		URL:                 url,
		Insecure:            true,
		Threads:             2,
		PointsPerRequest:    500,
		MetricCount:         25,
		CardinalityM:        40,
		CardinalityB:        0.95,
		Resources:           10,
		Interval:            15 * time.Second,
		StartTime:           "2026-01-01T00:00:00Z",
		Sweeps:              3,
		ExemplarProbability: 1,
	}
	return cfg.withDefaults()
}

// dpAttrKey identifies a stored series the way the v2 exporter would: data
// point attributes are unique per series within a metric by construction.
func dpAttrKey(attrs []attrKV) string {
	var sb strings.Builder
	for _, kv := range attrs {
		sb.WriteString(kv.Key)
		sb.WriteByte('=')
		sb.WriteString(kv.Value.GetStringValue())
		sb.WriteByte(',')
	}
	return sb.String()
}

func TestExportEndToEnd(t *testing.T) {
	capture, addr := startCaptureServer(t)
	cfg := exportTestConfig(addr)

	s := NewScheduler(slog.New(slog.DiscardHandler), &cfg, "test-seed", metrics.NewDisabledStore())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := s.Run(ctx); err != nil {
		t.Fatalf("scheduler: %v", err)
	}

	total, largest := cfg.totalSeries()
	wantPoints := total * uint64(cfg.Sweeps)
	_ = largest

	type seriesStats struct {
		points     int
		timestamps []uint64
	}
	var gotPoints uint64
	perMetricSeries := make(map[string]map[string]*seriesStats)
	typesSeen := make(map[string]int)
	temporalitiesSeen := make(map[mpb.AggregationTemporality]int)
	monotonicSeen := make(map[bool]int)
	exemplarEligible, exemplarPresent := 0, 0

	trackSeries := func(metricName string, attrs []attrKV, ts uint64) {
		m := perMetricSeries[metricName]
		if m == nil {
			m = make(map[string]*seriesStats)
			perMetricSeries[metricName] = m
		}
		key := dpAttrKey(attrs)
		st := m[key]
		if st == nil {
			st = &seriesStats{}
			m[key] = st
		}
		st.points++
		st.timestamps = append(st.timestamps, ts)
		gotPoints++
	}

	capture.mu.Lock()
	defer capture.mu.Unlock()
	for _, req := range capture.requests {
		for _, rm := range req.ResourceMetrics {
			if v := attrByKey(t, rm.Resource.Attributes, "service.name"); v == "" {
				t.Fatal("resource missing service.name")
			}
			attrByKey(t, rm.Resource.Attributes, "k8s.pod.name")
			for _, sm := range rm.ScopeMetrics {
				if sm.Scope.GetName() == "" {
					t.Fatal("scope missing name")
				}
				for _, m := range sm.Metrics {
					if m.Unit == "" || m.Description == "" {
						t.Fatalf("metric %q missing unit/description", m.Name)
					}
					switch data := m.Data.(type) {
					case *mpb.Metric_Gauge:
						typesSeen["gauge"] += len(data.Gauge.DataPoints)
						for _, dp := range data.Gauge.DataPoints {
							trackSeries(m.Name, dp.Attributes, dp.TimeUnixNano)
						}
					case *mpb.Metric_Sum:
						typesSeen["sum"] += len(data.Sum.DataPoints)
						temporalitiesSeen[data.Sum.AggregationTemporality]++
						monotonicSeen[data.Sum.IsMonotonic]++
						for _, dp := range data.Sum.DataPoints {
							trackSeries(m.Name, dp.Attributes, dp.TimeUnixNano)
							// Delta windows are (start, ts] so start must be
							// strictly earlier; a cumulative point may fall
							// exactly on its epoch start (counter just reset).
							if data.Sum.AggregationTemporality == mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA {
								if dp.StartTimeUnixNano >= dp.TimeUnixNano {
									t.Fatalf("delta sum %q start time not before timestamp", m.Name)
								}
							} else if dp.StartTimeUnixNano > dp.TimeUnixNano {
								t.Fatalf("cumulative sum %q start time after timestamp", m.Name)
							}
							exemplarEligible++
							exemplarPresent += len(dp.Exemplars)
							for _, ex := range dp.Exemplars {
								if len(ex.TraceId) != 16 || len(ex.SpanId) != 8 {
									t.Fatal("exemplar trace/span id lengths wrong")
								}
							}
						}
					case *mpb.Metric_Histogram:
						typesSeen["histogram"] += len(data.Histogram.DataPoints)
						temporalitiesSeen[data.Histogram.AggregationTemporality]++
						for _, dp := range data.Histogram.DataPoints {
							trackSeries(m.Name, dp.Attributes, dp.TimeUnixNano)
							if len(dp.BucketCounts) != len(dp.ExplicitBounds)+1 {
								t.Fatalf("histogram %q has %d buckets for %d bounds", m.Name, len(dp.BucketCounts), len(dp.ExplicitBounds))
							}
							var sum uint64
							for _, n := range dp.BucketCounts {
								sum += n
							}
							if sum != dp.Count {
								t.Fatalf("histogram %q count %d != bucket sum %d", m.Name, dp.Count, sum)
							}
							if dp.Sum == nil || dp.Min == nil || dp.Max == nil {
								t.Fatalf("histogram %q missing sum/min/max", m.Name)
							}
							exemplarEligible++
							exemplarPresent += len(dp.Exemplars)
						}
					case *mpb.Metric_ExponentialHistogram:
						typesSeen["exponential_histogram"] += len(data.ExponentialHistogram.DataPoints)
						temporalitiesSeen[data.ExponentialHistogram.AggregationTemporality]++
						for _, dp := range data.ExponentialHistogram.DataPoints {
							trackSeries(m.Name, dp.Attributes, dp.TimeUnixNano)
							var sum uint64
							for _, n := range dp.Positive.GetBucketCounts() {
								sum += n
							}
							for _, n := range dp.Negative.GetBucketCounts() {
								sum += n
							}
							sum += dp.ZeroCount
							if sum != dp.Count {
								t.Fatalf("exp histogram %q count %d != bucket sum %d", m.Name, dp.Count, sum)
							}
							exemplarEligible++
							exemplarPresent += len(dp.Exemplars)
						}
					case *mpb.Metric_Summary:
						typesSeen["summary"] += len(data.Summary.DataPoints)
						for _, dp := range data.Summary.DataPoints {
							trackSeries(m.Name, dp.Attributes, dp.TimeUnixNano)
							if len(dp.QuantileValues) == 0 {
								t.Fatalf("summary %q has no quantile values", m.Name)
							}
							for i := 1; i < len(dp.QuantileValues); i++ {
								if dp.QuantileValues[i].Quantile <= dp.QuantileValues[i-1].Quantile {
									t.Fatalf("summary %q quantiles not sorted", m.Name)
								}
								if dp.QuantileValues[i].Value < dp.QuantileValues[i-1].Value {
									t.Fatalf("summary %q values not ordered by quantile", m.Name)
								}
							}
						}
					default:
						t.Fatalf("metric %q has unexpected data type %T", m.Name, m.Data)
					}
				}
			}
		}
	}

	if gotPoints != wantPoints {
		t.Fatalf("received %d points, want %d (= %d series * %d sweeps)", gotPoints, wantPoints, total, cfg.Sweeps)
	}

	for _, typ := range []string{"gauge", "sum", "histogram", "exponential_histogram", "summary"} {
		if typesSeen[typ] == 0 {
			t.Fatalf("no %s points received", typ)
		}
	}
	if temporalitiesSeen[mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA] == 0 ||
		temporalitiesSeen[mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE] == 0 {
		t.Fatalf("both temporalities must appear, got %v", temporalitiesSeen)
	}
	if len(monotonicSeen) != 2 {
		t.Fatalf("both monotonic and non-monotonic sums must appear, got %v", monotonicSeen)
	}
	if exemplarPresent != exemplarEligible {
		t.Fatalf("exemplar_probability=1: want an exemplar on all %d eligible points, got %d", exemplarEligible, exemplarPresent)
	}

	// Exact per-metric cardinality: distinct data point attribute vectors per
	// metric must equal the configured decay value, and every series must have
	// exactly one point per sweep with interval spacing.
	names := metricNames(cfg.MetricCount)
	for i, name := range names {
		wantCard := cfg.cardinality(i + 1)
		series := perMetricSeries[name]
		if len(series) != wantCard {
			t.Fatalf("metric %q has %d distinct series, want %d", name, len(series), wantCard)
		}
		for key, st := range series {
			if st.points != cfg.Sweeps {
				t.Fatalf("metric %q series %s has %d points, want %d", name, key, st.points, cfg.Sweeps)
			}
			tsSet := make(map[uint64]struct{})
			var minTs, maxTs uint64 = ^uint64(0), 0
			for _, ts := range st.timestamps {
				tsSet[ts] = struct{}{}
				minTs = min(minTs, ts)
				maxTs = max(maxTs, ts)
			}
			if len(tsSet) != cfg.Sweeps {
				t.Fatalf("metric %q series has duplicate timestamps", name)
			}
			wantSpan := uint64(cfg.Interval.Nanoseconds()) * uint64(cfg.Sweeps-1)
			if maxTs-minTs != wantSpan {
				t.Fatalf("metric %q series spans %dns, want %dns", name, maxTs-minTs, wantSpan)
			}
		}
	}
}

// countingStore is a minimal in-memory metrics.Store for asserting counters.
type countingStore struct {
	mu     sync.Mutex
	counts map[metrics.Name]uint64
}

func newCountingStore() *countingStore {
	return &countingStore{counts: make(map[metrics.Name]uint64)}
}

func (s *countingStore) IncrementMetric(name metrics.Name, delta uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[name] += delta
}

func (s *countingStore) IncrementMetricWithAttr(name metrics.Name, delta uint64, attrKey, attrValue string) {
	s.IncrementMetric(name, delta)
}

func (s *countingStore) DecrementMetric(name metrics.Name, delta uint64) {}

func (s *countingStore) SetMetric(name metrics.Name, value uint64) {}

func (s *countingStore) GetMetric(name metrics.Name) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[name]
}

func (s *countingStore) AddMetricPoint(name metrics.Name, value uint64) {}

func (s *countingStore) AddMetricPointWithAttributes(name metrics.Name, value uint64, attributes map[string]string) {
}

// TestExportPartialSuccessNotRetried asserts the OTLP spec behavior: a partial
// success response is a success. The request is delivered exactly once (no
// retry re-sending accepted points), counts as exported, and the rejected
// points land on their own counter.
func TestExportPartialSuccessNotRetried(t *testing.T) {
	capture, addr := startCaptureServer(t)
	const rejectedPerRequest = 3
	capture.rejectPerRequest = rejectedPerRequest
	capture.rejectMessage = "attribute limit exceeded"

	cfg := exportTestConfig(addr)
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
	// One server-side request per counted request: a retry would re-deliver
	// the same payload and the server would see more requests than counted.
	if got := store.GetMetric(metrics.MetricGenRequestsTotal); got != requests {
		t.Fatalf("requests counter %d != %d requests received by server", got, requests)
	}
	total, _ := cfg.totalSeries()
	if got, want := store.GetMetric(metrics.MetricGenPointsTotal), total*uint64(cfg.Sweeps); got != want {
		t.Fatalf("points counter %d, want %d", got, want)
	}
	if got := store.GetMetric(metrics.MetricGenBytesTotal); got == 0 {
		t.Fatal("bytes counter not recorded")
	}
	if got, want := store.GetMetric(metrics.MetricGenPointsRejectedTotal), requests*rejectedPerRequest; got != want {
		t.Fatalf("rejected counter %d, want %d", got, want)
	}
}

func TestExportDeterministicAcrossRuns(t *testing.T) {
	sumsBySeed := make(map[string]string)
	for _, run := range []struct{ label, seed string }{
		{"a1", "seed-a"}, {"a2", "seed-a"}, {"b", "seed-b"},
	} {
		capture, addr := startCaptureServer(t)
		cfg := exportTestConfig(addr)
		cfg.MetricCount = 10
		cfg.CardinalityM = 20

		s := NewScheduler(slog.New(slog.DiscardHandler), &cfg, run.seed, metrics.NewDisabledStore())
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		if err := s.Run(ctx); err != nil {
			cancel()
			t.Fatalf("scheduler: %v", err)
		}
		cancel()

		// Fingerprint the run with an order-independent aggregate of every
		// numeric value.
		var sum float64
		var count int
		capture.mu.Lock()
		for _, req := range capture.requests {
			for _, rm := range req.ResourceMetrics {
				for _, sm := range rm.ScopeMetrics {
					for _, m := range sm.Metrics {
						if g := m.GetGauge(); g != nil {
							for _, dp := range g.DataPoints {
								sum += dp.GetAsDouble() + float64(dp.GetAsInt())
								count++
							}
						}
						if sm := m.GetSum(); sm != nil {
							for _, dp := range sm.DataPoints {
								sum += dp.GetAsDouble() + float64(dp.GetAsInt())
								count++
							}
						}
					}
				}
			}
		}
		capture.mu.Unlock()
		sumsBySeed[run.label] = fmt.Sprintf("%d:%.9g", count, sum)
	}

	if sumsBySeed["a1"] != sumsBySeed["a2"] {
		t.Fatalf("same seed produced different data: %s vs %s", sumsBySeed["a1"], sumsBySeed["a2"])
	}
	if sumsBySeed["a1"] == sumsBySeed["b"] {
		t.Fatal("different seeds produced identical data")
	}
}

// markerKey identifies one expected/observed staleness marker: the series
// (metric + data point attributes), the timestamp, and the pod name of the
// resource generation it was attributed to (the marker must carry the
// OUTGOING generation's resource identity or it would land on the wrong
// SeriesHash downstream).
func markerKey(metricName string, attrs []attrKV, tsNano uint64, pod string) string {
	return fmt.Sprintf("%s|%s|%d|%s", metricName, dpAttrKey(attrs), tsNano, pod)
}

// TestStalenessMarkers walks a churning run with staleness_markers enabled
// and asserts that exactly the expected marker points appear: one
// NoRecordedValue point per (series, churn boundary), timestamped at the
// first sweep after the boundary, attributed to the outgoing resource
// generation, with FLAG_NO_RECORDED_VALUE set and every value field zeroed,
// for all five point types.
func TestStalenessMarkers(t *testing.T) {
	capture, addr := startCaptureServer(t)
	cfg := Config{
		Enabled:           true,
		URL:               addr,
		Insecure:          true,
		Threads:           2,
		PointsPerRequest:  500,
		MetricCount:       5,
		TypeWeights:       TypeWeights{Gauge: 1, Sum: 1, Histogram: 1, ExponentialHistogram: 1, Summary: 1},
		CardinalityM:      12,
		CardinalityB:      1,
		Resources:         6,
		Interval:          15 * time.Second,
		TimestampJitterMs: 150,
		StartTime:         "2026-01-01T00:00:00Z",
		Sweeps:            9,
		ResourceLifetime:  60 * time.Second, // 4 sweeps per generation
		StalenessMarkers:  true,
	}
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config: %v", err)
	}

	seed := "marker-seed"
	s := NewScheduler(slog.New(slog.DiscardHandler), &cfg, seed, metrics.NewDisabledStore())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := s.Run(ctx); err != nil {
		t.Fatalf("scheduler: %v", err)
	}

	// Recompute the expected marker set from the same pure functions the
	// generator uses: a marker exists for series (def, j) at sweep k iff its
	// resource churned between k-1 and k.
	p, err := newPlan(cfg, seed, time.Now())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	expected := make(map[string]int)
	for _, def := range p.metrics {
		for j := range def.cardinality {
			sr := makeSeriesRef(def, j, cfg.Resources)
			for k := uint64(1); k < uint64(cfg.Sweeps); k++ {
				if !p.generationChanged(sr.resourceIdx, k) {
					continue
				}
				oldGen := p.generation(sr.resourceIdx, k) - 1
				pod := attrByKey(t, p.resources.build(sr.resourceIdx, oldGen).Attributes, "k8s.pod.name")
				ts := msToNano(p.pointTimeMs(sr.sVal, int64(k), sr.resourceIdx))
				expected[markerKey(def.name, sr.appendAttrs(nil), ts, pod)]++
			}
		}
	}
	if len(expected) == 0 {
		t.Fatal("test config produced no churn boundaries; markers untested")
	}

	got := make(map[string]int)
	markersByType := make(map[string]int)
	var realPoints int
	marker := func(metricName string, attrs []attrKV, ts uint64, pod, typ string) {
		got[markerKey(metricName, attrs, ts, pod)]++
		markersByType[typ]++
	}

	capture.mu.Lock()
	defer capture.mu.Unlock()
	for _, req := range capture.requests {
		for _, rm := range req.ResourceMetrics {
			pod := attrByKey(t, rm.Resource.Attributes, "k8s.pod.name")
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					switch data := m.Data.(type) {
					case *mpb.Metric_Gauge:
						for _, dp := range data.Gauge.DataPoints {
							if dp.Flags&1 == 0 {
								realPoints++
								continue
							}
							if dp.GetAsDouble() != 0 || dp.GetAsInt() != 0 {
								t.Fatalf("gauge marker %q has non-zero value", m.Name)
							}
							marker(m.Name, dp.Attributes, dp.TimeUnixNano, pod, "gauge")
						}
					case *mpb.Metric_Sum:
						for _, dp := range data.Sum.DataPoints {
							if dp.Flags&1 == 0 {
								realPoints++
								continue
							}
							if dp.GetAsDouble() != 0 || dp.GetAsInt() != 0 {
								t.Fatalf("sum marker %q has non-zero value", m.Name)
							}
							marker(m.Name, dp.Attributes, dp.TimeUnixNano, pod, "sum")
						}
					case *mpb.Metric_Histogram:
						for _, dp := range data.Histogram.DataPoints {
							if dp.Flags&1 == 0 {
								realPoints++
								continue
							}
							if dp.Count != 0 || dp.GetSum() != 0 || dp.GetMin() != 0 || dp.GetMax() != 0 {
								t.Fatalf("histogram marker %q has non-zero scalar values", m.Name)
							}
							// Bounds are series identity and must survive on the
							// marker; the counts array is same-length all-zero.
							if len(dp.ExplicitBounds) == 0 || len(dp.BucketCounts) != len(dp.ExplicitBounds)+1 {
								t.Fatalf("histogram marker %q lost its bounds shape", m.Name)
							}
							for _, n := range dp.BucketCounts {
								if n != 0 {
									t.Fatalf("histogram marker %q has non-zero bucket counts", m.Name)
								}
							}
							marker(m.Name, dp.Attributes, dp.TimeUnixNano, pod, "histogram")
						}
					case *mpb.Metric_ExponentialHistogram:
						for _, dp := range data.ExponentialHistogram.DataPoints {
							if dp.Flags&1 == 0 {
								realPoints++
								continue
							}
							if dp.Count != 0 || dp.GetSum() != 0 || dp.ZeroCount != 0 ||
								len(dp.Positive.GetBucketCounts()) != 0 || len(dp.Negative.GetBucketCounts()) != 0 {
								t.Fatalf("exp histogram marker %q has non-zero values", m.Name)
							}
							marker(m.Name, dp.Attributes, dp.TimeUnixNano, pod, "exponential_histogram")
						}
					case *mpb.Metric_Summary:
						for _, dp := range data.Summary.DataPoints {
							if dp.Flags&1 == 0 {
								realPoints++
								continue
							}
							if dp.Count != 0 || dp.Sum != 0 {
								t.Fatalf("summary marker %q has non-zero count/sum", m.Name)
							}
							if len(dp.QuantileValues) == 0 {
								t.Fatalf("summary marker %q lost its quantile levels", m.Name)
							}
							for _, qv := range dp.QuantileValues {
								if qv.Value != 0 {
									t.Fatalf("summary marker %q has non-zero quantile value", m.Name)
								}
							}
							marker(m.Name, dp.Attributes, dp.TimeUnixNano, pod, "summary")
						}
					}
				}
			}
		}
	}

	// Exact set equality: every expected marker appears exactly once, nothing
	// unexpected appears.
	for key, n := range expected {
		if got[key] != n {
			t.Fatalf("marker %s: got %d, want %d", key, got[key], n)
		}
	}
	for key, n := range got {
		if expected[key] != n {
			t.Fatalf("unexpected marker %s (x%d)", key, n)
		}
	}

	// The knob covers every point type, and markers are additive: real points
	// are unaffected.
	for _, typ := range []string{"gauge", "sum", "histogram", "exponential_histogram", "summary"} {
		if markersByType[typ] == 0 {
			t.Fatalf("no %s markers emitted", typ)
		}
	}
	total, _ := cfg.totalSeries()
	if wantReal := int(total) * cfg.Sweeps; realPoints != wantReal {
		t.Fatalf("real (unflagged) points: got %d, want %d", realPoints, wantReal)
	}
}

// TestStalenessMarkersOffByDefault churns without the knob and asserts no
// point carries FLAG_NO_RECORDED_VALUE.
func TestStalenessMarkersOffByDefault(t *testing.T) {
	capture, addr := startCaptureServer(t)
	cfg := exportTestConfig(addr)
	cfg.ResourceLifetime = 30 * time.Second // churn on, markers not requested
	cfg.Sweeps = 5

	s := NewScheduler(slog.New(slog.DiscardHandler), &cfg, "test-seed", metrics.NewDisabledStore())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := s.Run(ctx); err != nil {
		t.Fatalf("scheduler: %v", err)
	}

	var points int
	flagged := 0
	capture.mu.Lock()
	defer capture.mu.Unlock()
	for _, req := range capture.requests {
		for _, rm := range req.ResourceMetrics {
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					switch data := m.Data.(type) {
					case *mpb.Metric_Gauge:
						for _, dp := range data.Gauge.DataPoints {
							points++
							flagged += int(dp.Flags & 1)
						}
					case *mpb.Metric_Sum:
						for _, dp := range data.Sum.DataPoints {
							points++
							flagged += int(dp.Flags & 1)
						}
					case *mpb.Metric_Histogram:
						for _, dp := range data.Histogram.DataPoints {
							points++
							flagged += int(dp.Flags & 1)
						}
					case *mpb.Metric_ExponentialHistogram:
						for _, dp := range data.ExponentialHistogram.DataPoints {
							points++
							flagged += int(dp.Flags & 1)
						}
					case *mpb.Metric_Summary:
						for _, dp := range data.Summary.DataPoints {
							points++
							flagged += int(dp.Flags & 1)
						}
					}
				}
			}
		}
	}
	if points == 0 {
		t.Fatal("no points captured")
	}
	if flagged != 0 {
		t.Fatalf("staleness markers disabled but %d points carry FLAG_NO_RECORDED_VALUE", flagged)
	}
	total, _ := cfg.totalSeries()
	if want := int(total) * cfg.Sweeps; points != want {
		t.Fatalf("points: got %d, want %d (markers off must not change point counts)", points, want)
	}
}

package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	mpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

const (
	otlpScopeName    = "clickcannon.metrics"
	otlpScopeVersion = "1.0.0"

	// otlpExportTimeout bounds each export request; failures are logged and
	// dropped so the metrics worker is never blocked.
	otlpExportTimeout = 10 * time.Second

	// otlpMaxBufferedSamples bounds the sample-point buffer between exports;
	// when full the oldest sample is dropped.
	otlpMaxBufferedSamples = 10_000

	// otlpShutdownTimeout bounds the final synchronous flush on shutdown.
	otlpShutdownTimeout = 5 * time.Second
)

// otlpEmitter periodically exports the metrics worker's state as one OTLP
// metrics request. It is additive to the ClickHouse perf sink, both can run.
//
// Mapping (metric names are preserved exactly):
//   - names ending in "_total" -> cumulative monotonic int64 Sum, with
//     StartTimeUnixNano anchored at the worker start time
//   - all other cumulative/set values -> int64 Gauge
//   - per-attribute variants (e.g. worker_id-keyed counters) -> the same
//     metric name with the attribute on the data point
//   - sample points (EntryModePoint) -> int64 Gauge data points, one per
//     sample, keeping each sample's own timestamp and attributes
type otlpEmitter struct {
	log      *slog.Logger
	client   *otlpClient
	interval time.Duration

	resource  *resourcepb.Resource
	scope     *commonpb.InstrumentationScope
	startTime time.Time

	mu      sync.Mutex
	samples []Entry
	dropped uint64

	inFlight atomic.Bool
}

func newOTLPEmitter(log *slog.Logger, cfg OTLPConfig, runID, configName string, configAttributes map[string]string, startTime time.Time) (*otlpEmitter, error) {
	cfg = cfg.withDefaults()

	client, err := dialOTLP(cfg, otlpExportTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create otlp client: %w", err)
	}

	return &otlpEmitter{
		log:       log.With("component", "metrics_otlp"),
		client:    client,
		interval:  cfg.Interval,
		resource:  buildOTLPResource(runID, configName, configAttributes),
		scope:     &commonpb.InstrumentationScope{Name: otlpScopeName, Version: otlpScopeVersion},
		startTime: startTime,
	}, nil
}

// addSample buffers one sample point (EntryModePoint) until the next export.
// The buffer is bounded; when full the oldest sample is dropped.
func (e *otlpEmitter) addSample(m Entry) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.samples) >= otlpMaxBufferedSamples {
		copy(e.samples, e.samples[1:])
		e.samples = e.samples[:len(e.samples)-1]
		e.dropped++
	}
	e.samples = append(e.samples, m)
}

// drainSamples returns the buffered samples and the dropped-count since the
// last drain, resetting both.
func (e *otlpEmitter) drainSamples() ([]Entry, uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var samples []Entry
	if len(e.samples) > 0 {
		samples = make([]Entry, len(e.samples))
		copy(samples, e.samples)
		e.samples = e.samples[:0]
	}
	dropped := e.dropped
	e.dropped = 0
	return samples, dropped
}

// export snapshots the buffered samples, builds one OTLP request from them and
// the given cumulative snapshot, and sends it fire-and-forget: a failed export
// is logged at warn and its data dropped, never blocking the metrics worker.
// When the previous export is still in flight the interval is skipped and the
// buffered samples are kept for the next one.
func (e *otlpEmitter) export(ctx context.Context, snapshot map[metricKey]uint64) {
	if !e.inFlight.CompareAndSwap(false, true) {
		e.log.Warn("previous otlp export still in flight, skipping interval")
		return
	}

	samples, dropped := e.drainSamples()
	if dropped > 0 {
		e.log.Debug("otlp sample buffer overflow, dropped oldest samples", "dropped", dropped)
	}
	if len(snapshot) == 0 && len(samples) == 0 {
		e.inFlight.Store(false)
		return
	}

	req := e.buildRequest(snapshot, samples, time.Now())
	go func() {
		defer e.inFlight.Store(false)
		if err := e.client.export(ctx, req); err != nil {
			e.log.Warn("otlp export failed, dropping interval", "err", err)
		}
	}()
}

// flush synchronously exports the given snapshot plus any buffered samples.
// Called on shutdown when the run context is already cancelled, so it uses its
// own bounded context; a hung endpoint cannot block shutdown past the timeout.
func (e *otlpEmitter) flush(snapshot map[metricKey]uint64) {
	samples, dropped := e.drainSamples()
	if dropped > 0 {
		e.log.Debug("otlp sample buffer overflow, dropped oldest samples", "dropped", dropped)
	}
	if len(snapshot) == 0 && len(samples) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), otlpShutdownTimeout)
	defer cancel()

	req := e.buildRequest(snapshot, samples, time.Now())
	if err := e.client.export(ctx, req); err != nil {
		e.log.Warn("final otlp export failed", "err", err)
	}
}

func (e *otlpEmitter) close() {
	if err := e.client.close(); err != nil {
		e.log.Warn("failed to close otlp client", "err", err)
	}
}

func (e *otlpEmitter) buildRequest(snapshot map[metricKey]uint64, samples []Entry, now time.Time) *colmetricspb.ExportMetricsServiceRequest {
	nowNano := uint64(now.UnixNano())
	startNano := uint64(e.startTime.UnixNano())

	// Group data points by metric name so per-attribute variants land as
	// multiple data points on one metric.
	byName := make(map[Name]*mpb.Metric)
	var order []*mpb.Metric
	metricFor := func(name Name) *mpb.Metric {
		m, ok := byName[name]
		if !ok {
			m = &mpb.Metric{Name: string(name)}
			if strings.HasSuffix(string(name), "_total") {
				m.Data = &mpb.Metric_Sum{Sum: &mpb.Sum{
					AggregationTemporality: mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
					IsMonotonic:            true,
				}}
			} else {
				m.Data = &mpb.Metric_Gauge{Gauge: &mpb.Gauge{}}
			}
			byName[name] = m
			order = append(order, m)
		}
		return m
	}
	addPoint := func(name Name, dp *mpb.NumberDataPoint) {
		m := metricFor(name)
		switch data := m.Data.(type) {
		case *mpb.Metric_Sum:
			dp.StartTimeUnixNano = startNano
			data.Sum.DataPoints = append(data.Sum.DataPoints, dp)
		case *mpb.Metric_Gauge:
			data.Gauge.DataPoints = append(data.Gauge.DataPoints, dp)
		}
	}

	// Deterministic order keeps requests stable across intervals.
	keys := make([]metricKey, 0, len(snapshot))
	for key := range snapshot {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Name != keys[j].Name {
			return keys[i].Name < keys[j].Name
		}
		if keys[i].AttrKey != keys[j].AttrKey {
			return keys[i].AttrKey < keys[j].AttrKey
		}
		return keys[i].AttrValue < keys[j].AttrValue
	})

	for _, key := range keys {
		dp := &mpb.NumberDataPoint{
			TimeUnixNano: nowNano,
			Value:        &mpb.NumberDataPoint_AsInt{AsInt: int64(snapshot[key])},
		}
		if key.AttrKey != "" {
			dp.Attributes = []*commonpb.KeyValue{strKV(key.AttrKey, key.AttrValue)}
		}
		addPoint(key.Name, dp)
	}

	for _, s := range samples {
		addPoint(s.Name, &mpb.NumberDataPoint{
			TimeUnixNano: uint64(s.Timestamp.UnixNano()),
			Value:        &mpb.NumberDataPoint_AsInt{AsInt: int64(s.Value)},
			Attributes:   sortedKVs(s.Attributes),
		})
	}

	return &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*mpb.ResourceMetrics{{
			Resource: e.resource,
			ScopeMetrics: []*mpb.ScopeMetrics{{
				Scope:   e.scope,
				Metrics: order,
			}},
		}},
	}
}

func buildOTLPResource(runID, configName string, configAttributes map[string]string) *resourcepb.Resource {
	attrs := []*commonpb.KeyValue{
		strKV("service.name", "clickcannon"),
		strKV("service.instance.id", runID),
		strKV("clickcannon.run_name", configName),
	}
	attrs = append(attrs, sortedKVs(configAttributes)...)
	return &resourcepb.Resource{Attributes: attrs}
}

func strKV(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}},
	}
}

func sortedKVs(attributes map[string]string) []*commonpb.KeyValue {
	if len(attributes) == 0 {
		return nil
	}
	keys := make([]string, 0, len(attributes))
	for k := range attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	kvs := make([]*commonpb.KeyValue, 0, len(keys))
	for _, k := range keys {
		kvs = append(kvs, strKV(k, attributes[k]))
	}
	return kvs
}

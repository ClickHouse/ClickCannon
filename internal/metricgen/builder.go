package metricgen

import (
	"encoding/binary"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	mpb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

// builder accumulates synthesized points into an OTLP export request, grouping
// them by (resource, generation, scope) so the payload looks like real
// collector traffic: one ResourceMetrics per pod, one ScopeMetrics per
// instrumentation scope, points appended per metric. It is reused across
// requests (reset clears it) and is not safe for concurrent use: one per
// worker.
type builder struct {
	p      *plan
	groups map[uint64]*group
	order  []*group
	points int
}

type group struct {
	rm      *mpb.ResourceMetrics
	sm      *mpb.ScopeMetrics
	metrics map[int]*mpb.Metric
}

func newBuilder(p *plan) *builder {
	return &builder{p: p, groups: make(map[uint64]*group)}
}

func (b *builder) len() int { return b.points }

func (b *builder) build() *colmetricspb.ExportMetricsServiceRequest {
	rms := make([]*mpb.ResourceMetrics, len(b.order))
	for i, g := range b.order {
		rms[i] = g.rm
	}
	return &colmetricspb.ExportMetricsServiceRequest{ResourceMetrics: rms}
}

func (b *builder) reset() {
	clear(b.groups)
	b.order = b.order[:0]
	b.points = 0
}

func (b *builder) group(resourceIdx int, gen uint64, scopeIdx int) *group {
	key := mix3(uint64(resourceIdx), gen, uint64(scopeIdx))
	g := b.groups[key]
	if g == nil {
		scope := b.p.scopes[scopeIdx]
		sm := &mpb.ScopeMetrics{Scope: scope.scope, SchemaUrl: scope.schemaURL}
		g = &group{
			rm: &mpb.ResourceMetrics{
				Resource:     b.p.resources.get(resourceIdx, gen),
				ScopeMetrics: []*mpb.ScopeMetrics{sm},
			},
			sm:      sm,
			metrics: make(map[int]*mpb.Metric),
		}
		b.groups[key] = g
		b.order = append(b.order, g)
	}
	return g
}

func (g *group) metric(def *metricDef) *mpb.Metric {
	m := g.metrics[def.index]
	if m == nil {
		m = &mpb.Metric{Name: def.name, Description: def.description, Unit: def.unit}
		switch def.typ {
		case typeGauge:
			m.Data = &mpb.Metric_Gauge{Gauge: &mpb.Gauge{}}
		case typeSum:
			m.Data = &mpb.Metric_Sum{Sum: &mpb.Sum{
				AggregationTemporality: temporalityProto(def.temporality),
				IsMonotonic:            def.monotonic,
			}}
		case typeHistogram:
			m.Data = &mpb.Metric_Histogram{Histogram: &mpb.Histogram{
				AggregationTemporality: temporalityProto(def.temporality),
			}}
		case typeExpHistogram:
			m.Data = &mpb.Metric_ExponentialHistogram{ExponentialHistogram: &mpb.ExponentialHistogram{
				AggregationTemporality: temporalityProto(def.temporality),
			}}
		case typeSummary:
			m.Data = &mpb.Metric_Summary{Summary: &mpb.Summary{}}
		}
		g.metrics[def.index] = m
		g.sm.Metrics = append(g.sm.Metrics, m)
	}
	return m
}

func temporalityProto(t Temporality) mpb.AggregationTemporality {
	switch t {
	case TemporalityDelta:
		return mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA
	case TemporalityCumulative:
		return mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE
	default:
		return mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_UNSPECIFIED
	}
}

// noRecordedValueFlag is FLAG_NO_RECORDED_VALUE (DataPointFlags bit 0): the
// OTLP staleness marker, equivalent to a Prometheus staleness NaN.
const noRecordedValueFlag = uint32(mpb.DataPointFlags_DATA_POINT_FLAGS_NO_RECORDED_VALUE_MASK)

// addPoint synthesizes and appends the data point for series j of def at
// sweep k. This is the hot path: one call per (series, sweep).
func (b *builder) addPoint(def *metricDef, j int, k uint64) {
	s := makeSeriesRef(def, j, b.p.cfg.Resources)
	gen := b.p.generation(s.resourceIdx, k)

	// If the resource churned between sweep k-1 and k, the previous
	// generation's series just vanished: emit their final NoRecordedValue
	// marker before this sweep's real point (which belongs to the NEW
	// generation and is a different series).
	if b.p.cfg.StalenessMarkers && b.p.generationChanged(s.resourceIdx, k) {
		b.addStalenessMarker(def, s, k, gen-1)
	}

	g := b.group(s.resourceIdx, gen, def.scopeIdx)
	m := g.metric(def)

	tsNano := msToNano(b.p.pointTimeMs(s.sVal, int64(k), s.resourceIdx))
	attrs := s.appendAttrs(make([]attrKV, 0, len(def.dims)))

	switch def.typ {
	case typeGauge:
		dp := &mpb.NumberDataPoint{Attributes: attrs, TimeUnixNano: tsNano}
		setNumberValue(dp, s.gaugeValue(k), def.valueIsInt)
		gauge := m.GetGauge()
		gauge.DataPoints = append(gauge.DataPoints, dp)

	case typeSum:
		dp := &mpb.NumberDataPoint{Attributes: attrs, TimeUnixNano: tsNano}
		var v float64
		if def.temporality == TemporalityCumulative {
			epoch := s.epochStart(b.p, k)
			if def.monotonic {
				v = s.counterValue(k, epoch)
			} else {
				v = s.gaugeValue(k)
			}
			dp.StartTimeUnixNano = msToNano(b.p.pointTimeMs(s.sVal, epoch, s.resourceIdx))
		} else {
			v = s.deltaSumValue(k, def.monotonic)
			dp.StartTimeUnixNano = msToNano(b.p.pointTimeMs(s.sVal, int64(k)-1, s.resourceIdx))
		}
		setNumberValue(dp, v, def.valueIsInt)
		if ev, hi, lo, span, ok := s.exemplarFor(b.p, k, v); ok {
			dp.Exemplars = []*mpb.Exemplar{exemplarProto(ev, hi, lo, span, tsNano)}
		}
		sum := m.GetSum()
		sum.DataPoints = append(sum.DataPoints, dp)

	case typeHistogram:
		epoch, startNano := b.windowStart(s, def, k)
		counts := make([]uint64, len(def.bounds)+1)
		count, sumV, minV, maxV := s.histPoint(k, epoch, counts)
		dp := &mpb.HistogramDataPoint{
			Attributes:        attrs,
			StartTimeUnixNano: startNano,
			TimeUnixNano:      tsNano,
			Count:             count,
			Sum:               f64ptr(sumV),
			BucketCounts:      counts,
			ExplicitBounds:    def.bounds,
			Min:               f64ptr(minV),
			Max:               f64ptr(maxV),
		}
		if ev, hi, lo, span, ok := s.exemplarFor(b.p, k, maxV); ok {
			dp.Exemplars = []*mpb.Exemplar{exemplarProto(ev, hi, lo, span, tsNano)}
		}
		hist := m.GetHistogram()
		hist.DataPoints = append(hist.DataPoints, dp)

	case typeExpHistogram:
		epoch, startNano := b.windowStart(s, def, k)
		posLen, negLen := s.expBucketLens()
		pos := make([]uint64, posLen)
		var neg []uint64
		if negLen > 0 {
			neg = make([]uint64, negLen)
		}
		posOffset, negOffset, zeroCount, zeroThreshold, count, sumV, minV, maxV := s.expHistPoint(k, epoch, pos, neg)
		dp := &mpb.ExponentialHistogramDataPoint{
			Attributes:        attrs,
			StartTimeUnixNano: startNano,
			TimeUnixNano:      tsNano,
			Count:             count,
			Sum:               f64ptr(sumV),
			Scale:             def.expScale,
			ZeroCount:         zeroCount,
			ZeroThreshold:     zeroThreshold,
			Positive:          &mpb.ExponentialHistogramDataPoint_Buckets{Offset: posOffset, BucketCounts: pos},
			Min:               f64ptr(minV),
			Max:               f64ptr(maxV),
		}
		if neg != nil {
			dp.Negative = &mpb.ExponentialHistogramDataPoint_Buckets{Offset: negOffset, BucketCounts: neg}
		}
		if ev, hi, lo, span, ok := s.exemplarFor(b.p, k, maxV); ok {
			dp.Exemplars = []*mpb.Exemplar{exemplarProto(ev, hi, lo, span, tsNano)}
		}
		exp := m.GetExponentialHistogram()
		exp.DataPoints = append(exp.DataPoints, dp)

	case typeSummary:
		epoch := s.epochStart(b.p, k)
		qValues := make([]float64, len(def.quantiles))
		count, sumV := s.summaryPoint(k, epoch, qValues)
		qvs := make([]*mpb.SummaryDataPoint_ValueAtQuantile, len(def.quantiles))
		for i, q := range def.quantiles {
			qvs[i] = &mpb.SummaryDataPoint_ValueAtQuantile{Quantile: q, Value: qValues[i]}
		}
		dp := &mpb.SummaryDataPoint{
			Attributes:        attrs,
			StartTimeUnixNano: msToNano(b.p.pointTimeMs(s.sVal, epoch, s.resourceIdx)),
			TimeUnixNano:      tsNano,
			Count:             count,
			Sum:               sumV,
			QuantileValues:    qvs,
		}
		summary := m.GetSummary()
		summary.DataPoints = append(summary.DataPoints, dp)
	}

	b.points++
}

// addStalenessMarker appends the outgoing series' final NoRecordedValue point under the OLD generation's resource
// identity, mirroring prometheusreceiver when a scrape target disappears: identity fields (attributes, bounds, quantile levels) kept, values zeroed.
func (b *builder) addStalenessMarker(def *metricDef, s seriesRef, k, oldGen uint64) {
	g := b.group(s.resourceIdx, oldGen, def.scopeIdx)
	m := g.metric(def)

	tsNano := msToNano(b.p.pointTimeMs(s.sVal, int64(k), s.resourceIdx))
	attrs := s.appendAttrs(make([]attrKV, 0, len(def.dims)))

	switch def.typ {
	case typeGauge:
		dp := &mpb.NumberDataPoint{Attributes: attrs, TimeUnixNano: tsNano, Flags: noRecordedValueFlag}
		setNumberValue(dp, 0, def.valueIsInt)
		gauge := m.GetGauge()
		gauge.DataPoints = append(gauge.DataPoints, dp)

	case typeSum:
		dp := &mpb.NumberDataPoint{
			Attributes:        attrs,
			StartTimeUnixNano: b.markerStartNano(s, def, k),
			TimeUnixNano:      tsNano,
			Flags:             noRecordedValueFlag,
		}
		setNumberValue(dp, 0, def.valueIsInt)
		sum := m.GetSum()
		sum.DataPoints = append(sum.DataPoints, dp)

	case typeHistogram:
		dp := &mpb.HistogramDataPoint{
			Attributes:        attrs,
			StartTimeUnixNano: b.markerStartNano(s, def, k),
			TimeUnixNano:      tsNano,
			Flags:             noRecordedValueFlag,
			BucketCounts:      make([]uint64, len(def.bounds)+1),
			ExplicitBounds:    def.bounds, // bounds are series identity: the marker must keep them
		}
		hist := m.GetHistogram()
		hist.DataPoints = append(hist.DataPoints, dp)

	case typeExpHistogram:
		dp := &mpb.ExponentialHistogramDataPoint{
			Attributes:        attrs,
			StartTimeUnixNano: b.markerStartNano(s, def, k),
			TimeUnixNano:      tsNano,
			Flags:             noRecordedValueFlag,
			Scale:             def.expScale,
			Positive:          &mpb.ExponentialHistogramDataPoint_Buckets{},
		}
		exp := m.GetExponentialHistogram()
		exp.DataPoints = append(exp.DataPoints, dp)

	case typeSummary:
		qvs := make([]*mpb.SummaryDataPoint_ValueAtQuantile, len(def.quantiles))
		for i, q := range def.quantiles {
			// Quantile levels are series identity; values are zeroed.
			qvs[i] = &mpb.SummaryDataPoint_ValueAtQuantile{Quantile: q}
		}
		dp := &mpb.SummaryDataPoint{
			Attributes:        attrs,
			StartTimeUnixNano: b.markerStartNano(s, def, k),
			TimeUnixNano:      tsNano,
			Flags:             noRecordedValueFlag,
			QuantileValues:    qvs,
		}
		summary := m.GetSummary()
		summary.DataPoints = append(summary.DataPoints, dp)
	}

	b.points++
}

// markerStartNano is the StartTimeUnixNano a staleness marker carries: the
// same window/epoch start the outgoing series had at its final real point
// (sweep k-1, still inside the old generation): epoch start for cumulative
// temporality and summaries, the previous sweep for delta.
func (b *builder) markerStartNano(s seriesRef, def *metricDef, k uint64) uint64 {
	if def.temporality == TemporalityCumulative || def.typ == typeSummary {
		epoch := s.epochStart(b.p, k-1)
		return msToNano(b.p.pointTimeMs(s.sVal, epoch, s.resourceIdx))
	}
	return msToNano(b.p.pointTimeMs(s.sVal, int64(k)-1, s.resourceIdx))
}

// windowStart resolves the point's epoch and StartTimeUnixNano: the epoch
// start for cumulative temporality, the previous sweep for delta. Start times
// use the same per-(series, sweep) jitter as timestamps so delta windows stay
// contiguous: this window's start == the previous point's timestamp.
func (b *builder) windowStart(s seriesRef, def *metricDef, k uint64) (epoch int64, startNano uint64) {
	if def.temporality == TemporalityCumulative {
		epoch = s.epochStart(b.p, k)
		return epoch, msToNano(b.p.pointTimeMs(s.sVal, epoch, s.resourceIdx))
	}
	return int64(k) - 1, msToNano(b.p.pointTimeMs(s.sVal, int64(k)-1, s.resourceIdx))
}

func setNumberValue(dp *mpb.NumberDataPoint, v float64, isInt bool) {
	if isInt {
		dp.Value = &mpb.NumberDataPoint_AsInt{AsInt: int64(v)}
	} else {
		dp.Value = &mpb.NumberDataPoint_AsDouble{AsDouble: v}
	}
}

func exemplarProto(value float64, traceHi, traceLo, spanID uint64, tsNano uint64) *mpb.Exemplar {
	tid := make([]byte, 16)
	binary.BigEndian.PutUint64(tid[:8], traceHi)
	binary.BigEndian.PutUint64(tid[8:], traceLo)
	sid := make([]byte, 8)
	binary.BigEndian.PutUint64(sid, spanID)
	return &mpb.Exemplar{
		TimeUnixNano: tsNano,
		Value:        &mpb.Exemplar_AsDouble{AsDouble: value},
		TraceId:      tid,
		SpanId:       sid,
	}
}

func f64ptr(v float64) *float64 { return &v }

func msToNano(ms int64) uint64 {
	if ms < 0 {
		return 0
	}
	return uint64(ms) * 1_000_000
}

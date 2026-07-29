package otel

import (
	"clickcannon/internal/block"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

// metricsBuilder groups metric data points by (resource, scope) fingerprint
// into OTLP ResourceMetrics, and within each scope by (name, unit, description)
// into a single Metric, so data points of the same series definition are
// emitted together. One run only ever produces one metric type, so each Metric
// carries exactly one data oneof.
//
// Exemplars are not converted — generated data produces them empty, and the
// disk replay path drops any it finds (a documented passthrough limitation).
type metricsBuilder struct {
	groups map[uint64]*metricsScopeGroup
	order  []*metricspb.ResourceMetrics
	count  int
}

// metricsScopeGroup is one (resource, scope) bucket plus its per-metric index.
type metricsScopeGroup struct {
	sm      *metricspb.ScopeMetrics
	metrics map[uint64]*metricspb.Metric
}

func newMetricsBuilder() *metricsBuilder {
	return &metricsBuilder{groups: make(map[uint64]*metricsScopeGroup)}
}

func (b *metricsBuilder) len() int { return b.count }

func (b *metricsBuilder) add(r *block.MetricRow) {
	// Resource+scope fingerprint, same delimiter scheme as the logs builder.
	key := fnvStr(fnvOffset64, r.ServiceName)
	key = (key ^ 0x01) * fnvPrime64
	key = fnvStr(key, r.ResourceSchemaURL)
	key = (key ^ 0x02) * fnvPrime64
	key = fnvKVs(key, r.ResourceAttrs)
	key = (key ^ 0x2d) * fnvPrime64
	key = fnvStr(key, r.ScopeName)
	key = (key ^ 0x03) * fnvPrime64
	key = fnvStr(key, r.ScopeVersion)
	key = (key ^ 0x04) * fnvPrime64
	key = fnvStr(key, r.ScopeSchemaURL)
	key = (key ^ 0x05) * fnvPrime64
	key = fnvKVs(key, r.ScopeAttrs)

	g := b.groups[key]
	if g == nil {
		sm := &metricspb.ScopeMetrics{
			Scope: &commonpb.InstrumentationScope{
				Name:                   r.ScopeName,
				Version:                r.ScopeVersion,
				Attributes:             attrsFromKV(r.ScopeAttrs),
				DroppedAttributesCount: r.ScopeDroppedAttrCount,
			},
			SchemaUrl: r.ScopeSchemaURL,
		}
		rm := &metricspb.ResourceMetrics{
			Resource:     buildResource(r.ServiceName, r.ResourceAttrs),
			SchemaUrl:    r.ResourceSchemaURL,
			ScopeMetrics: []*metricspb.ScopeMetrics{sm},
		}
		g = &metricsScopeGroup{sm: sm, metrics: make(map[uint64]*metricspb.Metric)}
		b.groups[key] = g
		b.order = append(b.order, rm)
	}

	// Metric fingerprint within the scope group.
	mkey := fnvStr(fnvOffset64, r.MetricName)
	mkey = (mkey ^ 0x06) * fnvPrime64
	mkey = fnvStr(mkey, r.MetricUnit)
	mkey = (mkey ^ 0x07) * fnvPrime64
	mkey = fnvStr(mkey, r.MetricDescription)

	m := g.metrics[mkey]
	if m == nil {
		m = &metricspb.Metric{
			Name:        r.MetricName,
			Description: r.MetricDescription,
			Unit:        r.MetricUnit,
		}
		setMetricData(m, r)
		g.metrics[mkey] = m
		g.sm.Metrics = append(g.sm.Metrics, m)
	}

	appendDataPoint(m, r)
	b.count++
}

// setMetricData creates the empty data oneof for the row's metric type.
// Temporality and monotonicity live at the metric level in OTLP and are taken
// from the first row of the group (constant per series in this schema).
func setMetricData(m *metricspb.Metric, r *block.MetricRow) {
	switch r.Type {
	case block.MetricTypeGauge:
		m.Data = &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{}}
	case block.MetricTypeSum:
		m.Data = &metricspb.Metric_Sum{Sum: &metricspb.Sum{
			AggregationTemporality: metricspb.AggregationTemporality(r.AggregationTemporality),
			IsMonotonic:            r.IsMonotonic,
		}}
	case block.MetricTypeHistogram:
		m.Data = &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
			AggregationTemporality: metricspb.AggregationTemporality(r.AggregationTemporality),
		}}
	case block.MetricTypeExpHistogram:
		m.Data = &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{
			AggregationTemporality: metricspb.AggregationTemporality(r.AggregationTemporality),
		}}
	case block.MetricTypeSummary:
		m.Data = &metricspb.Metric_Summary{Summary: &metricspb.Summary{}}
	}
}

func appendDataPoint(m *metricspb.Metric, r *block.MetricRow) {
	switch d := m.Data.(type) {
	case *metricspb.Metric_Gauge:
		d.Gauge.DataPoints = append(d.Gauge.DataPoints, numberDataPoint(r))
	case *metricspb.Metric_Sum:
		d.Sum.DataPoints = append(d.Sum.DataPoints, numberDataPoint(r))
	case *metricspb.Metric_Histogram:
		sum := r.Sum
		minV := r.Min
		maxV := r.Max
		d.Histogram.DataPoints = append(d.Histogram.DataPoints, &metricspb.HistogramDataPoint{
			Attributes:        attrsFromKV(r.Attrs),
			StartTimeUnixNano: nano(r.StartTime),
			TimeUnixNano:      nano(r.Time),
			Count:             r.Count,
			Sum:               &sum,
			BucketCounts:      append([]uint64(nil), r.BucketCounts...),
			ExplicitBounds:    append([]float64(nil), r.ExplicitBounds...),
			Flags:             r.Flags,
			Min:               &minV,
			Max:               &maxV,
		})
	case *metricspb.Metric_ExponentialHistogram:
		sum := r.Sum
		minV := r.Min
		maxV := r.Max
		d.ExponentialHistogram.DataPoints = append(d.ExponentialHistogram.DataPoints, &metricspb.ExponentialHistogramDataPoint{
			Attributes:        attrsFromKV(r.Attrs),
			StartTimeUnixNano: nano(r.StartTime),
			TimeUnixNano:      nano(r.Time),
			Count:             r.Count,
			Sum:               &sum,
			Scale:             r.Scale,
			ZeroCount:         r.ZeroCount,
			Positive: &metricspb.ExponentialHistogramDataPoint_Buckets{
				Offset:       r.PositiveOffset,
				BucketCounts: append([]uint64(nil), r.PositiveBucketCounts...),
			},
			Negative: &metricspb.ExponentialHistogramDataPoint_Buckets{
				Offset:       r.NegativeOffset,
				BucketCounts: append([]uint64(nil), r.NegativeBucketCounts...),
			},
			Flags: r.Flags,
			Min:   &minV,
			Max:   &maxV,
		})
	case *metricspb.Metric_Summary:
		qv := make([]*metricspb.SummaryDataPoint_ValueAtQuantile, 0, len(r.QuantileQuantiles))
		for i := range r.QuantileQuantiles {
			v := 0.0
			if i < len(r.QuantileValues) {
				v = r.QuantileValues[i]
			}
			qv = append(qv, &metricspb.SummaryDataPoint_ValueAtQuantile{
				Quantile: r.QuantileQuantiles[i],
				Value:    v,
			})
		}
		d.Summary.DataPoints = append(d.Summary.DataPoints, &metricspb.SummaryDataPoint{
			Attributes:        attrsFromKV(r.Attrs),
			StartTimeUnixNano: nano(r.StartTime),
			TimeUnixNano:      nano(r.Time),
			Count:             r.Count,
			Sum:               r.Sum,
			QuantileValues:    qv,
			Flags:             r.Flags,
		})
	}
}

func numberDataPoint(r *block.MetricRow) *metricspb.NumberDataPoint {
	return &metricspb.NumberDataPoint{
		Attributes:        attrsFromKV(r.Attrs),
		StartTimeUnixNano: nano(r.StartTime),
		TimeUnixNano:      nano(r.Time),
		Value:             &metricspb.NumberDataPoint_AsDouble{AsDouble: r.Value},
		Flags:             r.Flags,
	}
}

func (b *metricsBuilder) build() *colmetricspb.ExportMetricsServiceRequest {
	return &colmetricspb.ExportMetricsServiceRequest{ResourceMetrics: b.order}
}

func (b *metricsBuilder) reset() {
	clear(b.groups)
	b.order = b.order[:0]
	b.count = 0
}

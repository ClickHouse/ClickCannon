package generate

import (
	"context"
	"fmt"
	"math"
	"time"

	"clickcannon/internal/block"
)

// metricNameToUnit maps a metric name to its canonical unit. Profile-defined
// MetricName pools should use these names so the unit column lines up; unknown
// names fall back to the profile's MetricUnit generator.
var metricNameToUnit = map[string]string{
	"http.server.duration":                  "ms",
	"http.server.request.size":              "By",
	"http.server.response.size":             "By",
	"http.client.duration":                  "ms",
	"rpc.server.duration":                   "ms",
	"db.client.connections.usage":           "{connection}",
	"system.cpu.utilization":                "1",
	"system.memory.usage":                   "By",
	"system.disk.io":                        "By",
	"system.network.io":                     "By",
	"process.runtime.jvm.memory.usage":      "By",
	"process.runtime.go.goroutines":         "{goroutine}",
	"process.runtime.go.mem.heap_alloc":     "By",
	"kafka.consumer.records_consumed_total": "{record}",
	"queue.size":                            "{message}",
	"app.orders.processed":                  "{order}",
}

// summaryQuantiles is the fixed quantile set emitted for summary metrics.
var summaryQuantiles = []float64{0.5, 0.9, 0.95, 0.99}

// aggregationTemporalityCumulative matches OTel's AGGREGATION_TEMPORALITY_CUMULATIVE.
const aggregationTemporalityCumulative = 2

// metricsSeries holds the values shared by every data point in one series.
type metricsSeries struct {
	metricName        string
	metricDescription string
	metricUnit        string
	serviceName       string
	scopeName         string
	scopeVersion      string
	resourceAttrs     map[string]string
	scopeAttrs        map[string]string
	pointAttrs        map[string]string
	startTime         time.Time
}

// MetricsFiller populates the Gen*Columns metrics variants with whole series
// (shared identity columns plus PointsPerSeries data points with advancing
// timestamps). Exemplars are always emitted empty.
type MetricsFiller struct {
	p    *Profile
	mcfg MetricsConfig
}

// NewMetricsFiller wraps a profile for metrics generation. Profile must already
// have applyDefaults() applied (GetProfile does this).
func NewMetricsFiller(p *Profile, mcfg MetricsConfig) *MetricsFiller {
	return &MetricsFiller{p: p, mcfg: mcfg}
}

// Fill emits whole series until at least n rows have been written. The concrete
// metrics column type decides which table schema is produced.
// Returns the number of rows actually written; may be less if ctx is cancelled.
func (f *MetricsFiller) Fill(ctx context.Context, rng *Rng, cols block.SharedColumns, n int) (int, error) {
	switch c := cols.(type) {
	case *GenMetricsGaugeColumns:
		return f.fillGauge(ctx, rng, c, n), nil
	case *GenMetricsSumColumns:
		return f.fillSum(ctx, rng, c, n), nil
	case *GenMetricsHistogramColumns:
		return f.fillHistogram(ctx, rng, c, n), nil
	case *GenMetricsExpHistogramColumns:
		return f.fillExpHistogram(ctx, rng, c, n), nil
	case *GenMetricsSummaryColumns:
		return f.fillSummary(ctx, rng, c, n), nil
	default:
		return 0, fmt.Errorf("unsupported metrics column type %T", cols)
	}
}

// newSeries draws the per-series identity values from the profile.
func (f *MetricsFiller) newSeries(rng *Rng, now time.Time, points int) metricsSeries {
	p := f.p

	metricName := p.MetricName.Generate(rng)
	unit := metricNameToUnit[metricName]
	if unit == "" {
		unit = p.MetricUnit.Generate(rng)
	}

	interval := time.Duration(f.mcfg.PointIntervalSeconds) * time.Second
	firstPoint := now.Add(-time.Duration(points-1) * interval)

	return metricsSeries{
		metricName:        metricName,
		metricDescription: p.MetricDescription.Generate(rng),
		metricUnit:        unit,
		serviceName:       p.ServiceName.Generate(rng),
		scopeName:         p.ScopeName.Generate(rng),
		scopeVersion:      p.ScopeVersion.Generate(rng),
		resourceAttrs:     p.ResourceAttrs.Generate(rng),
		scopeAttrs:        p.ScopeAttrs.Generate(rng),
		pointAttrs:        p.DataPointAttrs.Generate(rng),
		startTime:         firstPoint,
	}
}

// pointsForSeries samples the series length, clamped to the remaining rows.
func (f *MetricsFiller) pointsForSeries(rng *Rng, remaining int) int {
	points := f.mcfg.PointsPerSeriesMin
	if f.mcfg.PointsPerSeriesMax > f.mcfg.PointsPerSeriesMin {
		points += rng.IntN(f.mcfg.PointsPerSeriesMax - f.mcfg.PointsPerSeriesMin + 1)
	}
	if points < 1 {
		points = 1
	}
	if points > remaining {
		points = remaining
	}
	return points
}

// appendCommon writes the identity/timestamp columns shared by all metrics tables.
func appendCommon(cols *genMetricsColumnsBase, s *metricsSeries, pointTime time.Time) {
	cols.ResourceAttributes.Append(s.resourceAttrs)
	cols.ResourceSchemaUrl.Append("")
	cols.ScopeName.Append(s.scopeName)
	cols.ScopeVersion.Append(s.scopeVersion)
	cols.ScopeAttributes.Append(s.scopeAttrs)
	cols.ScopeDroppedAttrCount.Append(0)
	cols.ScopeSchemaUrl.Append("")
	cols.ServiceName.Append(s.serviceName)
	cols.MetricName.Append(s.metricName)
	cols.MetricDescription.Append(s.metricDescription)
	cols.MetricUnit.Append(s.metricUnit)
	cols.Attributes.Append(s.pointAttrs)
	cols.StartTimeUnix.Append(s.startTime)
	cols.TimeUnix.Append(pointTime)
}

func (f *MetricsFiller) fillGauge(ctx context.Context, rng *Rng, cols *GenMetricsGaugeColumns, n int) int {
	remaining := n
	now := time.Now()
	interval := time.Duration(f.mcfg.PointIntervalSeconds) * time.Second

	for remaining > 0 {
		select {
		case <-ctx.Done():
			return n - remaining
		default:
		}

		points := f.pointsForSeries(rng, remaining)
		s := f.newSeries(rng, now, points)

		// Random walk around a series-specific base level.
		base := rng.Float64() * 100
		for i := 0; i < points; i++ {
			appendCommon(&cols.genMetricsColumnsBase, &s, s.startTime.Add(time.Duration(i)*interval))
			cols.Value.Append(base + rng.Float64()*base*0.2)
			cols.Flags.Append(0)
			cols.appendEmpty()
		}

		remaining -= points
	}
	return n
}

func (f *MetricsFiller) fillSum(ctx context.Context, rng *Rng, cols *GenMetricsSumColumns, n int) int {
	remaining := n
	now := time.Now()
	interval := time.Duration(f.mcfg.PointIntervalSeconds) * time.Second

	for remaining > 0 {
		select {
		case <-ctx.Done():
			return n - remaining
		default:
		}

		points := f.pointsForSeries(rng, remaining)
		s := f.newSeries(rng, now, points)

		monotonic := rng.Float64() < 0.9
		value := rng.Float64() * 1000
		for i := 0; i < points; i++ {
			appendCommon(&cols.genMetricsColumnsBase, &s, s.startTime.Add(time.Duration(i)*interval))

			if monotonic {
				value += rng.Float64() * 100
			} else {
				value += rng.Float64()*100 - 50
			}
			cols.Value.Append(value)
			cols.Flags.Append(0)
			cols.appendEmpty()
			cols.AggregationTemporality.Append(aggregationTemporalityCumulative)
			cols.IsMonotonic.Append(monotonic)
		}

		remaining -= points
	}
	return n
}

func (f *MetricsFiller) fillHistogram(ctx context.Context, rng *Rng, cols *GenMetricsHistogramColumns, n int) int {
	remaining := n
	now := time.Now()
	interval := time.Duration(f.mcfg.PointIntervalSeconds) * time.Second

	bounds := make([]float64, 0, f.mcfg.HistogramBuckets)
	counts := make([]uint64, 0, f.mcfg.HistogramBuckets+1)

	for remaining > 0 {
		select {
		case <-ctx.Done():
			return n - remaining
		default:
		}

		points := f.pointsForSeries(rng, remaining)
		s := f.newSeries(rng, now, points)

		// Exponentially spaced explicit bounds, fixed per series (like an SDK).
		bounds = bounds[:0]
		bound := 0.5 + rng.Float64()
		for b := 0; b < f.mcfg.HistogramBuckets; b++ {
			bounds = append(bounds, bound)
			bound *= 2
		}

		// Cumulative totals that grow as the series progresses.
		var totalCount uint64
		var totalSum float64
		minVal := bounds[0] * rng.Float64()
		maxVal := minVal
		for i := 0; i < points; i++ {
			appendCommon(&cols.genMetricsColumnsBase, &s, s.startTime.Add(time.Duration(i)*interval))

			counts = counts[:0]
			for b := 0; b <= len(bounds); b++ {
				c := uint64(rng.IntN(100))
				counts = append(counts, c)
				totalCount += c

				// Approximate each observation at its bucket midpoint (or last bound).
				var mid float64
				switch {
				case b == 0:
					mid = bounds[0] / 2
				case b == len(bounds):
					mid = bounds[len(bounds)-1] * 1.5
				default:
					mid = (bounds[b-1] + bounds[b]) / 2
				}
				totalSum += mid * float64(c)
				if c > 0 && mid > maxVal {
					maxVal = mid
				}
			}

			cols.Count.Append(totalCount)
			cols.Sum.Append(totalSum)
			cols.BucketCounts.Append(counts)
			cols.ExplicitBounds.Append(bounds)
			cols.appendEmpty()
			cols.Flags.Append(0)
			cols.Min.Append(minVal)
			cols.Max.Append(maxVal)
			cols.AggregationTemporality.Append(aggregationTemporalityCumulative)
		}

		remaining -= points
	}
	return n
}

func (f *MetricsFiller) fillExpHistogram(ctx context.Context, rng *Rng, cols *GenMetricsExpHistogramColumns, n int) int {
	remaining := n
	now := time.Now()
	interval := time.Duration(f.mcfg.PointIntervalSeconds) * time.Second

	counts := make([]uint64, 0, f.mcfg.ExpHistogramBuckets)

	for remaining > 0 {
		select {
		case <-ctx.Done():
			return n - remaining
		default:
		}

		points := f.pointsForSeries(rng, remaining)
		s := f.newSeries(rng, now, points)

		scale := int32(f.mcfg.ExpHistogramScale)
		offset := int32(rng.IntN(64))
		// base^(offset) is the lower bound of the first positive bucket.
		growth := math.Pow(2, math.Pow(2, -float64(scale)))
		lowerBound := math.Pow(growth, float64(offset))

		var totalCount uint64
		var zeroCount uint64
		var totalSum float64
		minVal := lowerBound
		maxVal := lowerBound
		for i := 0; i < points; i++ {
			appendCommon(&cols.genMetricsColumnsBase, &s, s.startTime.Add(time.Duration(i)*interval))

			counts = counts[:0]
			bucketLower := lowerBound
			for b := 0; b < f.mcfg.ExpHistogramBuckets; b++ {
				c := uint64(rng.IntN(50))
				counts = append(counts, c)
				totalCount += c

				mid := bucketLower * (1 + growth) / 2
				totalSum += mid * float64(c)
				if c > 0 && mid > maxVal {
					maxVal = mid
				}
				bucketLower *= growth
			}
			z := uint64(rng.IntN(10))
			zeroCount += z
			totalCount += z
			if z > 0 {
				minVal = 0
			}

			cols.Count.Append(totalCount)
			cols.Sum.Append(totalSum)
			cols.Scale.Append(scale)
			cols.ZeroCount.Append(zeroCount)
			cols.PositiveOffset.Append(offset)
			cols.PositiveBucketCounts.Append(counts)
			cols.NegativeOffset.Append(0)
			cols.NegativeBucketCounts.Append(nil)
			cols.appendEmpty()
			cols.Flags.Append(0)
			cols.Min.Append(minVal)
			cols.Max.Append(maxVal)
			cols.AggregationTemporality.Append(aggregationTemporalityCumulative)
		}

		remaining -= points
	}
	return n
}

func (f *MetricsFiller) fillSummary(ctx context.Context, rng *Rng, cols *GenMetricsSummaryColumns, n int) int {
	remaining := n
	now := time.Now()
	interval := time.Duration(f.mcfg.PointIntervalSeconds) * time.Second

	values := make([]float64, 0, len(summaryQuantiles))

	for remaining > 0 {
		select {
		case <-ctx.Done():
			return n - remaining
		default:
		}

		points := f.pointsForSeries(rng, remaining)
		s := f.newSeries(rng, now, points)

		base := rng.Float64() * 500
		var totalCount uint64
		var totalSum float64
		for i := 0; i < points; i++ {
			appendCommon(&cols.genMetricsColumnsBase, &s, s.startTime.Add(time.Duration(i)*interval))

			// Monotone values across the fixed quantile set.
			values = values[:0]
			v := base * (0.5 + rng.Float64()*0.5)
			for range summaryQuantiles {
				values = append(values, v)
				v *= 1 + rng.Float64()
			}

			c := uint64(rng.IntN(1000) + 1)
			totalCount += c
			totalSum += values[0] * float64(c)

			cols.Count.Append(totalCount)
			cols.Sum.Append(totalSum)
			cols.QuantileQuantile.Append(summaryQuantiles)
			cols.QuantileValue.Append(values)
			cols.Flags.Append(0)
		}

		remaining -= points
	}
	return n
}

package otel

import (
	"context"
	"testing"
	"time"

	"clickcannon/internal/block"
	"clickcannon/internal/generate"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	gproto "google.golang.org/protobuf/proto"
)

// TestConvertMetricsFromGenerate runs generated metrics of every type through
// the reader and OTLP builder end-to-end and validates the resulting request
// is well-formed with the correct data oneof and data point counts.
func TestConvertMetricsFromGenerate(t *testing.T) {
	mcfg := generate.MetricsConfig{
		PointsPerSeriesMin:   5,
		PointsPerSeriesMax:   20,
		PointIntervalSeconds: 15,
		HistogramBuckets:     10,
		ExpHistogramBuckets:  20,
		ExpHistogramScale:    3,
	}

	cases := []struct {
		name string
		cols block.SharedColumns
		// countPoints returns the number of data points in the metric and
		// fails the test if the data oneof is the wrong type or malformed.
		countPoints func(t *testing.T, m *metricspb.Metric) int
	}{
		{
			name: "gauge",
			cols: generate.NewGenMetricsGaugeColumns(),
			countPoints: func(t *testing.T, m *metricspb.Metric) int {
				g := m.GetGauge()
				if g == nil {
					t.Fatalf("metric %q: not a gauge", m.Name)
				}
				for _, dp := range g.DataPoints {
					if dp.TimeUnixNano == 0 {
						t.Error("gauge data point timestamp is zero")
					}
					if _, ok := dp.Value.(*metricspb.NumberDataPoint_AsDouble); !ok {
						t.Error("gauge data point value is not double")
					}
				}
				return len(g.DataPoints)
			},
		},
		{
			name: "sum",
			cols: generate.NewGenMetricsSumColumns(),
			countPoints: func(t *testing.T, m *metricspb.Metric) int {
				s := m.GetSum()
				if s == nil {
					t.Fatalf("metric %q: not a sum", m.Name)
				}
				if s.AggregationTemporality != metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE {
					t.Errorf("sum temporality = %v, want cumulative", s.AggregationTemporality)
				}
				return len(s.DataPoints)
			},
		},
		{
			name: "histogram",
			cols: generate.NewGenMetricsHistogramColumns(),
			countPoints: func(t *testing.T, m *metricspb.Metric) int {
				h := m.GetHistogram()
				if h == nil {
					t.Fatalf("metric %q: not a histogram", m.Name)
				}
				for _, dp := range h.DataPoints {
					if len(dp.ExplicitBounds) != mcfg.HistogramBuckets {
						t.Errorf("bounds = %d, want %d", len(dp.ExplicitBounds), mcfg.HistogramBuckets)
					}
					if len(dp.BucketCounts) != len(dp.ExplicitBounds)+1 {
						t.Errorf("bucket counts = %d, want %d", len(dp.BucketCounts), len(dp.ExplicitBounds)+1)
					}
					if dp.Sum == nil || dp.Min == nil || dp.Max == nil {
						t.Error("histogram data point missing sum/min/max")
					}
				}
				return len(h.DataPoints)
			},
		},
		{
			name: "exponential_histogram",
			cols: generate.NewGenMetricsExpHistogramColumns(),
			countPoints: func(t *testing.T, m *metricspb.Metric) int {
				h := m.GetExponentialHistogram()
				if h == nil {
					t.Fatalf("metric %q: not an exponential histogram", m.Name)
				}
				for _, dp := range h.DataPoints {
					if dp.Scale != int32(mcfg.ExpHistogramScale) {
						t.Errorf("scale = %d, want %d", dp.Scale, mcfg.ExpHistogramScale)
					}
					if dp.Positive == nil || len(dp.Positive.BucketCounts) != mcfg.ExpHistogramBuckets {
						t.Errorf("positive buckets malformed: %+v", dp.Positive)
					}
					if dp.Negative == nil || len(dp.Negative.BucketCounts) != 0 {
						t.Errorf("negative buckets should be empty: %+v", dp.Negative)
					}
				}
				return len(h.DataPoints)
			},
		},
		{
			name: "summary",
			cols: generate.NewGenMetricsSummaryColumns(),
			countPoints: func(t *testing.T, m *metricspb.Metric) int {
				s := m.GetSummary()
				if s == nil {
					t.Fatalf("metric %q: not a summary", m.Name)
				}
				for _, dp := range s.DataPoints {
					if len(dp.QuantileValues) == 0 {
						t.Error("summary data point has no quantile values")
					}
					for i := 1; i < len(dp.QuantileValues); i++ {
						if dp.QuantileValues[i].Quantile <= dp.QuantileValues[i-1].Quantile {
							t.Error("summary quantiles not increasing")
						}
					}
				}
				return len(s.DataPoints)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prof, err := generate.GetProfile(generate.DefaultProfile)
			if err != nil {
				t.Fatalf("GetProfile: %v", err)
			}
			filler := generate.NewMetricsFiller(prof, mcfg)
			rng := generate.NewRng("seed", 0)

			const rows = 200
			n, err := filler.Fill(context.Background(), rng, tc.cols, rows)
			if err != nil {
				t.Fatalf("Fill: %v", err)
			}
			if n != rows {
				t.Fatalf("Fill wrote %d rows, want %d", n, rows)
			}

			reader, ok := tc.cols.(block.MetricsReader)
			if !ok {
				t.Fatalf("%T does not implement block.MetricsReader", tc.cols)
			}

			b := newMetricsBuilder()
			var row block.MetricRow
			for i := 0; i < reader.Rows(); i++ {
				reader.ReadMetricRow(i, &row)
				b.add(&row)
			}
			if b.len() != rows {
				t.Fatalf("builder len = %d, want %d", b.len(), rows)
			}

			req := b.build()
			total := 0
			for _, rm := range req.ResourceMetrics {
				if rm.Resource == nil {
					t.Fatal("resource metrics missing resource")
				}
				if !hasAttr(t, rm.Resource.Attributes, "service.name") {
					t.Error("resource missing service.name attribute")
				}
				for _, sm := range rm.ScopeMetrics {
					if sm.Scope == nil {
						t.Fatal("scope metrics missing scope")
					}
					for _, m := range sm.Metrics {
						if m.Name == "" {
							t.Error("metric missing name")
						}
						if m.Unit == "" {
							t.Error("metric missing unit")
						}
						total += tc.countPoints(t, m)
					}
				}
			}
			if total != rows {
				t.Fatalf("total data points across groups = %d, want %d", total, rows)
			}

			if _, err := gproto.Marshal(req); err != nil {
				t.Fatalf("marshal request: %v", err)
			}

			// A second batch must reset cleanly.
			b.reset()
			if b.len() != 0 {
				t.Fatalf("after reset len = %d, want 0", b.len())
			}
		})
	}
}

// TestConvertMetricsFieldMapping hand-builds rows and verifies exact OTLP field
// mapping for the sum, histogram, and summary shapes.
func TestConvertMetricsFieldMapping(t *testing.T) {
	start := time.Unix(1700000000, 0).UTC()
	ts := start.Add(30 * time.Second)

	common := block.MetricRow{
		ResourceAttrs:     []block.KV{{Key: "host.name", Value: "node-1"}},
		ServiceName:       "checkout",
		ScopeName:         "scope",
		ScopeVersion:      "1.2.3",
		MetricName:        "http.server.duration",
		MetricDescription: "duration of http requests",
		MetricUnit:        "ms",
		Attrs:             []block.KV{{Key: "http.method", Value: "GET"}},
		StartTime:         start,
		Time:              ts,
		Flags:             0,
	}

	t.Run("sum", func(t *testing.T) {
		r := common
		r.Type = block.MetricTypeSum
		r.Value = 42.5
		r.AggregationTemporality = 2
		r.IsMonotonic = true

		b := newMetricsBuilder()
		b.add(&r)
		req := b.build()

		if len(req.ResourceMetrics) != 1 {
			t.Fatalf("resource metrics = %d, want 1", len(req.ResourceMetrics))
		}
		rm := req.ResourceMetrics[0]
		if !hasAttr(t, rm.Resource.Attributes, "service.name") || !hasAttr(t, rm.Resource.Attributes, "host.name") {
			t.Error("resource attributes incomplete")
		}
		m := rm.ScopeMetrics[0].Metrics[0]
		if m.Name != "http.server.duration" || m.Unit != "ms" || m.Description != "duration of http requests" {
			t.Errorf("metric identity = %q %q %q", m.Name, m.Unit, m.Description)
		}
		s := m.GetSum()
		if s == nil {
			t.Fatal("not a sum")
		}
		if !s.IsMonotonic {
			t.Error("IsMonotonic not carried")
		}
		if s.AggregationTemporality != metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE {
			t.Errorf("temporality = %v", s.AggregationTemporality)
		}
		dp := s.DataPoints[0]
		if dp.GetAsDouble() != 42.5 {
			t.Errorf("value = %v, want 42.5", dp.GetAsDouble())
		}
		if dp.StartTimeUnixNano != uint64(start.UnixNano()) || dp.TimeUnixNano != uint64(ts.UnixNano()) {
			t.Errorf("timestamps = %d/%d", dp.StartTimeUnixNano, dp.TimeUnixNano)
		}
		if !hasAttr(t, dp.Attributes, "http.method") {
			t.Error("data point attributes missing")
		}
	})

	t.Run("histogram", func(t *testing.T) {
		r := common
		r.Type = block.MetricTypeHistogram
		r.Count = 10
		r.Sum = 123.4
		r.Min = 1
		r.Max = 50
		r.BucketCounts = []uint64{2, 5, 3}
		r.ExplicitBounds = []float64{10, 25}
		r.AggregationTemporality = 2

		b := newMetricsBuilder()
		b.add(&r)
		h := b.build().ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetHistogram()
		if h == nil {
			t.Fatal("not a histogram")
		}
		dp := h.DataPoints[0]
		if dp.Count != 10 || *dp.Sum != 123.4 || *dp.Min != 1 || *dp.Max != 50 {
			t.Errorf("count/sum/min/max = %d/%v/%v/%v", dp.Count, *dp.Sum, *dp.Min, *dp.Max)
		}
		if len(dp.BucketCounts) != 3 || len(dp.ExplicitBounds) != 2 {
			t.Errorf("buckets/bounds = %d/%d", len(dp.BucketCounts), len(dp.ExplicitBounds))
		}
	})

	t.Run("summary", func(t *testing.T) {
		r := common
		r.Type = block.MetricTypeSummary
		r.Count = 100
		r.Sum = 5000
		r.QuantileQuantiles = []float64{0.5, 0.99}
		r.QuantileValues = []float64{10, 90}

		b := newMetricsBuilder()
		b.add(&r)
		s := b.build().ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSummary()
		if s == nil {
			t.Fatal("not a summary")
		}
		dp := s.DataPoints[0]
		if dp.Count != 100 || dp.Sum != 5000 {
			t.Errorf("count/sum = %d/%v", dp.Count, dp.Sum)
		}
		if len(dp.QuantileValues) != 2 || dp.QuantileValues[1].Quantile != 0.99 || dp.QuantileValues[1].Value != 90 {
			t.Errorf("quantile values = %+v", dp.QuantileValues)
		}
	})

	// Two rows with the same metric identity must group into one Metric; a
	// different metric name must create a second Metric in the same scope.
	t.Run("grouping", func(t *testing.T) {
		r1 := common
		r1.Type = block.MetricTypeGauge
		r1.Value = 1
		r2 := r1
		r2.Value = 2
		r3 := r1
		r3.MetricName = "system.cpu.utilization"

		b := newMetricsBuilder()
		b.add(&r1)
		b.add(&r2)
		b.add(&r3)

		req := b.build()
		if len(req.ResourceMetrics) != 1 {
			t.Fatalf("resource metrics = %d, want 1", len(req.ResourceMetrics))
		}
		ms := req.ResourceMetrics[0].ScopeMetrics[0].Metrics
		if len(ms) != 2 {
			t.Fatalf("metrics = %d, want 2", len(ms))
		}
		if got := len(ms[0].GetGauge().DataPoints); got != 2 {
			t.Errorf("first metric data points = %d, want 2", got)
		}
		if got := len(ms[1].GetGauge().DataPoints); got != 1 {
			t.Errorf("second metric data points = %d, want 1", got)
		}
	})
}

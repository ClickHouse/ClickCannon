package generate

import (
	"context"
	"testing"
	"time"

	"clickcannon/internal/block"

	"github.com/ClickHouse/ch-go/proto"
)

// TestMetricsGenDiskRoundTrip generates a block for each metrics table type,
// encodes it as a Native block, and decodes it back into the disk-path
// columns. This proves the generate and disk column sets agree on column
// names, order, and types (a decode fails otherwise) and that values survive
// the round-trip.
func TestMetricsGenDiskRoundTrip(t *testing.T) {
	mcfg := MetricsConfig{
		PointsPerSeriesMin:   5,
		PointsPerSeriesMax:   20,
		PointIntervalSeconds: 15,
		HistogramBuckets:     10,
		ExpHistogramBuckets:  20,
		ExpHistogramScale:    3,
	}

	cases := []struct {
		name  string
		gen   block.SharedColumns
		disk  block.SharedColumns
		check func(t *testing.T, gen block.SharedColumns, res proto.Results, n int)
	}{
		{
			name: "gauge",
			gen:  NewGenMetricsGaugeColumns(),
			disk: block.NewMetricsGaugeSharedColumns(),
			check: func(t *testing.T, gen block.SharedColumns, res proto.Results, n int) {
				g := gen.(*GenMetricsGaugeColumns)
				value := findColumn(t, res, "Value").(*proto.ColFloat64)
				for i := 0; i < n; i++ {
					if got, want := (*value)[i], g.Value[i]; got != want {
						t.Fatalf("row %d Value = %v, want %v", i, got, want)
					}
				}
			},
		},
		{
			name: "sum",
			gen:  NewGenMetricsSumColumns(),
			disk: block.NewMetricsSumSharedColumns(),
			check: func(t *testing.T, gen block.SharedColumns, res proto.Results, n int) {
				g := gen.(*GenMetricsSumColumns)
				value := findColumn(t, res, "Value").(*proto.ColFloat64)
				temporality := findColumn(t, res, "AggregationTemporality").(*proto.ColInt32)
				monotonic := findColumn(t, res, "IsMonotonic").(*proto.ColBool)
				for i := 0; i < n; i++ {
					if got, want := (*value)[i], g.Value[i]; got != want {
						t.Fatalf("row %d Value = %v, want %v", i, got, want)
					}
					if got := (*temporality)[i]; got != aggregationTemporalityCumulative {
						t.Fatalf("row %d AggregationTemporality = %d, want %d", i, got, aggregationTemporalityCumulative)
					}
					if got, want := (*monotonic)[i], g.IsMonotonic[i]; got != want {
						t.Fatalf("row %d IsMonotonic = %v, want %v", i, got, want)
					}
				}
			},
		},
		{
			name: "histogram",
			gen:  NewGenMetricsHistogramColumns(),
			disk: block.NewMetricsHistogramSharedColumns(),
			check: func(t *testing.T, gen block.SharedColumns, res proto.Results, n int) {
				g := gen.(*GenMetricsHistogramColumns)
				count := findColumn(t, res, "Count").(*proto.ColUInt64)
				bucketCounts := findColumn(t, res, "BucketCounts").(*proto.ColArr[uint64])
				explicitBounds := findColumn(t, res, "ExplicitBounds").(*proto.ColArr[float64])
				for i := 0; i < n; i++ {
					if got, want := (*count)[i], g.Count[i]; got != want {
						t.Fatalf("row %d Count = %d, want %d", i, got, want)
					}
					bounds := explicitBounds.Row(i)
					if len(bounds) != mcfg.HistogramBuckets {
						t.Fatalf("row %d ExplicitBounds length = %d, want %d", i, len(bounds), mcfg.HistogramBuckets)
					}
					if got, want := bucketCounts.RowLen(i), len(bounds)+1; got != want {
						t.Fatalf("row %d BucketCounts length = %d, want %d", i, got, want)
					}
					for b := 1; b < len(bounds); b++ {
						if bounds[b] <= bounds[b-1] {
							t.Fatalf("row %d ExplicitBounds not increasing: %v", i, bounds)
						}
					}
				}
			},
		},
		{
			name: "exponential_histogram",
			gen:  NewGenMetricsExpHistogramColumns(),
			disk: block.NewMetricsExpHistogramSharedColumns(),
			check: func(t *testing.T, gen block.SharedColumns, res proto.Results, n int) {
				g := gen.(*GenMetricsExpHistogramColumns)
				count := findColumn(t, res, "Count").(*proto.ColUInt64)
				scale := findColumn(t, res, "Scale").(*proto.ColInt32)
				positive := findColumn(t, res, "PositiveBucketCounts").(*proto.ColArr[uint64])
				negative := findColumn(t, res, "NegativeBucketCounts").(*proto.ColArr[uint64])
				for i := 0; i < n; i++ {
					if got, want := (*count)[i], g.Count[i]; got != want {
						t.Fatalf("row %d Count = %d, want %d", i, got, want)
					}
					if got := (*scale)[i]; got != int32(mcfg.ExpHistogramScale) {
						t.Fatalf("row %d Scale = %d, want %d", i, got, mcfg.ExpHistogramScale)
					}
					if got := positive.RowLen(i); got != mcfg.ExpHistogramBuckets {
						t.Fatalf("row %d PositiveBucketCounts length = %d, want %d", i, got, mcfg.ExpHistogramBuckets)
					}
					if got := negative.RowLen(i); got != 0 {
						t.Fatalf("row %d NegativeBucketCounts length = %d, want 0", i, got)
					}
				}
			},
		},
		{
			name: "summary",
			gen:  NewGenMetricsSummaryColumns(),
			disk: block.NewMetricsSummarySharedColumns(),
			check: func(t *testing.T, gen block.SharedColumns, res proto.Results, n int) {
				g := gen.(*GenMetricsSummaryColumns)
				count := findColumn(t, res, "Count").(*proto.ColUInt64)
				quantiles := findColumn(t, res, "ValueAtQuantiles.Quantile").(*proto.ColArr[float64])
				values := findColumn(t, res, "ValueAtQuantiles.Value").(*proto.ColArr[float64])
				for i := 0; i < n; i++ {
					if got, want := (*count)[i], g.Count[i]; got != want {
						t.Fatalf("row %d Count = %d, want %d", i, got, want)
					}
					q := quantiles.Row(i)
					if len(q) != len(summaryQuantiles) {
						t.Fatalf("row %d quantile count = %d, want %d", i, len(q), len(summaryQuantiles))
					}
					v := values.Row(i)
					for j := 1; j < len(v); j++ {
						if v[j] < v[j-1] {
							t.Fatalf("row %d ValueAtQuantiles.Value not monotone: %v", i, v)
						}
					}
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := GetProfile("otel_demo")
			if err != nil {
				t.Fatalf("GetProfile: %v", err)
			}
			filler := NewMetricsFiller(p, mcfg)

			rng := NewRng("metrics-test", 0)
			const n = 200
			got, err := filler.Fill(context.Background(), rng, tc.gen, n)
			if err != nil {
				t.Fatalf("Fill: %v", err)
			}
			if got != n {
				t.Fatalf("Fill wrote %d rows, want %d", got, n)
			}

			genInput := tc.gen.Input()
			var buf proto.Buffer
			encBlock := proto.Block{Columns: len(genInput), Rows: n}
			if err := encBlock.EncodeRawBlock(&buf, blockVersion, genInput); err != nil {
				t.Fatalf("EncodeRawBlock: %v", err)
			}

			var decBlock proto.Block
			if err := decBlock.DecodeRawBlock(buf.Reader(), blockVersion, tc.disk.Results()); err != nil {
				t.Fatalf("DecodeRawBlock: %v", err)
			}
			if decBlock.Rows != n {
				t.Fatalf("decoded %d rows, want %d", decBlock.Rows, n)
			}

			// Column name/order parity between the two paths.
			if genCols, diskCols := genInput.Columns(), tc.disk.Input().Columns(); genCols != diskCols {
				t.Fatalf("column mismatch:\n gen:  %s\n disk: %s", genCols, diskCols)
			}

			res := tc.disk.Results()

			// Data points are grouped into series: many rows share a MetricName+ServiceName.
			metricName := findColumn(t, res, "MetricName").(proto.Column)
			serviceName := findColumn(t, res, "ServiceName").(proto.Column)
			distinct := map[string]struct{}{}
			for i := 0; i < n; i++ {
				distinct[block.ColStrRow(serviceName, i)+"\x00"+block.ColStrRow(metricName, i)] = struct{}{}
			}
			if len(distinct) >= n {
				t.Fatalf("expected points grouped under shared series, got %d distinct across %d rows", len(distinct), n)
			}

			// Timestamps survive the round-trip and StartTimeUnix <= TimeUnix.
			genBase := baseOf(tc.gen)
			timeUnix := findColumn(t, res, "TimeUnix").(*proto.ColDateTime)
			startTimeUnix := findColumn(t, res, "StartTimeUnix").(*proto.ColDateTime)
			for i := 0; i < n; i++ {
				want := genBase.TimeUnix.Data[i].Time().Truncate(time.Second)
				if got := timeUnix.Data[i].Time(); !got.Equal(want) {
					t.Fatalf("row %d TimeUnix = %v, want %v", i, got, want)
				}
				if startTimeUnix.Data[i].Time().After(timeUnix.Data[i].Time()) {
					t.Fatalf("row %d StartTimeUnix %v after TimeUnix %v", i, startTimeUnix.Data[i].Time(), timeUnix.Data[i].Time())
				}
			}

			// MetricUnit is never empty (name map or profile fallback).
			metricUnit := findColumn(t, res, "MetricUnit").(*proto.ColStr)
			for i := 0; i < n; i++ {
				if metricUnit.Row(i) == "" {
					t.Fatalf("row %d MetricUnit is empty", i)
				}
			}

			// The neutral MetricRow readers on both paths must agree, proving
			// the disk-path reader (LowCard/map handling) reads what the
			// generate-path reader wrote.
			genReader, ok := tc.gen.(block.MetricsReader)
			if !ok {
				t.Fatalf("%T does not implement block.MetricsReader", tc.gen)
			}
			diskReader, ok := tc.disk.(block.MetricsReader)
			if !ok {
				t.Fatalf("%T does not implement block.MetricsReader", tc.disk)
			}
			if diskReader.Rows() != n {
				t.Fatalf("disk reader rows = %d, want %d", diskReader.Rows(), n)
			}
			var genRow, diskRow block.MetricRow
			for i := 0; i < n; i++ {
				genReader.ReadMetricRow(i, &genRow)
				diskReader.ReadMetricRow(i, &diskRow)
				if genRow.Type != diskRow.Type {
					t.Fatalf("row %d Type = %v, want %v", i, diskRow.Type, genRow.Type)
				}
				if genRow.ServiceName != diskRow.ServiceName || genRow.MetricName != diskRow.MetricName || genRow.MetricUnit != diskRow.MetricUnit {
					t.Fatalf("row %d identity mismatch: gen=%q/%q/%q disk=%q/%q/%q", i,
						genRow.ServiceName, genRow.MetricName, genRow.MetricUnit,
						diskRow.ServiceName, diskRow.MetricName, diskRow.MetricUnit)
				}
				if !genRow.Time.Truncate(time.Second).Equal(diskRow.Time) {
					t.Fatalf("row %d Time = %v, want %v", i, diskRow.Time, genRow.Time)
				}
				if genRow.Value != diskRow.Value || genRow.Count != diskRow.Count || genRow.Sum != diskRow.Sum {
					t.Fatalf("row %d value mismatch: gen=%v/%d/%v disk=%v/%d/%v", i,
						genRow.Value, genRow.Count, genRow.Sum, diskRow.Value, diskRow.Count, diskRow.Sum)
				}
				if len(genRow.Attrs) != len(diskRow.Attrs) {
					t.Fatalf("row %d Attrs length = %d, want %d", i, len(diskRow.Attrs), len(genRow.Attrs))
				}
				for j := range genRow.Attrs {
					if genRow.Attrs[j] != diskRow.Attrs[j] {
						t.Fatalf("row %d Attrs[%d] = %v, want %v", i, j, diskRow.Attrs[j], genRow.Attrs[j])
					}
				}
			}

			tc.check(t, tc.gen, res, n)
		})
	}
}

// baseOf extracts the embedded genMetricsColumnsBase from any Gen*Columns metrics type.
func baseOf(cols block.SharedColumns) *genMetricsColumnsBase {
	switch c := cols.(type) {
	case *GenMetricsGaugeColumns:
		return &c.genMetricsColumnsBase
	case *GenMetricsSumColumns:
		return &c.genMetricsColumnsBase
	case *GenMetricsHistogramColumns:
		return &c.genMetricsColumnsBase
	case *GenMetricsExpHistogramColumns:
		return &c.genMetricsColumnsBase
	case *GenMetricsSummaryColumns:
		return &c.genMetricsColumnsBase
	default:
		return nil
	}
}

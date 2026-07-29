package block

import (
	"time"

	"github.com/ClickHouse/ch-go/proto"
)

// This file defines a neutral, SDK-free view over block column data so that an
// OTLP exporter can read rows out of a block without the block package (or the
// generate package) importing any OpenTelemetry types. The exporter converts
// these neutral rows into OTLP messages — a straight passthrough with no
// enrichment.

// KV is a neutral key/value attribute pair. Values are always strings, matching
// the ClickHouse Map(LowCardinality(String), String) attribute columns.
type KV struct {
	Key   string
	Value string
}

// LogRow is a neutral view of a single log row.
type LogRow struct {
	Timestamp         time.Time
	TraceID           string // hex, as stored (32 chars) — empty when absent
	SpanID            string // hex, as stored (16 chars) — empty when absent
	TraceFlags        uint32
	SeverityText      string
	SeverityNumber    int32
	ServiceName       string
	Body              string
	ResourceSchemaURL string
	ResourceAttrs     []KV
	ScopeSchemaURL    string
	ScopeName         string
	ScopeVersion      string
	ScopeAttrs        []KV
	LogAttrs          []KV
}

// TraceEvent is a neutral view of a single span event.
type TraceEvent struct {
	Timestamp time.Time
	Name      string
	Attrs     []KV
}

// TraceLink is a neutral view of a single span link.
type TraceLink struct {
	TraceID    string
	SpanID     string
	TraceState string
	Attrs      []KV
}

// TraceRow is a neutral view of a single span row. Note the traces schema has no
// resource/scope schema URL columns and no scope attributes column.
type TraceRow struct {
	Timestamp     time.Time
	TraceID       string
	SpanID        string
	ParentSpanID  string
	TraceState    string
	SpanName      string
	SpanKind      string
	ServiceName   string
	ResourceAttrs []KV
	ScopeName     string
	ScopeVersion  string
	SpanAttrs     []KV
	Duration      uint64 // nanoseconds
	StatusCode    string
	StatusMessage string
	Events        []TraceEvent
	Links         []TraceLink
}

// MetricType discriminates which OTel metrics table schema a MetricRow came
// from. One run only ever produces a single type, but the neutral row carries
// it so the exporter does not need out-of-band state.
type MetricType int

const (
	MetricTypeGauge MetricType = iota
	MetricTypeSum
	MetricTypeHistogram
	MetricTypeExpHistogram
	MetricTypeSummary
)

// MetricRow is a neutral view of a single metric data point row. Common fields
// are always set; typed fields are only meaningful for the matching Type.
// Exemplar columns are not carried — the exporter emits data points without
// exemplars (generated data produces them empty anyway).
type MetricRow struct {
	Type MetricType

	// Common columns (all five tables)
	ResourceAttrs         []KV
	ResourceSchemaURL     string
	ScopeName             string
	ScopeVersion          string
	ScopeAttrs            []KV
	ScopeDroppedAttrCount uint32
	ScopeSchemaURL        string
	ServiceName           string
	MetricName            string
	MetricDescription     string
	MetricUnit            string
	Attrs                 []KV
	StartTime             time.Time
	Time                  time.Time
	Flags                 uint32

	// Gauge / Sum
	Value                  float64
	AggregationTemporality int32 // sum, histogram, exponential histogram
	IsMonotonic            bool  // sum

	// Histogram / Exponential histogram / Summary
	Count uint64
	Sum   float64
	Min   float64 // histogram, exponential histogram
	Max   float64 // histogram, exponential histogram

	// Histogram
	BucketCounts   []uint64
	ExplicitBounds []float64

	// Exponential histogram
	Scale                int32
	ZeroCount            uint64
	PositiveOffset       int32
	PositiveBucketCounts []uint64
	NegativeOffset       int32
	NegativeBucketCounts []uint64

	// Summary
	QuantileQuantiles []float64
	QuantileValues    []float64
}

// MetricsReader is implemented by any block column set that holds metric rows.
type MetricsReader interface {
	Rows() int
	// ReadMetricRow fills dst with the row at index i, reusing dst's slices
	// where possible. All fields relevant to the row's Type are overwritten.
	ReadMetricRow(i int, dst *MetricRow)
}

// LogsReader is implemented by any block column set that holds log rows.
type LogsReader interface {
	// Rows returns the number of rows currently held.
	Rows() int
	// ReadLogRow fills dst with the row at index i, reusing dst's attribute
	// slices where possible. All fields of dst are overwritten.
	ReadLogRow(i int, dst *LogRow)
}

// TracesReader is implemented by any block column set that holds span rows.
type TracesReader interface {
	Rows() int
	ReadTraceRow(i int, dst *TraceRow)
}

// ColStrRow reads row i of a string-valued column, transparently handling the
// plain string column, the custom LowCard wrapper (disk decode path), and the
// standard ColLowCardinality (generate path). Returns "" for out-of-range or
// unsupported column types.
func ColStrRow(col proto.Column, i int) string {
	if i < 0 {
		return ""
	}
	switch c := col.(type) {
	case *proto.ColStr:
		if i < c.Rows() {
			return c.Row(i)
		}
	case *LowCard[string]:
		if i < c.Rows() {
			return c.RowString(i)
		}
	case *proto.ColLowCardinality[string]:
		if i < c.Rows() {
			return c.Row(i)
		}
	}
	return ""
}

// MapRowKV appends the key/value pairs of map row i (in stored column order) to
// dst and returns the extended slice. Handles both the custom LowCard key column
// (disk path) and the standard ColLowCardinality key column (generate path).
func MapRowKV(dst []KV, col proto.Column, i int) []KV {
	m, ok := col.(*proto.ColMap[string, string])
	if !ok || i < 0 || i >= m.Offsets.Rows() {
		return dst
	}
	start := 0
	end := int(m.Offsets[i])
	if i > 0 {
		start = int(m.Offsets[i-1])
	}
	keys, _ := m.Keys.(proto.Column)
	vals, _ := m.Values.(proto.Column)
	for idx := start; idx < end; idx++ {
		dst = append(dst, KV{Key: ColStrRow(keys, idx), Value: ColStrRow(vals, idx)})
	}
	return dst
}

// arrayRange returns the [start, end) element range of array row i.
func arrayRange(offsets proto.ColUInt64, i int) (int, int) {
	if i < 0 || i >= len(offsets) {
		return 0, 0
	}
	end := int(offsets[i])
	start := 0
	if i > 0 {
		start = int(offsets[i-1])
	}
	return start, end
}

// ReadEvents appends the span events of row i to dst. The three parallel array
// columns (timestamps, names, attributes) are indexed independently by their own
// offsets so mismatched lengths degrade gracefully rather than panic.
func ReadEvents(
	dst []TraceEvent,
	timestamps *proto.ColArr[proto.DateTime64],
	names *proto.ColArr[string],
	attrs *proto.ColArr[map[string]string],
	i int,
) []TraceEvent {
	tStart, tEnd := arrayRange(timestamps.Offsets, i)
	nStart, _ := arrayRange(names.Offsets, i)
	aStart, _ := arrayRange(attrs.Offsets, i)

	raw, _ := timestamps.Data.(*proto.ColDateTime64Raw)
	nameCol, _ := names.Data.(proto.Column)
	attrCol, _ := attrs.Data.(proto.Column)

	for j := 0; j < tEnd-tStart; j++ {
		ev := TraceEvent{}
		if raw != nil && tStart+j < len(raw.Data) {
			ev.Timestamp = raw.Data[tStart+j].Time(raw.Precision)
		}
		ev.Name = ColStrRow(nameCol, nStart+j)
		ev.Attrs = MapRowKV(nil, attrCol, aStart+j)
		dst = append(dst, ev)
	}
	return dst
}

// ReadLinks appends the span links of row i to dst.
func ReadLinks(
	dst []TraceLink,
	traceIDs *proto.ColArr[string],
	spanIDs *proto.ColArr[string],
	traceStates *proto.ColArr[string],
	attrs *proto.ColArr[map[string]string],
	i int,
) []TraceLink {
	tStart, tEnd := arrayRange(traceIDs.Offsets, i)
	sStart, _ := arrayRange(spanIDs.Offsets, i)
	stStart, _ := arrayRange(traceStates.Offsets, i)
	aStart, _ := arrayRange(attrs.Offsets, i)

	traceCol, _ := traceIDs.Data.(proto.Column)
	spanCol, _ := spanIDs.Data.(proto.Column)
	stateCol, _ := traceStates.Data.(proto.Column)
	attrCol, _ := attrs.Data.(proto.Column)

	for j := 0; j < tEnd-tStart; j++ {
		dst = append(dst, TraceLink{
			TraceID:    ColStrRow(traceCol, tStart+j),
			SpanID:     ColStrRow(spanCol, sStart+j),
			TraceState: ColStrRow(stateCol, stStart+j),
			Attrs:      MapRowKV(nil, attrCol, aStart+j),
		})
	}
	return dst
}

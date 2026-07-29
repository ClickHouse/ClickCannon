package generate

import (
	"time"

	"clickcannon/internal/block"

	"github.com/ClickHouse/ch-go/proto"
)

// newGenArrayFloat64 creates an Array(Float64) column.
func newGenArrayFloat64() *proto.ColArr[float64] {
	return &proto.ColArr[float64]{
		Data: new(proto.ColFloat64),
	}
}

// newGenArrayDateTime creates an Array(DateTime) column.
func newGenArrayDateTime() *proto.ColArr[time.Time] {
	return &proto.ColArr[time.Time]{
		Data: &proto.ColDateTime{Location: time.UTC},
	}
}

// genMetricsColumnsBase holds the columns shared by all five metrics tables
// for the generate path. Uses proto.ColLowCardinality[string] (with working
// Append) instead of the LowCard[string] wrapper used by the disk decode path.
// Concrete Gen*Columns embed it and append their type-specific columns.
type genMetricsColumnsBase struct {
	ResourceAttributes    *proto.ColMap[string, string]
	ResourceSchemaUrl     proto.ColStr
	ScopeName             proto.ColStr
	ScopeVersion          proto.ColStr
	ScopeAttributes       *proto.ColMap[string, string]
	ScopeDroppedAttrCount proto.ColUInt32
	ScopeSchemaUrl        proto.ColStr
	ServiceName           *proto.ColLowCardinality[string]
	MetricName            *proto.ColLowCardinality[string]
	MetricDescription     proto.ColStr
	MetricUnit            proto.ColStr
	Attributes            *proto.ColMap[string, string]
	StartTimeUnix         proto.ColDateTime
	TimeUnix              proto.ColDateTime

	names       []string
	cols        []proto.Column
	cachedInput proto.Input
}

func newGenMetricsColumnsBase() genMetricsColumnsBase {
	return genMetricsColumnsBase{
		ResourceAttributes:    newGenMap(),
		ResourceSchemaUrl:     proto.ColStr{},
		ScopeName:             proto.ColStr{},
		ScopeVersion:          proto.ColStr{},
		ScopeAttributes:       newGenMap(),
		ScopeDroppedAttrCount: proto.ColUInt32{},
		ScopeSchemaUrl:        proto.ColStr{},
		ServiceName:           newGenLowCardinality(),
		MetricName:            newGenLowCardinality(),
		MetricDescription:     proto.ColStr{},
		MetricUnit:            proto.ColStr{},
		Attributes:            newGenMap(),
		StartTimeUnix:         proto.ColDateTime{Location: time.UTC},
		TimeUnix:              proto.ColDateTime{Location: time.UTC},
	}
}

// initColumns builds the common name/column lists. It must be called on the
// base's final address (i.e. after embedding into the concrete struct) so the
// column pointers reference the embedded fields, not a temporary copy.
func (c *genMetricsColumnsBase) initColumns() {
	c.names = []string{
		"ResourceAttributes",
		"ResourceSchemaUrl",
		"ScopeName",
		"ScopeVersion",
		"ScopeAttributes",
		"ScopeDroppedAttrCount",
		"ScopeSchemaUrl",
		"ServiceName",
		"MetricName",
		"MetricDescription",
		"MetricUnit",
		"Attributes",
		"StartTimeUnix",
		"TimeUnix",
	}
	c.cols = []proto.Column{
		c.ResourceAttributes,
		&c.ResourceSchemaUrl,
		&c.ScopeName,
		&c.ScopeVersion,
		c.ScopeAttributes,
		&c.ScopeDroppedAttrCount,
		&c.ScopeSchemaUrl,
		c.ServiceName,
		c.MetricName,
		&c.MetricDescription,
		&c.MetricUnit,
		c.Attributes,
		&c.StartTimeUnix,
		&c.TimeUnix,
	}
}

func (c *genMetricsColumnsBase) addColumn(name string, col proto.Column) {
	c.names = append(c.names, name)
	c.cols = append(c.cols, col)
}

func (c *genMetricsColumnsBase) finishCache() {
	c.cachedInput = make(proto.Input, len(c.names))
	for i := range c.names {
		c.cachedInput[i] = proto.InputColumn{Name: c.names[i], Data: c.cols[i]}
	}
}

func (c *genMetricsColumnsBase) Reset() {
	for _, col := range c.cols {
		col.Reset()
	}
}

func (c *genMetricsColumnsBase) Results() proto.Results { return nil }

func (c *genMetricsColumnsBase) Input() proto.Input { return c.cachedInput }

func (c *genMetricsColumnsBase) FirstTimestamp() time.Time {
	if len(c.TimeUnix.Data) > 0 {
		return c.TimeUnix.Data[0].Time()
	}
	return time.Time{}
}

func (c *genMetricsColumnsBase) LastTimestamp() time.Time {
	if len(c.TimeUnix.Data) > 0 {
		return c.TimeUnix.Data[len(c.TimeUnix.Data)-1].Time()
	}
	return time.Time{}
}

func (c *genMetricsColumnsBase) UpdateDate()                               {}
func (c *genMetricsColumnsBase) ShiftTimestamp(_ block.ReplayTimeSnapshot) {}
func (c *genMetricsColumnsBase) UpdateTimestampNow()                       {}
func (c *genMetricsColumnsBase) UpdateTimestampMinute()                    {}
func (c *genMetricsColumnsBase) MutateIDs(_ int)                           {}

// genMetricsExemplarColumns holds the flattened Nested Exemplars columns for
// the generate path. The filler appends empty rows to keep offsets aligned.
type genMetricsExemplarColumns struct {
	ExemplarFilteredAttributes *proto.ColArr[map[string]string]
	ExemplarTimeUnix           *proto.ColArr[time.Time]
	ExemplarValue              *proto.ColArr[float64]
	ExemplarSpanID             *proto.ColArr[string]
	ExemplarTraceID            *proto.ColArr[string]
}

func newGenMetricsExemplarColumns() genMetricsExemplarColumns {
	return genMetricsExemplarColumns{
		ExemplarFilteredAttributes: newGenArrayMapLowCardinality(),
		ExemplarTimeUnix:           newGenArrayDateTime(),
		ExemplarValue:              newGenArrayFloat64(),
		ExemplarSpanID:             newGenArrayString(),
		ExemplarTraceID:            newGenArrayString(),
	}
}

func (e *genMetricsExemplarColumns) addTo(c *genMetricsColumnsBase) {
	c.addColumn("Exemplars.FilteredAttributes", e.ExemplarFilteredAttributes)
	c.addColumn("Exemplars.TimeUnix", e.ExemplarTimeUnix)
	c.addColumn("Exemplars.Value", e.ExemplarValue)
	c.addColumn("Exemplars.SpanId", e.ExemplarSpanID)
	c.addColumn("Exemplars.TraceId", e.ExemplarTraceID)
}

// appendEmpty appends an empty exemplar set for one data point row.
func (e *genMetricsExemplarColumns) appendEmpty() {
	e.ExemplarFilteredAttributes.Append(nil)
	e.ExemplarTimeUnix.Append(nil)
	e.ExemplarValue.Append(nil)
	e.ExemplarSpanID.Append(nil)
	e.ExemplarTraceID.Append(nil)
}

// =============================================================================
// Gauge
// =============================================================================

type GenMetricsGaugeColumns struct {
	genMetricsColumnsBase

	Value proto.ColFloat64
	Flags proto.ColUInt32
	genMetricsExemplarColumns
}

func NewGenMetricsGaugeColumns() *GenMetricsGaugeColumns {
	c := &GenMetricsGaugeColumns{
		genMetricsColumnsBase:     newGenMetricsColumnsBase(),
		genMetricsExemplarColumns: newGenMetricsExemplarColumns(),
	}

	c.initColumns()
	c.addColumn("Value", &c.Value)
	c.addColumn("Flags", &c.Flags)
	c.genMetricsExemplarColumns.addTo(&c.genMetricsColumnsBase)
	c.finishCache()

	return c
}

// =============================================================================
// Sum
// =============================================================================

type GenMetricsSumColumns struct {
	genMetricsColumnsBase

	Value proto.ColFloat64
	Flags proto.ColUInt32
	genMetricsExemplarColumns
	AggregationTemporality proto.ColInt32
	IsMonotonic            proto.ColBool
}

func NewGenMetricsSumColumns() *GenMetricsSumColumns {
	c := &GenMetricsSumColumns{
		genMetricsColumnsBase:     newGenMetricsColumnsBase(),
		genMetricsExemplarColumns: newGenMetricsExemplarColumns(),
	}

	c.initColumns()
	c.addColumn("Value", &c.Value)
	c.addColumn("Flags", &c.Flags)
	c.genMetricsExemplarColumns.addTo(&c.genMetricsColumnsBase)
	c.addColumn("AggregationTemporality", &c.AggregationTemporality)
	c.addColumn("IsMonotonic", &c.IsMonotonic)
	c.finishCache()

	return c
}

// =============================================================================
// Histogram
// =============================================================================

type GenMetricsHistogramColumns struct {
	genMetricsColumnsBase

	Count          proto.ColUInt64
	Sum            proto.ColFloat64
	BucketCounts   *proto.ColArr[uint64]
	ExplicitBounds *proto.ColArr[float64]
	genMetricsExemplarColumns
	Flags                  proto.ColUInt32
	Min                    proto.ColFloat64
	Max                    proto.ColFloat64
	AggregationTemporality proto.ColInt32
}

func NewGenMetricsHistogramColumns() *GenMetricsHistogramColumns {
	c := &GenMetricsHistogramColumns{
		genMetricsColumnsBase:     newGenMetricsColumnsBase(),
		BucketCounts:              newGenArrayUInt64(),
		ExplicitBounds:            newGenArrayFloat64(),
		genMetricsExemplarColumns: newGenMetricsExemplarColumns(),
	}

	c.initColumns()
	c.addColumn("Count", &c.Count)
	c.addColumn("Sum", &c.Sum)
	c.addColumn("BucketCounts", c.BucketCounts)
	c.addColumn("ExplicitBounds", c.ExplicitBounds)
	c.genMetricsExemplarColumns.addTo(&c.genMetricsColumnsBase)
	c.addColumn("Flags", &c.Flags)
	c.addColumn("Min", &c.Min)
	c.addColumn("Max", &c.Max)
	c.addColumn("AggregationTemporality", &c.AggregationTemporality)
	c.finishCache()

	return c
}

// =============================================================================
// Exponential histogram
// =============================================================================

type GenMetricsExpHistogramColumns struct {
	genMetricsColumnsBase

	Count                proto.ColUInt64
	Sum                  proto.ColFloat64
	Scale                proto.ColInt32
	ZeroCount            proto.ColUInt64
	PositiveOffset       proto.ColInt32
	PositiveBucketCounts *proto.ColArr[uint64]
	NegativeOffset       proto.ColInt32
	NegativeBucketCounts *proto.ColArr[uint64]
	genMetricsExemplarColumns
	Flags                  proto.ColUInt32
	Min                    proto.ColFloat64
	Max                    proto.ColFloat64
	AggregationTemporality proto.ColInt32
}

func NewGenMetricsExpHistogramColumns() *GenMetricsExpHistogramColumns {
	c := &GenMetricsExpHistogramColumns{
		genMetricsColumnsBase:     newGenMetricsColumnsBase(),
		PositiveBucketCounts:      newGenArrayUInt64(),
		NegativeBucketCounts:      newGenArrayUInt64(),
		genMetricsExemplarColumns: newGenMetricsExemplarColumns(),
	}

	c.initColumns()
	c.addColumn("Count", &c.Count)
	c.addColumn("Sum", &c.Sum)
	c.addColumn("Scale", &c.Scale)
	c.addColumn("ZeroCount", &c.ZeroCount)
	c.addColumn("PositiveOffset", &c.PositiveOffset)
	c.addColumn("PositiveBucketCounts", c.PositiveBucketCounts)
	c.addColumn("NegativeOffset", &c.NegativeOffset)
	c.addColumn("NegativeBucketCounts", c.NegativeBucketCounts)
	c.genMetricsExemplarColumns.addTo(&c.genMetricsColumnsBase)
	c.addColumn("Flags", &c.Flags)
	c.addColumn("Min", &c.Min)
	c.addColumn("Max", &c.Max)
	c.addColumn("AggregationTemporality", &c.AggregationTemporality)
	c.finishCache()

	return c
}

// =============================================================================
// Summary
// =============================================================================

type GenMetricsSummaryColumns struct {
	genMetricsColumnsBase

	Count            proto.ColUInt64
	Sum              proto.ColFloat64
	QuantileQuantile *proto.ColArr[float64]
	QuantileValue    *proto.ColArr[float64]
	Flags            proto.ColUInt32
}

func NewGenMetricsSummaryColumns() *GenMetricsSummaryColumns {
	c := &GenMetricsSummaryColumns{
		genMetricsColumnsBase: newGenMetricsColumnsBase(),
		QuantileQuantile:      newGenArrayFloat64(),
		QuantileValue:         newGenArrayFloat64(),
	}

	c.initColumns()
	c.addColumn("Count", &c.Count)
	c.addColumn("Sum", &c.Sum)
	c.addColumn("ValueAtQuantiles.Quantile", c.QuantileQuantile)
	c.addColumn("ValueAtQuantiles.Value", c.QuantileValue)
	c.addColumn("Flags", &c.Flags)
	c.finishCache()

	return c
}

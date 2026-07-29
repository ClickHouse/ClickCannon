package block

import (
	"time"

	"github.com/ClickHouse/ch-go/proto"
)

// Metrics column sets for the disk decode path. One SharedColumns
// implementation per OTel exporter metrics table (gauge, sum, histogram,
// exponential histogram, summary). Column names, order, and types match the
// exporter DDL exactly, including the flattened Nested Exemplars /
// ValueAtQuantiles columns, so `SELECT * ... FORMAT Native` exports decode
// directly.

// metricsColumnsBase holds the columns shared by all five metrics tables plus
// the SharedColumns plumbing. Concrete types embed it and append their
// type-specific columns before calling finishCache.
type metricsColumnsBase struct {
	resourceAttributes    *proto.ColMap[string, string]
	resourceSchemaURL     proto.ColStr
	scopeName             proto.ColStr
	scopeVersion          proto.ColStr
	scopeAttributes       *proto.ColMap[string, string]
	scopeDroppedAttrCount proto.ColUInt32
	scopeSchemaURL        proto.ColStr
	serviceName           *LowCard[string]
	metricName            *LowCard[string]
	metricDescription     proto.ColStr
	metricUnit            proto.ColStr
	attributes            *proto.ColMap[string, string]
	startTimeUnix         proto.ColDateTime
	timeUnix              proto.ColDateTime

	Names []string
	Cols  []proto.Column

	cachedResults proto.Results
	cachedInput   proto.Input
}

func newMetricsColumnsBase() metricsColumnsBase {
	// Let the runtime figure out sizing (same approach as the other schemas)
	strSize := 0
	bSize := 0

	return metricsColumnsBase{
		resourceAttributes:    newColMapLowCardinalityStringString(strSize, bSize),
		resourceSchemaURL:     newColString(strSize, bSize),
		scopeName:             newColString(strSize, bSize),
		scopeVersion:          newColString(strSize, bSize),
		scopeAttributes:       newColMapLowCardinalityStringString(strSize, bSize),
		scopeDroppedAttrCount: make(proto.ColUInt32, 0, bSize),
		scopeSchemaURL:        newColString(strSize, bSize),
		serviceName:           newColLowCardinalityString(strSize, bSize),
		metricName:            newColLowCardinalityString(strSize, bSize),
		metricDescription:     newColString(strSize, bSize),
		metricUnit:            newColString(strSize, bSize),
		attributes:            newColMapLowCardinalityStringString(strSize, bSize),
		startTimeUnix:         newColDateTime(bSize),
		timeUnix:              newColDateTime(bSize),
	}
}

// initColumns builds the common name/column lists. It must be called on the
// base's final address (i.e. after embedding into the concrete struct) so the
// column pointers reference the embedded fields, not a temporary copy.
func (c *metricsColumnsBase) initColumns() {
	c.Names = []string{
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
	c.Cols = []proto.Column{
		c.resourceAttributes,
		&c.resourceSchemaURL,
		&c.scopeName,
		&c.scopeVersion,
		c.scopeAttributes,
		&c.scopeDroppedAttrCount,
		&c.scopeSchemaURL,
		c.serviceName,
		c.metricName,
		&c.metricDescription,
		&c.metricUnit,
		c.attributes,
		&c.startTimeUnix,
		&c.timeUnix,
	}
}

func (c *metricsColumnsBase) addColumn(name string, col proto.Column) {
	c.Names = append(c.Names, name)
	c.Cols = append(c.Cols, col)
}

func (c *metricsColumnsBase) finishCache() {
	c.cachedResults = make(proto.Results, len(c.Names))
	for i := range c.Names {
		c.cachedResults[i] = proto.ResultColumn{Name: c.Names[i], Data: c.Cols[i]}
	}
	c.cachedInput = make(proto.Input, len(c.Names))
	for i := range c.Names {
		c.cachedInput[i] = proto.InputColumn{Name: c.Names[i], Data: c.Cols[i]}
	}
}

func (c *metricsColumnsBase) Reset() {
	for _, col := range c.Cols {
		col.Reset()
	}
}

func (c *metricsColumnsBase) Results() proto.Results { return c.cachedResults }

func (c *metricsColumnsBase) Input() proto.Input { return c.cachedInput }

func (c *metricsColumnsBase) FirstTimestamp() time.Time {
	if len(c.timeUnix.Data) > 0 {
		return c.timeUnix.Data[0].Time()
	}

	return time.Time{}
}

func (c *metricsColumnsBase) LastTimestamp() time.Time {
	if len(c.timeUnix.Data) > 0 {
		return c.timeUnix.Data[len(c.timeUnix.Data)-1].Time()
	}

	return time.Time{}
}

// shiftTimes applies shift to TimeUnix and moves StartTimeUnix by the same
// per-row delta so the start-to-sample distance is preserved.
// Exemplar timestamps are not shifted (same treatment as profiles'
// TimestampsUnixNano).
func (c *metricsColumnsBase) shiftTimes(shift func(time.Time) time.Time) {
	for i := range c.timeUnix.Data {
		original := c.timeUnix.Data[i].Time()
		shifted := shift(original)
		c.timeUnix.Data[i] = proto.ToDateTime(shifted)

		if i < len(c.startTimeUnix.Data) {
			delta := shifted.Sub(original)
			c.startTimeUnix.Data[i] = proto.ToDateTime(c.startTimeUnix.Data[i].Time().Add(delta))
		}
	}
}

func (c *metricsColumnsBase) UpdateDate() {
	c.shiftTimes(ShiftDateToToday)
}

func (c *metricsColumnsBase) UpdateTimestampMinute() {
	c.shiftTimes(ShiftTimestampMinute)
}

func (c *metricsColumnsBase) ShiftTimestamp(snapshot ReplayTimeSnapshot) {
	c.shiftTimes(snapshot.ShiftTimestamp)
}

func (c *metricsColumnsBase) UpdateTimestampNow() {
	now := time.Now()
	c.shiftTimes(func(_ time.Time) time.Time { return now })
}

// MutateIDs is a no-op for metrics: rows carry no unique IDs that need
// de-duplication across disk replay loops.
func (c *metricsColumnsBase) MutateIDs(_ int) {}

// metricsExemplarColumns holds the flattened Nested Exemplars columns shared
// by the gauge, sum, histogram, and exponential histogram tables.
type metricsExemplarColumns struct {
	filteredAttributes *proto.ColArr[map[string]string]
	timeUnix           *proto.ColArr[time.Time]
	value              *proto.ColArr[float64]
	spanID             *proto.ColArr[string]
	traceID            *proto.ColArr[string]
}

func newMetricsExemplarColumns() metricsExemplarColumns {
	strSize := 0
	bSize := 0

	return metricsExemplarColumns{
		filteredAttributes: newColArrayMapLowCardinalityStringString(strSize, bSize),
		timeUnix:           newColArrayDateTime(bSize),
		value:              newColArrayFloat64(bSize),
		spanID:             newColArrayString(strSize, bSize),
		traceID:            newColArrayString(strSize, bSize),
	}
}

func (e *metricsExemplarColumns) addTo(c *metricsColumnsBase) {
	c.addColumn("Exemplars.FilteredAttributes", e.filteredAttributes)
	c.addColumn("Exemplars.TimeUnix", e.timeUnix)
	c.addColumn("Exemplars.Value", e.value)
	c.addColumn("Exemplars.SpanId", e.spanID)
	c.addColumn("Exemplars.TraceId", e.traceID)
}

// =============================================================================
// Gauge
// =============================================================================

type MetricsGaugeSharedColumns struct {
	metricsColumnsBase

	value     proto.ColFloat64
	flags     proto.ColUInt32
	exemplars metricsExemplarColumns
}

func NewMetricsGaugeSharedColumns() *MetricsGaugeSharedColumns {
	bSize := 0

	c := MetricsGaugeSharedColumns{
		metricsColumnsBase: newMetricsColumnsBase(),
		value:              make(proto.ColFloat64, 0, bSize),
		flags:              make(proto.ColUInt32, 0, bSize),
		exemplars:          newMetricsExemplarColumns(),
	}

	c.initColumns()
	c.addColumn("Value", &c.value)
	c.addColumn("Flags", &c.flags)
	c.exemplars.addTo(&c.metricsColumnsBase)
	c.finishCache()

	return &c
}

// =============================================================================
// Sum
// =============================================================================

type MetricsSumSharedColumns struct {
	metricsColumnsBase

	value                  proto.ColFloat64
	flags                  proto.ColUInt32
	exemplars              metricsExemplarColumns
	aggregationTemporality proto.ColInt32
	isMonotonic            proto.ColBool
}

func NewMetricsSumSharedColumns() *MetricsSumSharedColumns {
	bSize := 0

	c := MetricsSumSharedColumns{
		metricsColumnsBase:     newMetricsColumnsBase(),
		value:                  make(proto.ColFloat64, 0, bSize),
		flags:                  make(proto.ColUInt32, 0, bSize),
		exemplars:              newMetricsExemplarColumns(),
		aggregationTemporality: make(proto.ColInt32, 0, bSize),
		isMonotonic:            make(proto.ColBool, 0, bSize),
	}

	c.initColumns()
	c.addColumn("Value", &c.value)
	c.addColumn("Flags", &c.flags)
	c.exemplars.addTo(&c.metricsColumnsBase)
	c.addColumn("AggregationTemporality", &c.aggregationTemporality)
	c.addColumn("IsMonotonic", &c.isMonotonic)
	c.finishCache()

	return &c
}

// =============================================================================
// Histogram
// =============================================================================

type MetricsHistogramSharedColumns struct {
	metricsColumnsBase

	count                  proto.ColUInt64
	sum                    proto.ColFloat64
	bucketCounts           *proto.ColArr[uint64]
	explicitBounds         *proto.ColArr[float64]
	exemplars              metricsExemplarColumns
	flags                  proto.ColUInt32
	min                    proto.ColFloat64
	max                    proto.ColFloat64
	aggregationTemporality proto.ColInt32
}

func NewMetricsHistogramSharedColumns() *MetricsHistogramSharedColumns {
	bSize := 0

	c := MetricsHistogramSharedColumns{
		metricsColumnsBase:     newMetricsColumnsBase(),
		count:                  make(proto.ColUInt64, 0, bSize),
		sum:                    make(proto.ColFloat64, 0, bSize),
		bucketCounts:           newColArrayUInt64(bSize),
		explicitBounds:         newColArrayFloat64(bSize),
		exemplars:              newMetricsExemplarColumns(),
		flags:                  make(proto.ColUInt32, 0, bSize),
		min:                    make(proto.ColFloat64, 0, bSize),
		max:                    make(proto.ColFloat64, 0, bSize),
		aggregationTemporality: make(proto.ColInt32, 0, bSize),
	}

	c.initColumns()
	c.addColumn("Count", &c.count)
	c.addColumn("Sum", &c.sum)
	c.addColumn("BucketCounts", c.bucketCounts)
	c.addColumn("ExplicitBounds", c.explicitBounds)
	c.exemplars.addTo(&c.metricsColumnsBase)
	c.addColumn("Flags", &c.flags)
	c.addColumn("Min", &c.min)
	c.addColumn("Max", &c.max)
	c.addColumn("AggregationTemporality", &c.aggregationTemporality)
	c.finishCache()

	return &c
}

// =============================================================================
// Exponential histogram
// =============================================================================

type MetricsExpHistogramSharedColumns struct {
	metricsColumnsBase

	count                  proto.ColUInt64
	sum                    proto.ColFloat64
	scale                  proto.ColInt32
	zeroCount              proto.ColUInt64
	positiveOffset         proto.ColInt32
	positiveBucketCounts   *proto.ColArr[uint64]
	negativeOffset         proto.ColInt32
	negativeBucketCounts   *proto.ColArr[uint64]
	exemplars              metricsExemplarColumns
	flags                  proto.ColUInt32
	min                    proto.ColFloat64
	max                    proto.ColFloat64
	aggregationTemporality proto.ColInt32
}

func NewMetricsExpHistogramSharedColumns() *MetricsExpHistogramSharedColumns {
	bSize := 0

	c := MetricsExpHistogramSharedColumns{
		metricsColumnsBase:     newMetricsColumnsBase(),
		count:                  make(proto.ColUInt64, 0, bSize),
		sum:                    make(proto.ColFloat64, 0, bSize),
		scale:                  make(proto.ColInt32, 0, bSize),
		zeroCount:              make(proto.ColUInt64, 0, bSize),
		positiveOffset:         make(proto.ColInt32, 0, bSize),
		positiveBucketCounts:   newColArrayUInt64(bSize),
		negativeOffset:         make(proto.ColInt32, 0, bSize),
		negativeBucketCounts:   newColArrayUInt64(bSize),
		exemplars:              newMetricsExemplarColumns(),
		flags:                  make(proto.ColUInt32, 0, bSize),
		min:                    make(proto.ColFloat64, 0, bSize),
		max:                    make(proto.ColFloat64, 0, bSize),
		aggregationTemporality: make(proto.ColInt32, 0, bSize),
	}

	c.initColumns()
	c.addColumn("Count", &c.count)
	c.addColumn("Sum", &c.sum)
	c.addColumn("Scale", &c.scale)
	c.addColumn("ZeroCount", &c.zeroCount)
	c.addColumn("PositiveOffset", &c.positiveOffset)
	c.addColumn("PositiveBucketCounts", c.positiveBucketCounts)
	c.addColumn("NegativeOffset", &c.negativeOffset)
	c.addColumn("NegativeBucketCounts", c.negativeBucketCounts)
	c.exemplars.addTo(&c.metricsColumnsBase)
	c.addColumn("Flags", &c.flags)
	c.addColumn("Min", &c.min)
	c.addColumn("Max", &c.max)
	c.addColumn("AggregationTemporality", &c.aggregationTemporality)
	c.finishCache()

	return &c
}

// =============================================================================
// Summary
// =============================================================================

type MetricsSummarySharedColumns struct {
	metricsColumnsBase

	count            proto.ColUInt64
	sum              proto.ColFloat64
	quantileValues   *proto.ColArr[float64]
	quantileQuantile *proto.ColArr[float64]
	flags            proto.ColUInt32
}

func NewMetricsSummarySharedColumns() *MetricsSummarySharedColumns {
	bSize := 0

	c := MetricsSummarySharedColumns{
		metricsColumnsBase: newMetricsColumnsBase(),
		count:              make(proto.ColUInt64, 0, bSize),
		sum:                make(proto.ColFloat64, 0, bSize),
		quantileQuantile:   newColArrayFloat64(bSize),
		quantileValues:     newColArrayFloat64(bSize),
		flags:              make(proto.ColUInt32, 0, bSize),
	}

	c.initColumns()
	c.addColumn("Count", &c.count)
	c.addColumn("Sum", &c.sum)
	c.addColumn("ValueAtQuantiles.Quantile", c.quantileQuantile)
	c.addColumn("ValueAtQuantiles.Value", c.quantileValues)
	c.addColumn("Flags", &c.flags)
	c.finishCache()

	return &c
}

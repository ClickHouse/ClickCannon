package generate

import "clickcannon/internal/block"

// This file implements block.LogsReader / block.TracesReader for the generate
// column types so the OTLP exporter can read generated rows. The generate path
// uses standard proto columns (working Row methods), so the shared block helpers
// handle both key-column variants transparently.

// Rows implements block.LogsReader.
func (c *GenLogsColumns) Rows() int { return len(c.Timestamp.Data) }

// ReadLogRow implements block.LogsReader for the generate path.
func (c *GenLogsColumns) ReadLogRow(i int, dst *block.LogRow) {
	dst.Timestamp = c.Timestamp.Data[i].Time(c.Timestamp.Precision)
	dst.TraceID = c.TraceID.Row(i)
	dst.SpanID = c.SpanID.Row(i)
	dst.TraceFlags = uint32(c.TraceFlags[i])
	dst.SeverityText = c.SeverityText.Row(i)
	dst.SeverityNumber = int32(c.SeverityNumber[i])
	dst.ServiceName = c.ServiceName.Row(i)
	dst.Body = c.Body.Row(i)
	dst.ResourceSchemaURL = c.ResourceSchemaUrl.Row(i)
	dst.ScopeSchemaURL = c.ScopeSchemaUrl.Row(i)
	dst.ScopeName = c.ScopeName.Row(i)
	dst.ScopeVersion = c.ScopeVersion.Row(i)
	dst.ResourceAttrs = block.MapRowKV(dst.ResourceAttrs[:0], c.ResourceAttributes, i)
	dst.ScopeAttrs = block.MapRowKV(dst.ScopeAttrs[:0], c.ScopeAttributes, i)
	dst.LogAttrs = block.MapRowKV(dst.LogAttrs[:0], c.LogAttributes, i)
}

// Rows implements block.MetricsReader for all five generate metrics column sets.
func (c *genMetricsColumnsBase) Rows() int { return len(c.TimeUnix.Data) }

// readMetricCommon fills the columns shared by all five metrics tables.
func (c *genMetricsColumnsBase) readMetricCommon(i int, dst *block.MetricRow) {
	dst.ResourceAttrs = block.MapRowKV(dst.ResourceAttrs[:0], c.ResourceAttributes, i)
	dst.ResourceSchemaURL = c.ResourceSchemaUrl.Row(i)
	dst.ScopeName = c.ScopeName.Row(i)
	dst.ScopeVersion = c.ScopeVersion.Row(i)
	dst.ScopeAttrs = block.MapRowKV(dst.ScopeAttrs[:0], c.ScopeAttributes, i)
	dst.ScopeDroppedAttrCount = c.ScopeDroppedAttrCount[i]
	dst.ScopeSchemaURL = c.ScopeSchemaUrl.Row(i)
	dst.ServiceName = c.ServiceName.Row(i)
	dst.MetricName = c.MetricName.Row(i)
	dst.MetricDescription = c.MetricDescription.Row(i)
	dst.MetricUnit = c.MetricUnit.Row(i)
	dst.Attrs = block.MapRowKV(dst.Attrs[:0], c.Attributes, i)
	dst.StartTime = c.StartTimeUnix.Data[i].Time()
	dst.Time = c.TimeUnix.Data[i].Time()
}

// ReadMetricRow implements block.MetricsReader for the generate path.
func (c *GenMetricsGaugeColumns) ReadMetricRow(i int, dst *block.MetricRow) {
	dst.Type = block.MetricTypeGauge
	c.readMetricCommon(i, dst)
	dst.Value = c.Value[i]
	dst.Flags = c.Flags[i]
}

// ReadMetricRow implements block.MetricsReader for the generate path.
func (c *GenMetricsSumColumns) ReadMetricRow(i int, dst *block.MetricRow) {
	dst.Type = block.MetricTypeSum
	c.readMetricCommon(i, dst)
	dst.Value = c.Value[i]
	dst.Flags = c.Flags[i]
	dst.AggregationTemporality = c.AggregationTemporality[i]
	dst.IsMonotonic = c.IsMonotonic[i]
}

// ReadMetricRow implements block.MetricsReader for the generate path.
func (c *GenMetricsHistogramColumns) ReadMetricRow(i int, dst *block.MetricRow) {
	dst.Type = block.MetricTypeHistogram
	c.readMetricCommon(i, dst)
	dst.Count = c.Count[i]
	dst.Sum = c.Sum[i]
	dst.BucketCounts = c.BucketCounts.RowAppend(i, dst.BucketCounts[:0])
	dst.ExplicitBounds = c.ExplicitBounds.RowAppend(i, dst.ExplicitBounds[:0])
	dst.Flags = c.Flags[i]
	dst.Min = c.Min[i]
	dst.Max = c.Max[i]
	dst.AggregationTemporality = c.AggregationTemporality[i]
}

// ReadMetricRow implements block.MetricsReader for the generate path.
func (c *GenMetricsExpHistogramColumns) ReadMetricRow(i int, dst *block.MetricRow) {
	dst.Type = block.MetricTypeExpHistogram
	c.readMetricCommon(i, dst)
	dst.Count = c.Count[i]
	dst.Sum = c.Sum[i]
	dst.Scale = c.Scale[i]
	dst.ZeroCount = c.ZeroCount[i]
	dst.PositiveOffset = c.PositiveOffset[i]
	dst.PositiveBucketCounts = c.PositiveBucketCounts.RowAppend(i, dst.PositiveBucketCounts[:0])
	dst.NegativeOffset = c.NegativeOffset[i]
	dst.NegativeBucketCounts = c.NegativeBucketCounts.RowAppend(i, dst.NegativeBucketCounts[:0])
	dst.Flags = c.Flags[i]
	dst.Min = c.Min[i]
	dst.Max = c.Max[i]
	dst.AggregationTemporality = c.AggregationTemporality[i]
}

// ReadMetricRow implements block.MetricsReader for the generate path.
func (c *GenMetricsSummaryColumns) ReadMetricRow(i int, dst *block.MetricRow) {
	dst.Type = block.MetricTypeSummary
	c.readMetricCommon(i, dst)
	dst.Count = c.Count[i]
	dst.Sum = c.Sum[i]
	dst.QuantileQuantiles = c.QuantileQuantile.RowAppend(i, dst.QuantileQuantiles[:0])
	dst.QuantileValues = c.QuantileValue.RowAppend(i, dst.QuantileValues[:0])
	dst.Flags = c.Flags[i]
}

// Rows implements block.TracesReader.
func (c *GenTracesColumns) Rows() int { return len(c.Timestamp.Data) }

// ReadTraceRow implements block.TracesReader for the generate path.
func (c *GenTracesColumns) ReadTraceRow(i int, dst *block.TraceRow) {
	dst.Timestamp = c.Timestamp.Data[i].Time(c.Timestamp.Precision)
	dst.TraceID = c.TraceID.Row(i)
	dst.SpanID = c.SpanID.Row(i)
	dst.ParentSpanID = c.ParentSpanID.Row(i)
	dst.TraceState = c.TraceState.Row(i)
	dst.SpanName = c.SpanName.Row(i)
	dst.SpanKind = c.SpanKind.Row(i)
	dst.ServiceName = c.ServiceName.Row(i)
	dst.ScopeName = c.ScopeName.Row(i)
	dst.ScopeVersion = c.ScopeVersion.Row(i)
	dst.Duration = c.Duration[i]
	dst.StatusCode = c.StatusCode.Row(i)
	dst.StatusMessage = c.StatusMessage.Row(i)
	dst.ResourceAttrs = block.MapRowKV(dst.ResourceAttrs[:0], c.ResourceAttributes, i)
	dst.SpanAttrs = block.MapRowKV(dst.SpanAttrs[:0], c.SpanAttributes, i)
	dst.Events = block.ReadEvents(dst.Events[:0], c.EventsTimestamps, c.EventsNames, c.EventsAttributes, i)
	dst.Links = block.ReadLinks(dst.Links[:0], c.LinksTraceIDs, c.LinksSpanIDs, c.LinksTraceStates, c.LinksAttributes, i)
}

package otel

import (
	"clickcannon/internal/block"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// tracesGroupKey identifies one (resource, scope) group: scalar folds the
// service/scope identity fields in fixed order, res is the order-insensitive
// resource attribute fingerprint (kvSetHash, convert.go). Both fields must
// match, so a collision in one alone can't merge two different groups.
type tracesGroupKey struct {
	scalar uint64
	res    kvSetHash
}

// tracesBuilder groups spans by (resource, scope) fingerprint into OTLP
// ResourceSpans.
type tracesBuilder struct {
	groups map[tracesGroupKey]*tracepb.ResourceSpans
	order  []*tracepb.ResourceSpans
	count  int
}

func newTracesBuilder() *tracesBuilder {
	return &tracesBuilder{groups: make(map[tracesGroupKey]*tracepb.ResourceSpans)}
}

func (b *tracesBuilder) len() int { return b.count }

func (b *tracesBuilder) add(r *block.TraceRow) {
	// See logsBuilder.add for the delimiter rationale. Resource attributes are
	// hashed order-insensitively (kvSetHash, convert.go) because all spans of a
	// trace carry the same attribute set in a random per-span order.
	scalar := fnvStr(fnvOffset64, r.ServiceName)
	scalar = (scalar ^ 0x01) * fnvPrime64
	scalar = fnvStr(scalar, r.ScopeName)
	scalar = (scalar ^ 0x03) * fnvPrime64
	scalar = fnvStr(scalar, r.ScopeVersion)
	key := tracesGroupKey{scalar: scalar, res: hashKVSet(r.ResourceAttrs)}

	rs := b.groups[key]
	if rs == nil {
		rs = &tracepb.ResourceSpans{
			Resource: buildResource(r.ServiceName, r.ResourceAttrs),
			ScopeSpans: []*tracepb.ScopeSpans{{
				Scope: &commonpb.InstrumentationScope{Name: r.ScopeName, Version: r.ScopeVersion},
			}},
		}
		b.groups[key] = rs
		b.order = append(b.order, rs)
	}

	start := nano(r.Timestamp)
	span := &tracepb.Span{
		TraceId:           decodeID(r.TraceID),
		SpanId:            decodeID(r.SpanID),
		TraceState:        r.TraceState,
		ParentSpanId:      decodeID(r.ParentSpanID),
		Name:              r.SpanName,
		Kind:              spanKind(r.SpanKind),
		StartTimeUnixNano: start,
		EndTimeUnixNano:   start + r.Duration,
		Attributes:        attrsFromKV(r.SpanAttrs),
		Status:            &tracepb.Status{Code: statusCode(r.StatusCode), Message: r.StatusMessage},
	}

	if len(r.Events) > 0 {
		span.Events = make([]*tracepb.Span_Event, 0, len(r.Events))
		for i := range r.Events {
			e := &r.Events[i]
			span.Events = append(span.Events, &tracepb.Span_Event{
				TimeUnixNano: nano(e.Timestamp),
				Name:         e.Name,
				Attributes:   attrsFromKV(e.Attrs),
			})
		}
	}
	if len(r.Links) > 0 {
		span.Links = make([]*tracepb.Span_Link, 0, len(r.Links))
		for i := range r.Links {
			l := &r.Links[i]
			span.Links = append(span.Links, &tracepb.Span_Link{
				TraceId:    decodeID(l.TraceID),
				SpanId:     decodeID(l.SpanID),
				TraceState: l.TraceState,
				Attributes: attrsFromKV(l.Attrs),
			})
		}
	}

	rs.ScopeSpans[0].Spans = append(rs.ScopeSpans[0].Spans, span)
	b.count++
}

func (b *tracesBuilder) build() *coltracepb.ExportTraceServiceRequest {
	return &coltracepb.ExportTraceServiceRequest{ResourceSpans: b.order}
}

func (b *tracesBuilder) reset() {
	clear(b.groups)
	b.order = b.order[:0]
	b.count = 0
}

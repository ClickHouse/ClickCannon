package otel

import (
	"clickcannon/internal/block"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// logsGroupKey identifies one (resource, scope) group: scalar folds the
// string identity fields in fixed order, res and scope are order-insensitive
// attribute fingerprints (kvSetHash, convert.go). All fields must match, so a
// collision in one alone can't merge two different groups.
type logsGroupKey struct {
	scalar uint64
	res    kvSetHash
	scope  kvSetHash
}

// logsBuilder groups log records by (resource, scope) fingerprint into OTLP
// ResourceLogs so records sharing a resource+scope are emitted together.
type logsBuilder struct {
	groups map[logsGroupKey]*logspb.ResourceLogs
	order  []*logspb.ResourceLogs
	count  int
}

func newLogsBuilder() *logsBuilder {
	return &logsBuilder{groups: make(map[logsGroupKey]*logspb.ResourceLogs)}
}

func (b *logsBuilder) len() int { return b.count }

func (b *logsBuilder) add(r *block.LogRow) {
	// Fold a distinct non-printable delimiter after each scalar field so
	// adjacent values with ambiguous boundaries ("ab"+"c" vs "a"+"bc") cannot
	// collide; printable delimiters would collide with the same byte appearing
	// in the data. Attribute sets are hashed order-insensitively (kvSetHash,
	// convert.go) because identical attr maps arrive in a random per-record
	// order. A residual hash collision would merge two groups; that is an
	// accepted tradeoff for a load generator (affects only grouping metadata).
	scalar := fnvStr(fnvOffset64, r.ServiceName)
	scalar = (scalar ^ 0x01) * fnvPrime64
	scalar = fnvStr(scalar, r.ResourceSchemaURL)
	scalar = (scalar ^ 0x02) * fnvPrime64
	scalar = fnvStr(scalar, r.ScopeName)
	scalar = (scalar ^ 0x03) * fnvPrime64
	scalar = fnvStr(scalar, r.ScopeVersion)
	scalar = (scalar ^ 0x04) * fnvPrime64
	scalar = fnvStr(scalar, r.ScopeSchemaURL)
	key := logsGroupKey{
		scalar: scalar,
		res:    hashKVSet(r.ResourceAttrs),
		scope:  hashKVSet(r.ScopeAttrs),
	}

	rl := b.groups[key]
	if rl == nil {
		rl = &logspb.ResourceLogs{
			Resource:  buildResource(r.ServiceName, r.ResourceAttrs),
			SchemaUrl: r.ResourceSchemaURL,
			ScopeLogs: []*logspb.ScopeLogs{{
				Scope: &commonpb.InstrumentationScope{
					Name:       r.ScopeName,
					Version:    r.ScopeVersion,
					Attributes: attrsFromKV(r.ScopeAttrs),
				},
				SchemaUrl: r.ScopeSchemaURL,
			}},
		}
		b.groups[key] = rl
		b.order = append(b.order, rl)
	}

	ts := nano(r.Timestamp)
	rl.ScopeLogs[0].LogRecords = append(rl.ScopeLogs[0].LogRecords, &logspb.LogRecord{
		TimeUnixNano:         ts,
		ObservedTimeUnixNano: ts,
		SeverityNumber:       logspb.SeverityNumber(r.SeverityNumber),
		SeverityText:         r.SeverityText,
		Body:                 stringValue(r.Body),
		Attributes:           attrsFromKV(r.LogAttrs),
		Flags:                r.TraceFlags,
		TraceId:              decodeID(r.TraceID),
		SpanId:               decodeID(r.SpanID),
	})
	b.count++
}

func (b *logsBuilder) build() *collogspb.ExportLogsServiceRequest {
	return &collogspb.ExportLogsServiceRequest{ResourceLogs: b.order}
}

func (b *logsBuilder) reset() {
	clear(b.groups)
	b.order = b.order[:0]
	b.count = 0
}

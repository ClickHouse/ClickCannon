package otel

import (
	"encoding/hex"
	"time"

	"clickcannon/internal/block"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// This file converts neutral block rows into OTLP protobuf messages. It is a
// straight passthrough: column values map directly onto OTLP fields with no
// enrichment, resource detection, or SDK involvement.

const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211
)

// fnvStr folds a string into an FNV-1a running hash (no allocation).
func fnvStr(h uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= fnvPrime64
	}
	return h
}

// fnvKVs folds key/value pairs into the running hash, in stored order. Rows that
// share identical attributes (in the same column order) produce identical hashes,
// which is how records are grouped into a single Resource/Scope.
//
// STILL USED BY THE LOGS BUILDER (convert_logs.go). The TRACES builder uses the
// order-INSENSITIVE hashKVSet below instead: see its comment for why.
func fnvKVs(h uint64, kvs []block.KV) uint64 {
	for _, kv := range kvs {
		h = fnvStr(h, kv.Key)
		h = (h ^ 0x1f) * fnvPrime64
		h = fnvStr(h, kv.Value)
		h = (h ^ 0x1e) * fnvPrime64
	}
	return h
}

// fnvKVPair hashes ONE key/value pair to a standalone digest. The delimiters are
// the same ones fnvKVs uses, so a given pair contributes the same bits in both
// schemes; only the way pairs are COMBINED differs.
func fnvKVPair(kv block.KV) uint64 {
	h := fnvStr(fnvOffset64, kv.Key)
	h = (h ^ 0x1f) * fnvPrime64
	h = fnvStr(h, kv.Value)
	return (h ^ 0x1e) * fnvPrime64
}

// mix64 is splitmix64's finaliser, used as a second accumulator independent of xor.
func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

// kvSetHash is an order-insensitive fingerprint of a kv set: xor, a
// sum-of-mixed accumulator, and the pair count are compared together so
// reordered attrs hash equal while collisions stay unlikely. Needed because Go
// map iteration order is random, and an order-sensitive hash was splitting one
// resource into per-span groups (~4x wire inflation).
type kvSetHash struct {
	xor uint64
	sum uint64
	n   uint32
}

// hashKVSet computes the order-insensitive fingerprint of kvs. No allocation.
func hashKVSet(kvs []block.KV) kvSetHash {
	var out kvSetHash
	for _, kv := range kvs {
		h := fnvKVPair(kv)
		out.xor ^= h
		out.sum += mix64(h)
		out.n++
	}
	return out
}

func stringValue(s string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
}

// attrsFromKV converts neutral KV pairs to OTLP KeyValue attributes.
func attrsFromKV(kvs []block.KV) []*commonpb.KeyValue {
	if len(kvs) == 0 {
		return nil
	}
	out := make([]*commonpb.KeyValue, 0, len(kvs))
	for _, kv := range kvs {
		out = append(out, &commonpb.KeyValue{Key: kv.Key, Value: stringValue(kv.Value)})
	}
	return out
}

// buildResource builds an OTLP Resource. service.name is stored in a dedicated
// column, so it is added explicitly unless the attribute set already carries it.
func buildResource(serviceName string, attrs []block.KV) *resourcepb.Resource {
	out := make([]*commonpb.KeyValue, 0, len(attrs)+1)
	hasService := false
	for _, kv := range attrs {
		if kv.Key == "service.name" {
			hasService = true
		}
		out = append(out, &commonpb.KeyValue{Key: kv.Key, Value: stringValue(kv.Value)})
	}
	if !hasService && serviceName != "" {
		out = append(out, &commonpb.KeyValue{Key: "service.name", Value: stringValue(serviceName)})
	}
	return &resourcepb.Resource{Attributes: out}
}

// decodeID hex-decodes an ID into raw OTLP bytes. Non-hex input falls back to the
// raw bytes of the string so nothing is silently dropped.
func decodeID(s string) []byte {
	if s == "" {
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return []byte(s)
	}
	return b
}

func spanKind(s string) tracepb.Span_SpanKind {
	switch s {
	case "SPAN_KIND_INTERNAL", "Internal", "internal", "INTERNAL":
		return tracepb.Span_SPAN_KIND_INTERNAL
	case "SPAN_KIND_SERVER", "Server", "server", "SERVER":
		return tracepb.Span_SPAN_KIND_SERVER
	case "SPAN_KIND_CLIENT", "Client", "client", "CLIENT":
		return tracepb.Span_SPAN_KIND_CLIENT
	case "SPAN_KIND_PRODUCER", "Producer", "producer", "PRODUCER":
		return tracepb.Span_SPAN_KIND_PRODUCER
	case "SPAN_KIND_CONSUMER", "Consumer", "consumer", "CONSUMER":
		return tracepb.Span_SPAN_KIND_CONSUMER
	default:
		return tracepb.Span_SPAN_KIND_UNSPECIFIED
	}
}

func statusCode(s string) tracepb.Status_StatusCode {
	switch s {
	case "STATUS_CODE_OK", "Ok", "OK", "ok":
		return tracepb.Status_STATUS_CODE_OK
	case "STATUS_CODE_ERROR", "Error", "ERROR", "error":
		return tracepb.Status_STATUS_CODE_ERROR
	default:
		return tracepb.Status_STATUS_CODE_UNSET
	}
}

// nano returns the unix-nanosecond timestamp, guarding the zero time (whose
// UnixNano would be a large negative value).
func nano(t time.Time) uint64 {
	if t.IsZero() {
		return 0
	}
	return uint64(t.UnixNano())
}

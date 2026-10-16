package otlpsize

import (
	"log/slog"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// AttrSize is the uncompressed OTLP protobuf size in bytes of the attributes of one level.
type AttrSize struct {
	Expected int
	Min      int
	Max      int
}

// Breakdown is the attribute size per resource, scope and signal level of one signal.
type Breakdown struct {
	Profile       string
	Signal        string
	ResourceAttrs AttrSize
	ScopeAttrs    AttrSize
	SignalAttrs   AttrSize
}

func (bd Breakdown) Attrs() []slog.Attr {
	return []slog.Attr{
		slog.String("profile", bd.Profile),
		slog.String("signal", bd.Signal),
		slog.Int("resource_attr_expected_bytes", bd.ResourceAttrs.Expected),
		slog.Int("scope_attr_expected_bytes", bd.ScopeAttrs.Expected),
		slog.Int("signal_attr_expected_bytes", bd.SignalAttrs.Expected),
		slog.Int("attr_expected_bytes", bd.ResourceAttrs.Expected+bd.ScopeAttrs.Expected+bd.SignalAttrs.Expected),
		slog.Int("resource_attr_min_bytes", bd.ResourceAttrs.Min),
		slog.Int("resource_attr_max_bytes", bd.ResourceAttrs.Max),
		slog.Int("scope_attr_min_bytes", bd.ScopeAttrs.Min),
		slog.Int("scope_attr_max_bytes", bd.ScopeAttrs.Max),
		slog.Int("signal_attr_min_bytes", bd.SignalAttrs.Min),
		slog.Int("signal_attr_max_bytes", bd.SignalAttrs.Max),
	}
}

// AttrBytes returns the encoded size of attrs in their parent message, including each field tag and length prefix.
// The attributes field number is below 16 in every OTLP message, so each tag is one byte.
func AttrBytes(attrs ...*commonpb.KeyValue) int {
	n := 0
	for _, kv := range attrs {
		n += protowire.SizeTag(1) + protowire.SizeBytes(proto.Size(kv))
	}
	return n
}

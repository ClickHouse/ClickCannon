package otlpsize

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

func TestAttrBytesMatchesEncodedParent(t *testing.T) {
	attrs := []*commonpb.KeyValue{
		{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "api"}}},
		{Key: "k8s.pod.uid", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: string(make([]byte, 200))}}},
	}
	if got, want := AttrBytes(attrs...), proto.Size(&resourcepb.Resource{Attributes: attrs}); got != want {
		t.Fatalf("AttrBytes = %d, want %d", got, want)
	}
}

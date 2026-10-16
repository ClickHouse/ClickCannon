package otel

import (
	"context"
	"testing"

	"clickcannon/internal/block"
	"clickcannon/internal/generate"
	"clickcannon/internal/otlpsize"

	"google.golang.org/protobuf/proto"
)

func TestSyntheticSize(t *testing.T) {
	for _, dataType := range []string{"logs", "traces"} {
		t.Run(dataType, func(t *testing.T) {
			bd, err := SyntheticSize(dataType, generate.Config{}, "otlp-size")
			if err != nil {
				t.Fatal(err)
			}
			for _, size := range []otlpsize.AttrSize{bd.ResourceAttrs, bd.ScopeAttrs, bd.SignalAttrs} {
				if size.Min > size.Expected || size.Expected > size.Max {
					t.Errorf("attr size %+v: want min <= expected <= max", size)
				}
			}
		})
	}
}

func TestSyntheticSizeContainsGeneratedResource(t *testing.T) {
	bd, err := SyntheticSize("logs", generate.Config{}, "otlp-size")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := generate.GetProfile(generate.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	cols := generate.NewGenLogsColumns()
	generate.NewLogsFiller(profile).Fill(context.Background(), generate.NewRng("otlp-size", 0), cols, 1)
	var row block.LogRow
	cols.ReadLogRow(0, &row)

	got := proto.Size(buildResource(row.ServiceName, row.ResourceAttrs))
	if got < bd.ResourceAttrs.Min || got > bd.ResourceAttrs.Max {
		t.Fatalf("generated resource bytes = %d, want within %+v", got, bd.ResourceAttrs)
	}
}

func TestSyntheticSizeUnsupportedDataType(t *testing.T) {
	if _, err := SyntheticSize("profiles", generate.Config{}, "otlp-size"); err == nil {
		t.Fatal("want error for data_type profiles")
	}
}

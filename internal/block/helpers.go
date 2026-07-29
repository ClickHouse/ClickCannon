package block

import (
	"time"

	"github.com/ClickHouse/ch-go/proto"
)

// LowCard is a custom LowCardinality wrapper type.
// The standard LowCardinality[T] is broken in ch-go, something isn't being reset properly
// when I use it with reusable blocks. This wrapper type uses the Raw version, and since we only use the string
// implementation, it works for now. It's actually magic; this took forever to debug.
type LowCard[T comparable] struct {
	proto.ColLowCardinalityRaw
}

func (c *LowCard[T]) Append(v T) {
	panic("custom clickcannon LowCardinality type is missing Append implementation")
}

func (c *LowCard[T]) AppendArr(v []T) {
	panic("custom clickcannon LowCardinality type is missing AppendArr implementation")
}

func (c *LowCard[T]) Row(i int) T {
	panic("custom clickcannon LowCardinality type is missing Row implementation")
}

// If the above magic wrapper type fails, you can fall back to this but be sure to set reuse_blocks to false in config.
//func newColLowCardinalityString(strSize, batchSize int) *proto.ColLowCardinality[string] {
//	lc := proto.NewLowCardinality[string](&proto.ColStr{
//		Buf: make([]byte, 0, strSize*batchSize),
//		Pos: make([]proto.Position, 0, batchSize),
//	})
//	lc.Values = make([]string, 0, batchSize)
//
//	return lc
//}

func newColLowCardinalityString(strSize, batchSize int) *LowCard[string] {
	lc := LowCard[string]{
		ColLowCardinalityRaw: proto.ColLowCardinalityRaw{
			Index: &proto.ColStr{
				Buf: make([]byte, 0, strSize*batchSize),
				Pos: make([]proto.Position, 0, batchSize),
			},
			Key: proto.KeyUInt8,
		},
	}

	return &lc
}

func newColMapLowCardinalityStringString(strSize, batchSize int) *proto.ColMap[string, string] {
	lc := newColLowCardinalityString(strSize, batchSize)
	s := newColString(strSize, batchSize)

	m := proto.NewMap[string, string](lc, &s)

	return m
}

func newColArrayLowCardinalityString(strSize, batchSize int) *proto.ColArr[string] {
	col := newColLowCardinalityString(strSize, batchSize)
	return &proto.ColArr[string]{
		Offsets: make(proto.ColUInt64, 0, batchSize),
		Data:    col,
	}
}

func newColArrayMapLowCardinalityStringString(strSize, batchSize int) *proto.ColArr[map[string]string] {
	col := newColMapLowCardinalityStringString(strSize, batchSize)
	return &proto.ColArr[map[string]string]{
		Offsets: make(proto.ColUInt64, 0, batchSize),
		Data:    col,
	}
}

func newColString(strSize, batchSize int) proto.ColStr {
	return proto.ColStr{
		Buf: make([]byte, 0, strSize*batchSize),
		Pos: make([]proto.Position, 0, batchSize),
	}
}

func newColArrayString(strSize, batchSize int) *proto.ColArr[string] {
	col := newColString(strSize, batchSize)
	return &proto.ColArr[string]{
		Offsets: make(proto.ColUInt64, 0, batchSize),
		Data:    &col,
	}
}

func newColDateTime64Raw(batchSize int) proto.ColDateTime64Raw {
	return proto.ColDateTime64Raw{
		ColDateTime64: proto.ColDateTime64{
			Data:         make([]proto.DateTime64, 0, batchSize),
			Location:     time.UTC,
			Precision:    proto.PrecisionNano,
			PrecisionSet: true,
		},
	}
}

func newColDateTime(batchSize int) proto.ColDateTime {
	return proto.ColDateTime{
		Data:     make([]proto.DateTime, 0, batchSize),
		Location: time.UTC,
	}
}

func newColArrayDateTime64Raw(batchSize int) *proto.ColArr[proto.DateTime64] {
	col := newColDateTime64Raw(batchSize)
	return &proto.ColArr[proto.DateTime64]{
		Offsets: make(proto.ColUInt64, 0, batchSize),
		Data:    &col,
	}
}

func newColArrayDateTime(batchSize int) *proto.ColArr[time.Time] {
	col := newColDateTime(batchSize)
	return &proto.ColArr[time.Time]{
		Offsets: make(proto.ColUInt64, 0, batchSize),
		Data:    &col,
	}
}

func newColArrayFloat64(batchSize int) *proto.ColArr[float64] {
	col := make(proto.ColFloat64, 0, batchSize)
	return &proto.ColArr[float64]{
		Offsets: make(proto.ColUInt64, 0, batchSize),
		Data:    &col,
	}
}

func newColArrayUInt64(batchSize int) *proto.ColArr[uint64] {
	col := make(proto.ColUInt64, 0, batchSize)
	return &proto.ColArr[uint64]{
		Offsets: make(proto.ColUInt64, 0, batchSize),
		Data:    &col,
	}
}

func newColArrayInt32(batchSize int) *proto.ColArr[int32] {
	col := make(proto.ColInt32, 0, batchSize)
	return &proto.ColArr[int32]{
		Offsets: make(proto.ColUInt64, 0, batchSize),
		Data:    &col,
	}
}

func newColArrayInt64(batchSize int) *proto.ColArr[int64] {
	col := make(proto.ColInt64, 0, batchSize)
	return &proto.ColArr[int64]{
		Offsets: make(proto.ColUInt64, 0, batchSize),
		Data:    &col,
	}
}

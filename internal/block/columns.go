package block

import (
	"fmt"
	"time"

	"github.com/ClickHouse/ch-go/proto"
)

type SharedColumns interface {
	// Reset underlying data
	Reset()
	// Results structure for block decoding
	Results() proto.Results
	// Input structure for block inserting
	Input() proto.Input

	// FirstTimestamp Returns the first timestamp in the block
	FirstTimestamp() time.Time
	// LastTimestamp Returns the last timestamp in the block
	LastTimestamp() time.Time

	// UpdateDate for shifting the date component on old data to today
	UpdateDate()
	// ShiftTimestamp applies a replay time snapshot for shifting the timestamp
	ShiftTimestamp(snapshot ReplayTimeSnapshot)
	// UpdateTimestampNow for shifting the timestamp to be the current time
	UpdateTimestampNow()
	UpdateTimestampMinute()

	// MutateIDs applies a deterministic mutation to ID columns (e.g. TraceID, SpanID)
	// based on the loop index, so that replayed data from different loop iterations
	// can be distinguished. No-op when loopIndex == 0.
	MutateIDs(loopIndex int)
}

type DynamicSharedColumns struct {
	results          proto.Results
	input            proto.Input
	cols             []proto.Column
	timestampIndices []int // 跟踪哪些列索引是DateTime/DateTime64类型
}

func NewDynamicSharedColumns() *DynamicSharedColumns {
	return &DynamicSharedColumns{}
}

func (c *DynamicSharedColumns) Reset() {
	for _, col := range c.cols {
		col.Reset()
	}
}

func (c *DynamicSharedColumns) Results() proto.Results { return c.results }

func (c *DynamicSharedColumns) Input() proto.Input { return c.input }

func (c *DynamicSharedColumns) FirstTimestamp() time.Time {
	if len(c.timestampIndices) == 0 {
		return time.Time{}
	}
	idx := c.timestampIndices[0]
	col := c.cols[idx]
	switch dt := col.(type) {
	case *proto.ColDateTime64Raw:
		if len(dt.Data) > 0 {
			return dt.Data[0].Time(dt.Precision)
		}
	case *proto.ColDateTime:
		if len(dt.Data) > 0 {
			return dt.Data[0].Time()
		}
	}
	return time.Time{}
}

func (c *DynamicSharedColumns) LastTimestamp() time.Time {
	if len(c.timestampIndices) == 0 {
		return time.Time{}
	}
	idx := c.timestampIndices[0]
	col := c.cols[idx]
	switch dt := col.(type) {
	case *proto.ColDateTime64Raw:
		if len(dt.Data) > 0 {
			return dt.Data[len(dt.Data)-1].Time(dt.Precision)
		}
	case *proto.ColDateTime:
		if len(dt.Data) > 0 {
			return dt.Data[len(dt.Data)-1].Time()
		}
	}
	return time.Time{}
}

func (c *DynamicSharedColumns) UpdateDate() {
	for _, idx := range c.timestampIndices {
		col := c.cols[idx]
		switch dt := col.(type) {
		case *proto.ColDateTime64Raw:
			// 将日期部分移到今天，保留时间部分
			// 使用列的Location确保时区一致性
			loc := dt.Location
			for i := range dt.Data {
				original := dt.Data[i].Time(dt.Precision)
				// 将original转换到列的时区以获取正确的时分秒
				originalInColLoc := original.In(loc)
				// 获取当前日期（在列的时区中）
				now := time.Now().In(loc)
				// 创建新时间：今天的日期 + 原始的时分秒（在列的时区中）
				shifted := time.Date(
					now.Year(), now.Month(), now.Day(),
					originalInColLoc.Hour(), originalInColLoc.Minute(), originalInColLoc.Second(),
					originalInColLoc.Nanosecond(),
					loc,
				)
				dt.Data[i] = proto.ToDateTime64(shifted, dt.Precision)
			}
		case *proto.ColDateTime:
			// 将日期部分移到今天，保留时间部分
			loc := dt.Location
			for i := range dt.Data {
				original := dt.Data[i].Time()
				originalInColLoc := original.In(loc)
				now := time.Now().In(loc)
				shifted := time.Date(
					now.Year(), now.Month(), now.Day(),
					originalInColLoc.Hour(), originalInColLoc.Minute(), originalInColLoc.Second(),
					originalInColLoc.Nanosecond(),
					loc,
				)
				dt.Data[i] = proto.ToDateTime(shifted)
			}
		}
	}
}

func (c *DynamicSharedColumns) ShiftTimestamp(snapshot ReplayTimeSnapshot) {
	for _, idx := range c.timestampIndices {
		col := c.cols[idx]
		switch dt := col.(type) {
		case *proto.ColDateTime64Raw:
			// 应用replay时间快照偏移
			for i := range dt.Data {
				original := dt.Data[i].Time(dt.Precision)
				shifted := snapshot.ShiftTimestamp(original)
				dt.Data[i] = proto.ToDateTime64(shifted, dt.Precision)
			}
		case *proto.ColDateTime:
			// 应用replay时间快照偏移
			for i := range dt.Data {
				original := dt.Data[i].Time()
				shifted := snapshot.ShiftTimestamp(original)
				dt.Data[i] = proto.ToDateTime(shifted)
			}
		}
	}
}

func (c *DynamicSharedColumns) UpdateTimestampNow() {
	now := time.Now()
	for _, idx := range c.timestampIndices {
		col := c.cols[idx]
		switch dt := col.(type) {
		case *proto.ColDateTime64Raw:
			// 更新所有DateTime64值为当前时间
			for i := range dt.Data {
				dt.Data[i] = proto.ToDateTime64(now, dt.Precision)
			}
		case *proto.ColDateTime:
			// 更新所有DateTime值为当前时间
			for i := range dt.Data {
				dt.Data[i] = proto.ToDateTime(now)
			}
		}
	}
}

func (c *DynamicSharedColumns) UpdateTimestampMinute() {
	for _, idx := range c.timestampIndices {
		col := c.cols[idx]
		switch dt := col.(type) {
		case *proto.ColDateTime64Raw:
			// 移到当前分钟，保留秒和纳秒
			// 使用列的Location确保时区一致性
			loc := dt.Location
			for i := range dt.Data {
				original := dt.Data[i].Time(dt.Precision)
				originalInColLoc := original.In(loc)
				now := time.Now().In(loc)
				// 创建新时间：当前的年月日时分 + 原始的秒和纳秒
				shifted := time.Date(
					now.Year(), now.Month(), now.Day(),
					now.Hour(), now.Minute(),
					originalInColLoc.Second(), originalInColLoc.Nanosecond(),
					loc,
				)
				dt.Data[i] = proto.ToDateTime64(shifted, dt.Precision)
			}
		case *proto.ColDateTime:
			// 移到当前分钟，保留秒
			loc := dt.Location
			for i := range dt.Data {
				original := dt.Data[i].Time()
				originalInColLoc := original.In(loc)
				now := time.Now().In(loc)
				shifted := time.Date(
					now.Year(), now.Month(), now.Day(),
					now.Hour(), now.Minute(),
					originalInColLoc.Second(), originalInColLoc.Nanosecond(),
					loc,
				)
				dt.Data[i] = proto.ToDateTime(shifted)
			}
		}
	}
}

// MutateIDs 对于动态列不实现ID变更功能。
// 这是因为我们无法在没有语义信息的情况下识别哪些列代表ID（TraceId、SpanId等）。
// 这是可以接受的，因为ID变更主要用于生成模式的循环，而磁盘模式通常重放真实数据，
// 其中ID的唯一性已经得到保证。
func (c *DynamicSharedColumns) MutateIDs(loopIndex int) {
	// no-op: 无法识别ID列，因此不进行变更
}

func (c *DynamicSharedColumns) BindResults(results proto.Results) error {
	c.results = results
	c.input = make(proto.Input, len(results))
	c.cols = make([]proto.Column, len(results))
	for i := range results {
		// results[i].Data is a proto.ColResult; ensure it also satisfies proto.Column (both result+input)
		if col, ok := results[i].Data.(proto.Column); ok {
			c.cols[i] = col
			c.input[i] = proto.InputColumn{Name: results[i].Name, Data: col}
		} else {
			return fmt.Errorf("result column %q does not implement full Column interface", results[i].Name)
		}
	}
	return nil
}

func (c *DynamicSharedColumns) BindInput(input proto.Input) error {
	c.input = input
	c.results = make(proto.Results, len(input))
	c.cols = make([]proto.Column, len(input))
	for i := range input {
		if col, ok := input[i].Data.(proto.Column); ok {
			c.cols[i] = col
			c.results[i] = proto.ResultColumn{Name: input[i].Name, Data: col}
		} else {
			return fmt.Errorf("input column %q does not implement full Column interface", input[i].Name)
		}
	}
	return nil
}

// NewDynamicSharedColumnsFromSchema constructs a DynamicSharedColumns instance
// with concrete column implementations based on the provided ClickHouse column types.
// types should be ColumnType values (string form) as returned by system.columns.type
func NewDynamicSharedColumnsFromSchema(names []string, types []proto.ColumnType) *DynamicSharedColumns {
	c := NewDynamicSharedColumns()
	cols := make([]proto.Column, len(names))
	var timestampIndices []int

	for i := range names {
		t := types[i]
		var col proto.Column
		base := t.Base()

		// 跟踪DateTime/DateTime64列的索引用于时间戳操作
		if base == proto.ColumnTypeDateTime64 || base == proto.ColumnTypeDateTime {
			timestampIndices = append(timestampIndices, i)
		}

		switch base {
		case proto.ColumnTypeDateTime64:
			v := newColDateTime64Raw(0)
			col = &v
		case proto.ColumnTypeDateTime:
			v := newColDateTime(0)
			col = &v
		case proto.ColumnTypeUInt64:
			v := make(proto.ColUInt64, 0, 0)
			col = &v
		case proto.ColumnTypeUInt32:
			v := make(proto.ColUInt32, 0, 0)
			col = &v
		case proto.ColumnTypeUInt8:
			v := make(proto.ColUInt8, 0, 0)
			col = &v
		case proto.ColumnTypeInt64:
			v := make(proto.ColInt64, 0, 0)
			col = &v
		case proto.ColumnTypeFloat64:
			v := make(proto.ColFloat64, 0, 0)
			col = &v
		case proto.ColumnTypeString, proto.ColumnTypeFixedString:
			v := newColString(0, 0)
			col = &v
		case proto.ColumnTypeLowCardinality:
			// LowCardinality(String) and similar: map to low-cardinality string
			v := newColLowCardinalityString(0, 0)
			col = v
		case proto.ColumnTypeMap:
			// Map(String, String) -> ColMap[string,string]
			v := newColMapLowCardinalityStringString(0, 0)
			col = v
		case proto.ColumnTypeArray:
			// Fallback to string array
			v := newColArrayString(0, 0)
			col = v
		case proto.ColumnTypeInt32:
			v := make(proto.ColInt32, 0, 0)
			col = &v
		case proto.ColumnTypeInt16:
			v := make(proto.ColInt16, 0, 0)
			col = &v
		case proto.ColumnTypeInt8:
			v := make(proto.ColInt8, 0, 0)
			col = &v
		case proto.ColumnTypeFloat32:
			v := make(proto.ColFloat32, 0, 0)
			col = &v
		default:
			// Fallback to string
			v := newColString(0, 0)
			col = &v
		}
		cols[i] = col
	}

	// Build Results and Input
	results := make(proto.Results, len(names))
	input := make(proto.Input, len(names))
	for i := range names {
		results[i] = proto.ResultColumn{Name: names[i], Data: cols[i]}
		input[i] = proto.InputColumn{Name: names[i], Data: cols[i]}
	}
	c.BindResults(results)
	c.timestampIndices = timestampIndices

	return c
}

// shiftHexByte shifts a hex character [0-9a-f] forward by n within the hex alphabet (mod 16).
// Falls back to raw byte addition for non-hex characters.
func shiftHexByte(b byte, n int) byte {
	var val int
	switch {
	case b >= '0' && b <= '9':
		val = int(b - '0')
	case b >= 'a' && b <= 'f':
		val = int(b-'a') + 10
	default:
		return b + byte(n)
	}
	val = (val + n) % 16
	if val < 10 {
		return byte('0' + val)
	}
	return byte('a' + val - 10)
}

// idShiftBytes is the number of trailing bytes mutated per ID column.
// 3 hex characters gives 16^3 = 4096 distinct loop values.
const idShiftBytes = 3

// shiftColStrLastByte shifts the last idShiftBytes of each string in col by loopIndex,
// treating them as base-16 digits within the hex alphabet [0-9a-f].
// Strings shorter than idShiftBytes are shifted over however many bytes they have.
func shiftColStrLastByte(col *proto.ColStr, loopIndex int) {
	for i := range col.Pos {
		length := col.Pos[i].End - col.Pos[i].Start
		if length == 0 {
			continue
		}
		n := min(idShiftBytes, length)
		rem := loopIndex
		for j := 1; j <= n; j++ {
			col.Buf[col.Pos[i].End-j] = shiftHexByte(col.Buf[col.Pos[i].End-j], rem%16)
			rem /= 16
		}
	}
}

// ShiftDateToToday shifts the time.Time to current date without affecting time component
func ShiftDateToToday(oldTime time.Time) time.Time {
	// 使用原时间的时区获取当前时间，确保时区一致
	loc := oldTime.Location()
	now := time.Now().In(loc)
	hour, minute, sec := oldTime.Clock()
	nsec := oldTime.Nanosecond()

	newTime := time.Date(
		now.Year(),
		now.Month(),
		now.Day(),
		hour,
		minute,
		sec,
		nsec,
		loc,
	)

	return newTime
}

// ShiftTimestampMinute shifts the time.Time to current minute without affecting seconds component
func ShiftTimestampMinute(original time.Time) time.Time {
	// 使用原时间的时区获取当前时间，确保时区一致
	loc := original.Location()
	now := time.Now().In(loc)
	newTime := time.Date(
		now.Year(),
		now.Month(),
		now.Day(),
		now.Hour(),
		now.Minute(),
		original.Second(),
		original.Nanosecond(),
		loc,
	)

	return newTime
}

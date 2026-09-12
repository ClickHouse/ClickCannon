package block

import (
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

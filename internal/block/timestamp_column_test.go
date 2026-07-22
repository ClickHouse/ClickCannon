package block

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ClickHouse/ch-go/proto"
)

// TestTimestampColumnNameMismatch_BugConditionExploration demonstrates the bug where
// UpdateTimestampNow fails to update custom timestamp column names.
//
// **CRITICAL - BUGFIX EXPLORATION TEST**:
// - This test MUST FAIL on unfixed code - failure confirms the bug exists
// - DO NOT attempt to fix the test or code when it fails
// - These tests encode the expected behavior and will validate the fix later
//
// **Validates: Requirements 1.4, 1.5, 1.6**
func TestTimestampColumnNameMismatch_BugConditionExploration(t *testing.T) {
	historicalTime := time.Date(2020, 1, 1, 10, 0, 0, 0, time.UTC)
	testCases := []struct {
		name          string
		columnName    string
		columnType    proto.ColumnType
		expectUpdated bool // true if we expect timestamp to be updated (will be false on unfixed code)
	}{
		{
			name:          "tnow_DateTime64_CustomName",
			columnName:    "tnow",
			columnType:    proto.ColumnTypeDateTime64,
			expectUpdated: true, // Expected behavior: should update custom name
		},
		{
			name:          "event_time_DateTime64_CustomName",
			columnName:    "event_time",
			columnType:    proto.ColumnTypeDateTime64,
			expectUpdated: true, // Expected behavior: should update custom name
		},
		{
			name:          "log_time_DateTime_CustomName",
			columnName:    "log_time",
			columnType:    proto.ColumnTypeDateTime,
			expectUpdated: true, // Expected behavior: should update custom name
		},
		{
			name:          "Timestamp_StandardName",
			columnName:    "Timestamp",
			columnType:    proto.ColumnTypeDateTime64,
			expectUpdated: true, // Standard name should work (control case)
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create DynamicSharedColumns with custom timestamp column name
			names := []string{tc.columnName, "message"}
			types := []proto.ColumnType{tc.columnType, proto.ColumnTypeString}

			cols := NewDynamicSharedColumnsFromSchema(names, types)

			// Populate with historical timestamp values
			switch tc.columnType {
			case proto.ColumnTypeDateTime64:
				timestampCol := cols.cols[0].(*proto.ColDateTime64Raw)
				timestampCol.Data = append(timestampCol.Data, proto.ToDateTime64(historicalTime, proto.PrecisionNano))
				timestampCol.Data = append(timestampCol.Data, proto.ToDateTime64(historicalTime.Add(time.Hour), proto.PrecisionNano))
			case proto.ColumnTypeDateTime:
				timestampCol := cols.cols[0].(*proto.ColDateTime)
				timestampCol.Data = append(timestampCol.Data, proto.ToDateTime(historicalTime))
				timestampCol.Data = append(timestampCol.Data, proto.ToDateTime(historicalTime.Add(time.Hour)))
			}

			// Populate message column
			messageCol := cols.cols[1].(*proto.ColStr)
			messageCol.Append("test message 1")
			messageCol.Append("test message 2")

			// Call UpdateTimestampNow - this should update all DateTime columns
			beforeUpdate := time.Now()
			cols.UpdateTimestampNow()

			// Check if timestamps were updated
			var actualTimestamp time.Time
			switch tc.columnType {
			case proto.ColumnTypeDateTime64:
				timestampCol := cols.cols[0].(*proto.ColDateTime64Raw)
				actualTimestamp = timestampCol.Data[0].Time(proto.PrecisionNano)
			case proto.ColumnTypeDateTime:
				timestampCol := cols.cols[0].(*proto.ColDateTime)
				actualTimestamp = timestampCol.Data[0].Time()
			}

			// Check if timestamp is current (within last 5 seconds)
			timeDiff := time.Since(actualTimestamp)
			isCurrentTime := timeDiff >= 0 && timeDiff < 5*time.Second
			_ = beforeUpdate // Used in calculation above

			if tc.expectUpdated {
				if !isCurrentTime {
					// **BUG DETECTED**: This will fail on unfixed code for custom column names
					// The timestamp should be current but remains historical
					t.Errorf("BUG CONFIRMED - Timestamp column %q was NOT updated to current time.\n"+
						"  Historical timestamp: %v\n"+
						"  Actual timestamp: %v\n"+
						"  Expected: current time (within 5 seconds of %v)\n"+
						"  Time diff from now: %v\n"+
						"  This confirms the hardcoded column name bug exists.",
						tc.columnName, historicalTime, actualTimestamp, time.Now(), timeDiff)
				} else {
					t.Logf("✓ Timestamp column %q correctly updated to current time: %v", tc.columnName, actualTimestamp)
				}
			}
		})
	}
}

// TestMultipleTimestampColumns_BugConditionExploration tests tables with multiple timestamp columns.
//
// **CRITICAL - BUGFIX EXPLORATION TEST**:
// - This test MUST FAIL on unfixed code - failure confirms bug exists
// - Expected behavior: ALL timestamp columns should be updated regardless of names
//
// **Validates: Requirements 1.5, 2.10**
func TestMultipleTimestampColumns_BugConditionExploration(t *testing.T) {
	historicalTime := time.Date(2020, 6, 15, 14, 30, 0, 0, time.UTC)

	names := []string{"event_time", "log_time", "message"}
	types := []proto.ColumnType{proto.ColumnTypeDateTime64, proto.ColumnTypeDateTime, proto.ColumnTypeString}

	cols := NewDynamicSharedColumnsFromSchema(names, types)

	// Populate event_time (DateTime64)
	eventTimeCol := cols.cols[0].(*proto.ColDateTime64Raw)
	eventTimeCol.Data = append(eventTimeCol.Data, proto.ToDateTime64(historicalTime, proto.PrecisionNano))
	eventTimeCol.Data = append(eventTimeCol.Data, proto.ToDateTime64(historicalTime.Add(time.Minute), proto.PrecisionNano))

	// Populate log_time (DateTime)
	logTimeCol := cols.cols[1].(*proto.ColDateTime)
	logTimeCol.Data = append(logTimeCol.Data, proto.ToDateTime(historicalTime))
	logTimeCol.Data = append(logTimeCol.Data, proto.ToDateTime(historicalTime.Add(time.Minute)))

	// Populate message
	messageCol := cols.cols[2].(*proto.ColStr)
	messageCol.Append("event 1")
	messageCol.Append("event 2")

	// Call UpdateTimestampNow
	cols.UpdateTimestampNow()

	// Check if both timestamp columns were updated
	eventTime := eventTimeCol.Data[0].Time(proto.PrecisionNano)
	logTime := logTimeCol.Data[0].Time()

	eventTimeDiff := time.Since(eventTime)
	logTimeDiff := time.Since(logTime)

	eventTimeUpdated := eventTimeDiff >= 0 && eventTimeDiff < 5*time.Second
	logTimeUpdated := logTimeDiff >= 0 && logTimeDiff < 5*time.Second

	if !eventTimeUpdated {
		t.Errorf("BUG CONFIRMED - event_time column NOT updated.\n"+
			"  Historical: %v\n"+
			"  Actual: %v\n"+
			"  Expected: current time\n"+
			"  Time diff: %v",
			historicalTime, eventTime, eventTimeDiff)
	} else {
		t.Logf("✓ event_time correctly updated: %v", eventTime)
	}

	if !logTimeUpdated {
		t.Errorf("BUG CONFIRMED - log_time column NOT updated.\n"+
			"  Historical: %v\n"+
			"  Actual: %v\n"+
			"  Expected: current time\n"+
			"  Time diff: %v",
			historicalTime, logTime, logTimeDiff)
	} else {
		t.Logf("✓ log_time correctly updated: %v", logTime)
	}

	// Both columns should be updated to approximately the same time
	if eventTimeUpdated && logTimeUpdated {
		timeDiffBetweenCols := eventTime.Sub(logTime)
		if timeDiffBetweenCols < 0 {
			timeDiffBetweenCols = -timeDiffBetweenCols
		}
		if timeDiffBetweenCols > 2*time.Second {
			t.Errorf("Timestamp columns not updated consistently: event_time=%v, log_time=%v, diff=%v",
				eventTime, logTime, timeDiffBetweenCols)
		}
	}
}

// TestTimestampShiftModes_BugConditionExploration tests different shift modes with custom column names.
//
// **CRITICAL - BUGFIX EXPLORATION TEST**:
// - This test MUST FAIL on unfixed code for custom column names
// - Tests shift_timestamp modes: date, minute, all
//
// **Validates: Requirements 1.4, 2.7, 2.8, 2.9**
func TestTimestampShiftModes_BugConditionExploration(t *testing.T) {
	// Historical timestamp: 2020-06-15 14:35:42.123456789
	historicalTime := time.Date(2020, 6, 15, 14, 35, 42, 123456789, time.UTC)

	testCases := []struct {
		name         string
		columnName   string
		shiftMode    string
		validateFunc func(t *testing.T, original, shifted time.Time)
	}{
		{
			name:       "ShiftDate_CustomColumnName",
			columnName: "tnow",
			shiftMode:  "date",
			validateFunc: func(t *testing.T, original, shifted time.Time) {
				now := time.Now()
				// Date should be today
				if shifted.Year() != now.Year() || shifted.Month() != now.Month() || shifted.Day() != now.Day() {
					t.Errorf("BUG CONFIRMED - Date not shifted to today.\n"+
						"  Original: %v\n"+
						"  Shifted: %v\n"+
						"  Expected date: %v-%02d-%02d",
						original, shifted, now.Year(), now.Month(), now.Day())
				}
				// Time component should be preserved (compare in UTC)
				originalUTC := original.UTC()
				shiftedUTC := shifted.UTC()
				if shiftedUTC.Hour() != originalUTC.Hour() || shiftedUTC.Minute() != originalUTC.Minute() || shiftedUTC.Second() != originalUTC.Second() {
					t.Errorf("Time component not preserved: original=%02d:%02d:%02d UTC, shifted=%02d:%02d:%02d UTC",
						originalUTC.Hour(), originalUTC.Minute(), originalUTC.Second(),
						shiftedUTC.Hour(), shiftedUTC.Minute(), shiftedUTC.Second())
				}
			},
		},
		{
			name:       "ShiftMinute_CustomColumnName",
			columnName: "event_time",
			shiftMode:  "minute",
			validateFunc: func(t *testing.T, original, shifted time.Time) {
				now := time.Now()
				// Should be current minute
				if shifted.Year() != now.Year() || shifted.Month() != now.Month() || shifted.Day() != now.Day() ||
					shifted.Hour() != now.Hour() || shifted.Minute() != now.Minute() {
					t.Errorf("BUG CONFIRMED - Timestamp not shifted to current minute.\n"+
						"  Original: %v\n"+
						"  Shifted: %v\n"+
						"  Expected: %v (current minute)",
						original, shifted, now.Truncate(time.Minute))
				}
				// Seconds and nanoseconds should be preserved
				if shifted.Second() != original.Second() || shifted.Nanosecond() != original.Nanosecond() {
					t.Errorf("Seconds/nanoseconds not preserved: original=%d.%09d, shifted=%d.%09d",
						original.Second(), original.Nanosecond(),
						shifted.Second(), shifted.Nanosecond())
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			names := []string{tc.columnName}
			types := []proto.ColumnType{proto.ColumnTypeDateTime64}

			cols := NewDynamicSharedColumnsFromSchema(names, types)

			// Populate with historical timestamp
			timestampCol := cols.cols[0].(*proto.ColDateTime64Raw)
			timestampCol.Data = append(timestampCol.Data, proto.ToDateTime64(historicalTime, proto.PrecisionNano))

			// Apply shift based on mode
			switch tc.shiftMode {
			case "date":
				cols.UpdateDate()
			case "minute":
				cols.UpdateTimestampMinute()
			case "all":
				// For "all" mode we need ReplayTimeKeeper, test separately
				log := slog.New(slog.NewTextHandler(os.Stderr, nil))
				keeper := NewReplayTimeKeeper(log)
				ctx := context.Background()
				go keeper.Run(ctx)
				keeper.ReportEarliestTimestamp(historicalTime)
				snapshot := keeper.Snapshot(0)
				cols.ShiftTimestamp(snapshot)
			}

			// Get shifted timestamp
			shiftedTimestamp := timestampCol.Data[0].Time(proto.PrecisionNano)

			// Validate
			tc.validateFunc(t, historicalTime, shiftedTimestamp)
		})
	}
}

// TestNativeFileWithCustomTimestampColumn_BugConditionExploration tests reading a Native
// format file with custom timestamp column name.
//
// **CRITICAL - BUGFIX EXPLORATION TEST**:
// - This test simulates real disk mode scenario
// - Creates Native file, reads it, applies shift_timestamp=now
// - Should fail on unfixed code when column name is not "Timestamp"
//
// **Validates: Requirements 1.4, 1.6**
func TestNativeFileWithCustomTimestampColumn_BugConditionExploration(t *testing.T) {
	tempDir := t.TempDir()
	nativeFile := filepath.Join(tempDir, "test_tnow.native")

	// Create a Native format file with custom timestamp column "tnow"
	historicalTime := time.Date(2019, 3, 10, 8, 15, 30, 0, time.UTC)

	// Write Native format file
	f, err := os.Create(nativeFile)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer f.Close()

	var buf proto.Buffer

	// Create input columns for encoding
	var input proto.Input

	// Add tnow column (DateTime64)
	tnowCol := newColDateTime64Raw(2)
	tnowCol.Data = append(tnowCol.Data, proto.ToDateTime64(historicalTime, proto.PrecisionNano))
	tnowCol.Data = append(tnowCol.Data, proto.ToDateTime64(historicalTime.Add(time.Minute), proto.PrecisionNano))
	input = append(input, proto.InputColumn{Name: "tnow", Data: &tnowCol})

	// Add message column
	messageCol := newColString(2, 50)
	messageCol.Append("message 1")
	messageCol.Append("message 2")
	input = append(input, proto.InputColumn{Name: "message", Data: &messageCol})

	// Encode to Native format
	block := proto.Block{Rows: 2, Columns: len(input)}
	block.EncodeRawBlock(&buf, 54451, input)

	if _, err := f.Write(buf.Buf); err != nil {
		t.Fatalf("Failed to write native file: %v", err)
	}

	// Close file to ensure data is flushed
	if err := f.Close(); err != nil {
		t.Fatalf("Failed to close file: %v", err)
	}

	// Now read the file back and apply UpdateTimestampNow
	f, err = os.Open(nativeFile)
	if err != nil {
		t.Fatalf("Failed to open test file: %v", err)
	}
	defer f.Close()

	reader := proto.NewReader(f)

	// Read with DynamicSharedColumns
	names := []string{"tnow", "message"}
	types := []proto.ColumnType{proto.ColumnTypeDateTime64, proto.ColumnTypeString}
	cols := NewDynamicSharedColumnsFromSchema(names, types)

	// Debug: check Results length
	if len(cols.Results()) == 0 {
		t.Fatalf("cols.Results() is empty! This should not happen.")
	}
	t.Logf("cols.Results() length: %d", len(cols.Results()))

	var dec proto.Block
	err = dec.DecodeRawBlock(reader, 54451, cols.Results())
	if err != nil {
		t.Fatalf("Failed to decode block: %v", err)
	}

	// Before update - should be historical
	tnowColRead := cols.cols[0].(*proto.ColDateTime64Raw)
	timestampBefore := tnowColRead.Data[0].Time(proto.PrecisionNano)

	if !timestampBefore.Equal(historicalTime) {
		t.Errorf("Timestamp before update incorrect: got %v, want %v", timestampBefore, historicalTime)
	}

	// Apply UpdateTimestampNow
	cols.UpdateTimestampNow()

	// After update - should be current time
	timestampAfter := tnowColRead.Data[0].Time(proto.PrecisionNano)
	timeDiff := time.Since(timestampAfter)

	if timeDiff < 0 || timeDiff > 5*time.Second {
		t.Errorf("BUG CONFIRMED - Native file with custom column 'tnow' NOT updated to current time.\n"+
			"  Historical: %v\n"+
			"  After UpdateTimestampNow: %v\n"+
			"  Time diff from now: %v\n"+
			"  Expected: current time (within 5 seconds)\n"+
			"  This confirms the hardcoded column name bug in disk mode.",
			historicalTime, timestampAfter, timeDiff)
	} else {
		t.Logf("✓ Native file custom column 'tnow' correctly updated to: %v", timestampAfter)
	}
}

package block

import (
	"testing"
	"time"

	"github.com/ClickHouse/ch-go/proto"
)

// TestBugCondition_TimestampColumnNameMismatch is a bug condition exploration test.
// This test demonstrates that DynamicSharedColumns with custom time column names
// (e.g., "tnow", "event_time") fails to update timestamps when shift operations are called.
//
// EXPECTED BEHAVIOR ON UNFIXED CODE: This test SHOULD FAIL
// - The bug is that UpdateTimestampNow() is a no-op stub for DynamicSharedColumns
// - Custom time columns are not identified or updated
// - Timestamps remain historical (counterexample found)
//
// EXPECTED BEHAVIOR ON FIXED CODE: This test should pass
// - DynamicSharedColumns should track which columns are DateTime types
// - UpdateTimestampNow() should update all DateTime columns to current time
//
// **Validates: Requirements 1.4, 1.5, 1.6**
func TestBugCondition_TimestampColumnNameMismatch(t *testing.T) {
	testCases := []struct {
		name              string
		columnName        string
		columnType        proto.ColumnType
		historicalTime    time.Time
		expectTimeShifted bool
	}{
		{
			name:              "Custom column tnow (DateTime64)",
			columnName:        "tnow",
			columnType:        proto.ColumnTypeDateTime64,
			historicalTime:    time.Date(2020, 1, 1, 10, 30, 0, 0, time.UTC),
			expectTimeShifted: true,
		},
		{
			name:              "Custom column event_time (DateTime64)",
			columnName:        "event_time",
			columnType:        proto.ColumnTypeDateTime64,
			historicalTime:    time.Date(2021, 6, 15, 14, 22, 33, 0, time.UTC),
			expectTimeShifted: true,
		},
		{
			name:              "Custom column log_time (DateTime)",
			columnName:        "log_time",
			columnType:        proto.ColumnTypeDateTime,
			historicalTime:    time.Date(2019, 3, 20, 8, 15, 0, 0, time.UTC),
			expectTimeShifted: true,
		},
		{
			name:              "Standard Timestamp column (DateTime64) - control case",
			columnName:        "Timestamp",
			columnType:        proto.ColumnTypeDateTime64,
			historicalTime:    time.Date(2020, 1, 1, 10, 30, 0, 0, time.UTC),
			expectTimeShifted: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create DynamicSharedColumns with custom timestamp column name
			names := []string{tc.columnName, "trace_id", "span_id"}
			types := []proto.ColumnType{tc.columnType, proto.ColumnTypeString, proto.ColumnTypeString}

			cols := NewDynamicSharedColumnsFromSchema(names, types)

			// Populate the timestamp column with historical data
			const rowCount = 10
			switch tc.columnType {
			case proto.ColumnTypeDateTime64:
				if dt64Col, ok := cols.cols[0].(*proto.ColDateTime64Raw); ok {
					dt64Col.Precision = proto.PrecisionNano
					for i := 0; i < rowCount; i++ {
						dt64Col.Data = append(dt64Col.Data, proto.ToDateTime64(tc.historicalTime.Add(time.Duration(i)*time.Second), proto.PrecisionNano))
					}
				} else {
					t.Fatalf("expected ColDateTime64Raw for %s column", tc.columnName)
				}
			case proto.ColumnTypeDateTime:
				if dtCol, ok := cols.cols[0].(*proto.ColDateTime); ok {
					for i := 0; i < rowCount; i++ {
						dtCol.Data = append(dtCol.Data, proto.ToDateTime(tc.historicalTime.Add(time.Duration(i)*time.Second)))
					}
				} else {
					t.Fatalf("expected ColDateTime for %s column", tc.columnName)
				}
			}

			// Record time before shift
			beforeShift := time.Now()
			time.Sleep(10 * time.Millisecond) // Ensure some time passes

			// Call UpdateTimestampNow - this should update the custom time column
			cols.UpdateTimestampNow()

			// Record time after shift
			afterShift := time.Now()

			// Verify that timestamps were updated to current time
			if tc.expectTimeShifted {
				switch tc.columnType {
				case proto.ColumnTypeDateTime64:
					if dt64Col, ok := cols.cols[0].(*proto.ColDateTime64Raw); ok {
						for i := 0; i < len(dt64Col.Data); i++ {
							actualTime := dt64Col.Data[i].Time(proto.PrecisionNano)

							// BUG CONDITION: On unfixed code, actualTime will still be historical
							// because UpdateTimestampNow() is a no-op stub for DynamicSharedColumns
							if actualTime.Before(beforeShift.Add(-1 * time.Second)) {
								t.Errorf("COUNTEREXAMPLE FOUND: Column %q was NOT updated by UpdateTimestampNow(). "+
									"Row %d has timestamp %v which is before beforeShift %v. "+
									"This demonstrates the bug: custom time column names are not detected or updated.",
									tc.columnName, i, actualTime, beforeShift)
							}

							// Verify timestamp is current (within reasonable window)
							if actualTime.Before(beforeShift) || actualTime.After(afterShift.Add(100*time.Millisecond)) {
								t.Errorf("Column %q row %d: expected timestamp between %v and %v, got %v",
									tc.columnName, i, beforeShift, afterShift, actualTime)
							}
						}
					}
				case proto.ColumnTypeDateTime:
					if dtCol, ok := cols.cols[0].(*proto.ColDateTime); ok {
						for i := 0; i < len(dtCol.Data); i++ {
							actualTime := dtCol.Data[i].Time()

							// BUG CONDITION: On unfixed code, actualTime will still be historical
							if actualTime.Before(beforeShift.Add(-1 * time.Second)) {
								t.Errorf("COUNTEREXAMPLE FOUND: Column %q was NOT updated by UpdateTimestampNow(). "+
									"Row %d has timestamp %v which is before beforeShift %v. "+
									"This demonstrates the bug: custom time column names are not detected or updated.",
									tc.columnName, i, actualTime, beforeShift)
							}

							// Verify timestamp is current (within reasonable window)
							// DateTime has only second precision, so allow larger window
							if actualTime.Before(beforeShift.Add(-1*time.Second)) || actualTime.After(afterShift.Add(2*time.Second)) {
								t.Errorf("Column %q row %d: expected timestamp between %v and %v, got %v",
									tc.columnName, i, beforeShift.Add(-1*time.Second), afterShift.Add(2*time.Second), actualTime)
							}
						}
					}
				}
			}
		})
	}
}

// TestBugCondition_MultipleTimeColumns tests that all DateTime columns are updated
// when shift operations are called, regardless of their names.
//
// EXPECTED BEHAVIOR ON UNFIXED CODE: This test SHOULD FAIL
// - UpdateTimestampNow() is a no-op for DynamicSharedColumns
// - Both event_time and log_time will remain historical
//
// **Validates: Requirements 1.5, 2.10**
func TestBugCondition_MultipleTimeColumns(t *testing.T) {
	// Create table with multiple time columns (common in real schemas)
	names := []string{"event_time", "log_time", "trace_id"}
	types := []proto.ColumnType{proto.ColumnTypeDateTime64, proto.ColumnTypeDateTime, proto.ColumnTypeString}

	cols := NewDynamicSharedColumnsFromSchema(names, types)

	// Populate both time columns with historical data
	historicalTime1 := time.Date(2020, 1, 1, 10, 30, 0, 0, time.UTC)
	historicalTime2 := time.Date(2020, 1, 1, 10, 45, 0, 0, time.UTC)
	const rowCount = 5

	// event_time (DateTime64)
	if dt64Col, ok := cols.cols[0].(*proto.ColDateTime64Raw); ok {
		dt64Col.Precision = proto.PrecisionNano
		for i := 0; i < rowCount; i++ {
			dt64Col.Data = append(dt64Col.Data, proto.ToDateTime64(historicalTime1.Add(time.Duration(i)*time.Second), proto.PrecisionNano))
		}
	} else {
		t.Fatalf("expected ColDateTime64Raw for event_time")
	}

	// log_time (DateTime)
	if dtCol, ok := cols.cols[1].(*proto.ColDateTime); ok {
		for i := 0; i < rowCount; i++ {
			dtCol.Data = append(dtCol.Data, proto.ToDateTime(historicalTime2.Add(time.Duration(i)*time.Second)))
		}
	} else {
		t.Fatalf("expected ColDateTime for log_time")
	}

	beforeShift := time.Now()
	time.Sleep(10 * time.Millisecond)

	// Call UpdateTimestampNow - should update BOTH time columns
	cols.UpdateTimestampNow()

	afterShift := time.Now()

	// Check event_time was updated
	if dt64Col, ok := cols.cols[0].(*proto.ColDateTime64Raw); ok {
		for i := 0; i < len(dt64Col.Data); i++ {
			actualTime := dt64Col.Data[i].Time(proto.PrecisionNano)
			if actualTime.Before(beforeShift.Add(-1 * time.Second)) {
				t.Errorf("COUNTEREXAMPLE: event_time column was NOT updated. Row %d has historical timestamp %v",
					i, actualTime)
			}
			if actualTime.Before(beforeShift) || actualTime.After(afterShift.Add(100*time.Millisecond)) {
				t.Errorf("event_time row %d: expected timestamp between %v and %v, got %v",
					i, beforeShift, afterShift, actualTime)
			}
		}
	}

	// Check log_time was updated
	if dtCol, ok := cols.cols[1].(*proto.ColDateTime); ok {
		for i := 0; i < len(dtCol.Data); i++ {
			actualTime := dtCol.Data[i].Time()
			if actualTime.Before(beforeShift.Add(-1 * time.Second)) {
				t.Errorf("COUNTEREXAMPLE: log_time column was NOT updated. Row %d has historical timestamp %v",
					i, actualTime)
			}
			// DateTime has only second precision, allow larger window
			if actualTime.Before(beforeShift.Add(-1*time.Second)) || actualTime.After(afterShift.Add(2*time.Second)) {
				t.Errorf("log_time row %d: expected timestamp between %v and %v, got %v",
					i, beforeShift.Add(-1*time.Second), afterShift.Add(2*time.Second), actualTime)
			}
		}
	}
}

// TestBugCondition_UpdateDateWithCustomColumns tests date shifting on custom time columns
//
// EXPECTED BEHAVIOR ON UNFIXED CODE: This test SHOULD FAIL
// - UpdateDate() is a no-op for DynamicSharedColumns
// - Custom time columns retain historical dates
//
// **Validates: Requirements 2.7**
func TestBugCondition_UpdateDateWithCustomColumns(t *testing.T) {
	names := []string{"tnow", "message"}
	types := []proto.ColumnType{proto.ColumnTypeDateTime64, proto.ColumnTypeString}

	cols := NewDynamicSharedColumnsFromSchema(names, types)

	// Historical timestamp with specific time component
	historicalTime := time.Date(2020, 5, 10, 14, 22, 33, 123456789, time.UTC)
	const rowCount = 3

	if dt64Col, ok := cols.cols[0].(*proto.ColDateTime64Raw); ok {
		dt64Col.Precision = proto.PrecisionNano
		for i := 0; i < rowCount; i++ {
			dt64Col.Data = append(dt64Col.Data, proto.ToDateTime64(historicalTime, proto.PrecisionNano))
		}
	} else {
		t.Fatalf("expected ColDateTime64Raw for tnow")
	}

	// Call UpdateDate - should shift date to today, preserve time IN ORIGINAL TIMEZONE
	cols.UpdateDate()

	today := time.Now()

	if dt64Col, ok := cols.cols[0].(*proto.ColDateTime64Raw); ok {
		for i := 0; i < len(dt64Col.Data); i++ {
			actualTime := dt64Col.Data[i].Time(proto.PrecisionNano)

			// BUG CONDITION: Date will still be 2020-05-10 on unfixed code
			if actualTime.Year() == 2020 && actualTime.Month() == time.May && actualTime.Day() == 10 {
				t.Errorf("COUNTEREXAMPLE: tnow column date was NOT updated. Row %d has historical date %v",
					i, actualTime)
			}

			// Verify date is today
			if actualTime.Year() != today.Year() || actualTime.Month() != today.Month() || actualTime.Day() != today.Day() {
				t.Errorf("Row %d: expected date to be today %v, got %v",
					i, today.Format("2006-01-02"), actualTime.Format("2006-01-02"))
			}

			// Verify time component is preserved
			// Note: .Time() returns time in local timezone, but it represents the same moment
			// So we need to convert both to UTC for comparison
			actualTimeUTC := actualTime.UTC()
			historicalTimeUTC := historicalTime.UTC()
			expectedHour, expectedMinute, expectedSecond := historicalTimeUTC.Hour(), historicalTimeUTC.Minute(), historicalTimeUTC.Second()
			if actualTimeUTC.Hour() != expectedHour || actualTimeUTC.Minute() != expectedMinute || actualTimeUTC.Second() != expectedSecond {
				t.Errorf("Row %d: expected time %02d:%02d:%02d UTC, got %02d:%02d:%02d UTC\n"+
					"  actualTime (local): %v\n"+
					"  actualTime (UTC): %v\n"+
					"  historicalTime: %v",
					i, expectedHour, expectedMinute, expectedSecond, actualTimeUTC.Hour(), actualTimeUTC.Minute(), actualTimeUTC.Second(),
					actualTime, actualTimeUTC, historicalTime)
			}
		}
	}
}

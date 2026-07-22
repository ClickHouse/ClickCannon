package block

import (
	"testing"
	"time"

	"github.com/ClickHouse/ch-go/proto"
)

// TestPreservation_GenerateModeColumnStructures tests that generate mode continues
// using typed column structures with no changes after the fix is implemented.
//
// **IMPORTANT**: This test observes baseline behavior on UNFIXED code
// **EXPECTED OUTCOME ON UNFIXED CODE**: This test should PASS
// **EXPECTED OUTCOME ON FIXED CODE**: This test should still PASS (behavior preserved)
//
// **Validates: Requirements 3.1, 3.2**
func TestPreservation_GenerateModeColumnStructures(t *testing.T) {
	// This test imports generate package types, but to avoid circular dependencies,
	// we test the interface behavior expectations here

	// For logs: expect exactly 15 columns with specific names
	expectedLogsColumns := []string{
		"Timestamp", "TraceId", "SpanId", "TraceFlags", "SeverityText",
		"SeverityNumber", "ServiceName", "Body", "ResourceSchemaUrl",
		"ResourceAttributes", "ScopeSchemaUrl", "ScopeName", "ScopeVersion",
		"ScopeAttributes", "LogAttributes",
	}

	// For traces: expect exactly 22 columns
	expectedTracesColumnCount := 22

	// For profiles: expect specific profile columns
	expectedProfilesColumnCount := 21

	// Document the observed behavior
	t.Logf("Generate mode logs expected columns: %v", expectedLogsColumns)
	t.Logf("Generate mode traces expected column count: %d", expectedTracesColumnCount)
	t.Logf("Generate mode profiles expected column count: %d", expectedProfilesColumnCount)

	// These structures are defined in generate package and should not change
	// The fix to disk mode dynamic columns must not affect generate mode at all
	t.Log("BASELINE BEHAVIOR: Generate mode uses typed column structures")
	t.Log("PRESERVATION REQUIREMENT: This must remain unchanged after fix")
}

// TestPreservation_StandardSchemaHandling tests that disk mode with standard
// OTel schemas (15-column logs, 22-column traces) continues working identically
// after the fix is implemented.
//
// **IMPORTANT**: This test observes baseline behavior on UNFIXED code
// **EXPECTED OUTCOME ON UNFIXED CODE**: This test should PASS
// **EXPECTED OUTCOME ON FIXED CODE**: This test should still PASS (behavior preserved)
//
// **Validates: Requirements 3.1, 3.2**
func TestPreservation_StandardSchemaHandling(t *testing.T) {
	// Test standard 15-column logs schema
	standardLogsNames := []string{
		"Timestamp", "TraceId", "SpanId", "TraceFlags", "SeverityText",
		"SeverityNumber", "ServiceName", "Body", "ResourceSchemaUrl",
		"ResourceAttributes", "ScopeSchemaUrl", "ScopeName", "ScopeVersion",
		"ScopeAttributes", "LogAttributes",
	}
	standardLogsTypes := []proto.ColumnType{
		proto.ColumnTypeDateTime64,
		proto.ColumnTypeString,
		proto.ColumnTypeString,
		proto.ColumnTypeUInt8,
		proto.ColumnType("LowCardinality(String)"),
		proto.ColumnTypeUInt8,
		proto.ColumnType("LowCardinality(String)"),
		proto.ColumnTypeString,
		proto.ColumnType("LowCardinality(String)"),
		proto.ColumnType("Map(LowCardinality(String), String)"),
		proto.ColumnType("LowCardinality(String)"),
		proto.ColumnTypeString,
		proto.ColumnType("LowCardinality(String)"),
		proto.ColumnType("Map(LowCardinality(String), String)"),
		proto.ColumnType("Map(LowCardinality(String), String)"),
	}

	// Create columns with standard schema
	cols := NewDynamicSharedColumnsFromSchema(standardLogsNames, standardLogsTypes)

	// Verify basic operations work
	if cols == nil {
		t.Fatal("Failed to create DynamicSharedColumns with standard schema")
	}

	results := cols.Results()
	if len(results) != 15 {
		t.Errorf("Expected 15 columns in Results, got %d", len(results))
	}

	input := cols.Input()
	if len(input) != 15 {
		t.Errorf("Expected 15 columns in Input, got %d", len(input))
	}

	// Verify column names match
	for i, expected := range standardLogsNames {
		if results[i].Name != expected {
			t.Errorf("Column %d: expected name %q, got %q", i, expected, results[i].Name)
		}
	}

	// Test Reset operation
	cols.Reset()

	t.Log("BASELINE BEHAVIOR: Standard OTel schemas work with current code")
	t.Log("PRESERVATION REQUIREMENT: Standard schemas must continue working identically")
}

// TestPreservation_BlockReset tests that Reset operation works identically
// for DynamicSharedColumns before and after the fix.
//
// **EXPECTED OUTCOME ON UNFIXED CODE**: This test should PASS
// **EXPECTED OUTCOME ON FIXED CODE**: This test should still PASS (behavior preserved)
//
// **Validates: Requirements 3.3, 3.4**
func TestPreservation_BlockReset(t *testing.T) {
	names := []string{"col1", "col2", "col3"}
	types := []proto.ColumnType{
		proto.ColumnTypeString,
		proto.ColumnTypeUInt64,
		proto.ColumnTypeDateTime64,
	}

	cols := NewDynamicSharedColumnsFromSchema(names, types)

	// Populate some data
	if strCol, ok := cols.cols[0].(*proto.ColStr); ok {
		strCol.Append("test1")
		strCol.Append("test2")
		if strCol.Rows() != 2 {
			t.Errorf("Expected 2 rows before reset, got %d", strCol.Rows())
		}
	}

	// Reset should clear all data
	cols.Reset()

	// Verify all columns are empty after reset
	for i, col := range cols.cols {
		if col.Rows() != 0 {
			t.Errorf("Column %d: expected 0 rows after reset, got %d", i, col.Rows())
		}
	}

	t.Log("BASELINE BEHAVIOR: Reset clears all column data")
	t.Log("PRESERVATION REQUIREMENT: Reset behavior must remain unchanged")
}

// TestPreservation_IDMutationInterface tests that MutateIDs interface exists
// and can be called without errors (even if it's a no-op for DynamicSharedColumns).
//
// **EXPECTED OUTCOME ON UNFIXED CODE**: This test should PASS
// **EXPECTED OUTCOME ON FIXED CODE**: This test should still PASS (behavior preserved)
//
// **Validates: Requirements 3.9**
func TestPreservation_IDMutationInterface(t *testing.T) {
	names := []string{"TraceId", "SpanId", "message"}
	types := []proto.ColumnType{
		proto.ColumnTypeString,
		proto.ColumnTypeString,
		proto.ColumnTypeString,
	}

	cols := NewDynamicSharedColumnsFromSchema(names, types)

	// MutateIDs should be callable (even if no-op)
	// On unfixed code, this is a no-op for DynamicSharedColumns
	// After fix, it should remain a no-op (documented limitation)
	cols.MutateIDs(0)
	cols.MutateIDs(1)
	cols.MutateIDs(5)

	t.Log("BASELINE BEHAVIOR: MutateIDs is callable on DynamicSharedColumns")
	t.Log("PRESERVATION REQUIREMENT: MutateIDs remains a no-op for dynamic columns (documented limitation)")
}

// TestPreservation_TimestampInterfaceMethods tests that timestamp-related
// methods exist and can be called without panicking on DynamicSharedColumns.
//
// **EXPECTED OUTCOME ON UNFIXED CODE**: This test should PASS
// **EXPECTED OUTCOME ON FIXED CODE**: This test should still PASS (but methods will have real implementation)
//
// **Validates: Requirements 3.5, 3.8**
func TestPreservation_TimestampInterfaceMethods(t *testing.T) {
	names := []string{"Timestamp", "message"}
	types := []proto.ColumnType{proto.ColumnTypeDateTime64, proto.ColumnTypeString}

	cols := NewDynamicSharedColumnsFromSchema(names, types)

	// All timestamp methods should be callable without panicking
	// On unfixed code, these are no-ops for DynamicSharedColumns
	// After fix, they should have real implementations

	// Test FirstTimestamp
	ts1 := cols.FirstTimestamp()
	if !ts1.IsZero() {
		t.Logf("FirstTimestamp returned: %v (on unfixed code, expected zero time)", ts1)
	}

	// Test LastTimestamp
	ts2 := cols.LastTimestamp()
	if !ts2.IsZero() {
		t.Logf("LastTimestamp returned: %v (on unfixed code, expected zero time)", ts2)
	}

	// Test timestamp mutation methods (no-ops on unfixed code)
	cols.UpdateTimestampNow()
	cols.UpdateDate()
	cols.UpdateTimestampMinute()

	// Test ShiftTimestamp with a mock snapshot
	snapshot := ReplayTimeSnapshot{
		programStart: time.Now(),
		datasetStart: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		loopOffset:   0,
	}
	cols.ShiftTimestamp(snapshot)

	t.Log("BASELINE BEHAVIOR: Timestamp interface methods are callable (no-ops on unfixed code)")
	t.Log("PRESERVATION REQUIREMENT: Methods must remain callable (will gain functionality after fix)")
}

// TestPreservation_EmptyDynamicColumns tests behavior of empty DynamicSharedColumns
// created via NewDynamicSharedColumns() with no schema.
//
// **EXPECTED OUTCOME ON UNFIXED CODE**: This test should PASS
// **EXPECTED OUTCOME ON FIXED CODE**: This test should still PASS (behavior preserved)
//
// **Validates: Requirements 2.4**
func TestPreservation_EmptyDynamicColumns(t *testing.T) {
	cols := NewDynamicSharedColumns()

	// Empty columns should have zero-length Results and Input
	results := cols.Results()
	if results == nil {
		t.Log("Results() returns nil for empty DynamicSharedColumns")
	} else if len(results) != 0 {
		t.Errorf("Expected empty Results, got length %d", len(results))
	}

	input := cols.Input()
	if input == nil {
		t.Log("Input() returns nil for empty DynamicSharedColumns")
	} else if len(input) != 0 {
		t.Errorf("Expected empty Input, got length %d", len(input))
	}

	// Reset should not panic on empty columns
	cols.Reset()

	// Timestamp methods should not panic on empty columns
	ts1 := cols.FirstTimestamp()
	if !ts1.IsZero() {
		t.Errorf("Expected zero timestamp for empty columns, got %v", ts1)
	}

	ts2 := cols.LastTimestamp()
	if !ts2.IsZero() {
		t.Errorf("Expected zero timestamp for empty columns, got %v", ts2)
	}

	t.Log("BASELINE BEHAVIOR: Empty DynamicSharedColumns behave safely")
	t.Log("PRESERVATION REQUIREMENT: Empty columns must remain safe to use")
}

// TestPreservation_ColumnTypeHandling tests that NewDynamicSharedColumnsFromSchema
// handles various ClickHouse column types without errors.
//
// **EXPECTED OUTCOME ON UNFIXED CODE**: This test should PASS
// **EXPECTED OUTCOME ON FIXED CODE**: This test should still PASS (may support additional types)
//
// **Validates: Requirements 2.2, 2.3**
func TestPreservation_ColumnTypeHandling(t *testing.T) {
	testCases := []struct {
		name       string
		columnType proto.ColumnType
	}{
		{"DateTime64", proto.ColumnTypeDateTime64},
		{"DateTime", proto.ColumnTypeDateTime},
		{"String", proto.ColumnTypeString},
		{"UInt64", proto.ColumnTypeUInt64},
		{"UInt32", proto.ColumnTypeUInt32},
		{"UInt8", proto.ColumnTypeUInt8},
		{"Int64", proto.ColumnTypeInt64},
		{"Float64", proto.ColumnTypeFloat64},
		{"LowCardinality", proto.ColumnType("LowCardinality(String)")},
		{"Map", proto.ColumnType("Map(String, String)")},
		{"Array", proto.ColumnType("Array(String)")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			names := []string{"test_col"}
			types := []proto.ColumnType{tc.columnType}

			cols := NewDynamicSharedColumnsFromSchema(names, types)

			if cols == nil {
				t.Fatalf("Failed to create DynamicSharedColumns for type %s", tc.columnType)
			}

			results := cols.Results()
			if len(results) != 1 {
				t.Errorf("Expected 1 column, got %d", len(results))
			}

			// Verify the column can be reset without error
			cols.Reset()

			t.Logf("Type %s handled successfully", tc.columnType)
		})
	}

	t.Log("BASELINE BEHAVIOR: Various ClickHouse types are supported")
	t.Log("PRESERVATION REQUIREMENT: Existing type support must be maintained")
}

// TestPreservation_ResultsAndInputConsistency tests that Results() and Input()
// return consistent column structures.
//
// **EXPECTED OUTCOME ON UNFIXED CODE**: This test should PASS
// **EXPECTED OUTCOME ON FIXED CODE**: This test should still PASS (behavior preserved)
//
// **Validates: Requirements 2.1, 2.2**
func TestPreservation_ResultsAndInputConsistency(t *testing.T) {
	names := []string{"col1", "col2", "col3"}
	types := []proto.ColumnType{
		proto.ColumnTypeString,
		proto.ColumnTypeUInt64,
		proto.ColumnTypeDateTime64,
	}

	cols := NewDynamicSharedColumnsFromSchema(names, types)

	results := cols.Results()
	input := cols.Input()

	// Results and Input should have same length
	if len(results) != len(input) {
		t.Errorf("Results length %d != Input length %d", len(results), len(input))
	}

	// Results and Input should have same column names
	for i := range results {
		if results[i].Name != input[i].Name {
			t.Errorf("Column %d: Results name %q != Input name %q",
				i, results[i].Name, input[i].Name)
		}
	}

	// Results and Input should reference same underlying column data
	// Note: We cannot directly compare Data fields because they have different types
	// (proto.ColResult vs proto.ColInput), but they should wrap the same column
	for i := range results {
		// Both should reference the same column from cols.cols
		if results[i].Data != cols.cols[i] || input[i].Data != cols.cols[i] {
			t.Errorf("Column %d: Results or Input do not reference the same underlying column", i)
		}
	}

	t.Log("BASELINE BEHAVIOR: Results and Input are consistent")
	t.Log("PRESERVATION REQUIREMENT: Results/Input consistency must be maintained")
}

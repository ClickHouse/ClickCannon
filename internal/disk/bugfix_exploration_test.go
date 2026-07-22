package disk

import (
	"clickcannon/internal/block"
	"clickcannon/internal/metrics"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ClickHouse/ch-go/proto"
)

// TestBugConditionExploration_ColumnCountMismatch tests the bug condition where
// the disk worker fails to decode Native format files when the file's column count
// doesn't match the hardcoded expectations (15/16 for logs, 22 for traces).
//
// **CRITICAL**: This test is EXPECTED TO FAIL on unfixed code - failure confirms the bug exists.
// DO NOT attempt to fix the test or the code when it fails.
//
// **Validates: Requirements 1.1, 1.2, 1.3 from bugfix.md**
func TestBugConditionExploration_ColumnCountMismatch(t *testing.T) {
	testCases := []struct {
		name        string
		columnCount int
		description string
	}{
		{
			name:        "72_column_extended_schema",
			columnCount: 72,
			description: "Extended schema with 72 columns (custom logs table)",
		},
		{
			name:        "5_column_minimal_schema",
			columnCount: 5,
			description: "Minimal schema with only 5 essential columns",
		},
		{
			name:        "50_column_arbitrary_schema",
			columnCount: 50,
			description: "Arbitrary 50-column structure for fallback testing",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a temporary Native format file with the specified column count
			testFile := filepath.Join(t.TempDir(), "test_data.native")
			err := createNativeFileWithColumnCount(testFile, tc.columnCount, 10)
			if err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}

			// Create a disk worker configured with hardcoded column expectations
			// This mimics the current buggy behavior where NewLogsSharedColumns
			// expects exactly 15 or 16 columns
			blockCreateFunc := func() block.SharedColumns {
				// Hardcoded to 15 columns (simulating NewLogsSharedColumns behavior)
				return createHardcodedLogsColumns()
			}

			blockPool := block.NewGarbageBlockPool(blockCreateFunc)
			insertQueue := make(chan block.SharedColumns, 1)
			defer close(insertQueue)

			worker := newWorker(
				1,
				testLogger(t),
				dataFile{Path: testFile, Index: 0, LoopIndex: 0, Compressed: false},
				ShiftTimestampNone,
				0,
				blockPool,
				insertQueue,
				metrics.NewDisabledStore(),
				true, // passthrough mode
				block.NewReplayTimeKeeper(testLogger(t)),
			)

			// Run the worker and capture the error
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err = worker.Run(ctx)

			// Expected behavior on UNFIXED code:
			// - For fileColumnCount != hardcodedColumnCount: decode should fail
			// - Error should contain "target: X (columns) != Y (target)"
			// - This confirms the bug exists

			if tc.columnCount != 15 {
				// Bug condition: column count mismatch
				// UNFIXED code should fail here with column count mismatch error
				if err == nil || !errors.Is(err, io.EOF) {
					// The worker may skip blocks with incompatible layout and return normally
					// We need to check if any data was successfully processed
					t.Logf("Worker completed without explicit error for %d columns", tc.columnCount)
					t.Logf("Expected: column count mismatch error 'target: %d (columns) != 15 (target)'", tc.columnCount)
					t.Logf("Actual: worker completed (blocks may have been skipped)")

					// For bug exploration, we document this as a counterexample
					t.Errorf("COUNTEREXAMPLE FOUND: File with %d columns did not produce expected column mismatch error", tc.columnCount)
					t.Errorf("This confirms the bug: hardcoded column expectations prevent processing files with %d columns", tc.columnCount)
				} else {
					t.Logf("Worker returned error: %v", err)
				}
			} else {
				// Control case: 15 columns should work with hardcoded expectations
				if err != nil && !errors.Is(err, io.EOF) {
					t.Errorf("Expected 15-column file to work, but got error: %v", err)
				}
			}
		})
	}
}

// createNativeFileWithColumnCount creates a Native format file with the specified
// number of columns and rows. This simulates data exported from tables with
// varying column counts.
func createNativeFileWithColumnCount(path string, columnCount, rowCount int) error {
	// Create columns based on the column count
	var columns proto.Input
	for i := 0; i < columnCount; i++ {
		col := proto.ColStr{}
		for j := 0; j < rowCount; j++ {
			col.Append("test_value")
		}
		columns = append(columns, proto.InputColumn{
			Name: generateColumnName(i),
			Data: &col,
		})
	}

	// Encode to Native format
	var buf proto.Buffer
	b := proto.Block{Rows: rowCount}
	b.EncodeRawBlock(&buf, 54451, columns)

	// Write to file
	return os.WriteFile(path, buf.Buf, 0644)
}

// createHardcodedLogsColumns creates a SharedColumns instance with hardcoded
// 15-column structure, simulating the current buggy NewLogsSharedColumns behavior.
func createHardcodedLogsColumns() block.SharedColumns {
	// Create a hardcoded 15-column structure
	names := []string{
		"Timestamp", "TraceId", "SpanId", "TraceFlags", "SeverityText",
		"SeverityNumber", "ServiceName", "Body", "ResourceSchemaUrl",
		"ResourceAttributes", "ScopeSchemaUrl", "ScopeName", "ScopeVersion",
		"ScopeAttributes", "LogAttributes",
	}

	types := []proto.ColumnType{
		proto.ColumnTypeDateTime64,
		proto.ColumnTypeString,
		proto.ColumnTypeString,
		proto.ColumnTypeUInt32,
		proto.ColumnTypeString,
		proto.ColumnTypeUInt8,
		proto.ColumnTypeString,
		proto.ColumnTypeString,
		proto.ColumnTypeString,
		proto.ColumnType("Map(LowCardinality(String), String)"),
		proto.ColumnTypeString,
		proto.ColumnTypeString,
		proto.ColumnTypeString,
		proto.ColumnType("Map(LowCardinality(String), String)"),
		proto.ColumnType("Map(LowCardinality(String), String)"),
	}

	return block.NewDynamicSharedColumnsFromSchema(names, types)
}

// testLogger creates a simple logger for tests
func testLogger(t *testing.T) *slog.Logger {
	// Create a no-op logger for tests
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Helper function to generate column names
func generateColumnName(index int) string {
	return []string{
		"col0", "col1", "col2", "col3", "col4", "col5", "col6", "col7", "col8", "col9",
		"col10", "col11", "col12", "col13", "col14", "col15", "col16", "col17", "col18", "col19",
		"col20", "col21", "col22", "col23", "col24", "col25", "col26", "col27", "col28", "col29",
		"col30", "col31", "col32", "col33", "col34", "col35", "col36", "col37", "col38", "col39",
		"col40", "col41", "col42", "col43", "col44", "col45", "col46", "col47", "col48", "col49",
		"col50", "col51", "col52", "col53", "col54", "col55", "col56", "col57", "col58", "col59",
		"col60", "col61", "col62", "col63", "col64", "col65", "col66", "col67", "col68", "col69",
		"col70", "col71", "col72", "col73", "col74", "col75", "col76", "col77", "col78", "col79",
	}[index%80]
}

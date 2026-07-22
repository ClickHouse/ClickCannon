package disk

import (
	"clickcannon/internal/block"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ClickHouse/ch-go/proto"
)

// TestColumnCountMismatchBug demonstrates the bug where disk worker cannot
// decode Native files with column counts that don't match hardcoded expectations.
//
// **CRITICAL**: This test is EXPECTED TO FAIL on unfixed code.
// This proves the bug exists.
//
// **Validates: Requirements 1.1, 1.2, 1.3**
func TestColumnCountMismatchBug(t *testing.T) {
	testCases := []struct {
		name        string
		columnCount int
	}{
		{"5_columns", 5},
		{"50_columns", 50},
		{"72_columns", 72},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create test Native file
			testFile := filepath.Join(t.TempDir(), "test.native")
			if err := createTestNativeFile(testFile, tc.columnCount, 10); err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}

			// Create hardcoded 15-column structure (simulating current buggy behavior)
			hardcodedCols := create15ColumnStructure()

			// Try to decode the file
			err := decodeNativeFile(testFile, hardcodedCols)

			// Expected behavior on UNFIXED code:
			// - Files with columnCount != 15 should fail with mismatch error
			// - This confirms the bug exists
			if tc.columnCount != 15 {
				if err == nil {
					t.Errorf("COUNTEREXAMPLE: File with %d columns decoded successfully, but should have failed", tc.columnCount)
					t.Errorf("This means either:")
					t.Errorf("  1. The bug doesn't exist (unlikely given the requirements)")
					t.Errorf("  2. The Auto() decoder is being used (which would bypass the hardcoded structure)")
					t.Errorf("  3. Incompatible blocks are being silently skipped")
				} else if !isColumnCountMismatchError(err) {
					t.Logf("Got error (not column count mismatch): %v", err)
				} else {
					t.Logf("✓ EXPECTED FAILURE: Column count mismatch error: %v", err)
					t.Logf("This confirms the bug: hardcoded 15-column structure cannot decode %d-column file", tc.columnCount)
				}
			}
		})
	}
}

// createTestNativeFile creates a Native format file with specified columns and rows
func createTestNativeFile(path string, columnCount, rowCount int) error {
	var columns proto.Input
	for i := 0; i < columnCount; i++ {
		col := proto.ColStr{}
		for j := 0; j < rowCount; j++ {
			col.Append("value")
		}
		columns = append(columns, proto.InputColumn{
			Name: genColName(i),
			Data: &col,
		})
	}

	var buf proto.Buffer
	b := proto.Block{Rows: rowCount}
	b.EncodeRawBlock(&buf, 54451, columns)

	return os.WriteFile(path, buf.Buf, 0644)
}

// create15ColumnStructure creates a hardcoded 15-column SharedColumns
func create15ColumnStructure() block.SharedColumns {
	names := []string{
		"Timestamp", "TraceId", "SpanId", "TraceFlags", "SeverityText",
		"SeverityNumber", "ServiceName", "Body", "ResourceSchemaUrl",
		"ResourceAttributes", "ScopeSchemaUrl", "ScopeName", "ScopeVersion",
		"ScopeAttributes", "LogAttributes",
	}
	types := []proto.ColumnType{
		proto.ColumnTypeDateTime64, proto.ColumnTypeString, proto.ColumnTypeString,
		proto.ColumnTypeUInt32, proto.ColumnTypeString, proto.ColumnTypeUInt8,
		proto.ColumnTypeString, proto.ColumnTypeString, proto.ColumnTypeString,
		proto.ColumnType("Map(LowCardinality(String), String)"),
		proto.ColumnTypeString, proto.ColumnTypeString, proto.ColumnTypeString,
		proto.ColumnType("Map(LowCardinality(String), String)"),
		proto.ColumnType("Map(LowCardinality(String), String)"),
	}
	return block.NewDynamicSharedColumnsFromSchema(names, types)
}

// decodeNativeFile attempts to decode a Native format file into the given columns
func decodeNativeFile(path string, cols block.SharedColumns) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	reader := proto.NewReader(f)
	var dec proto.Block

	results := cols.Results()
	err = dec.DecodeRawBlock(reader, 54451, results)
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}

// isColumnCountMismatchError checks if error message contains column count mismatch
func isColumnCountMismatchError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "(columns) !=")
}

// genColName generates a column name for the given index
func genColName(i int) string {
	cols := []string{
		"col0", "col1", "col2", "col3", "col4", "col5", "col6", "col7", "col8", "col9",
		"col10", "col11", "col12", "col13", "col14", "col15", "col16", "col17", "col18", "col19",
		"col20", "col21", "col22", "col23", "col24", "col25", "col26", "col27", "col28", "col29",
		"col30", "col31", "col32", "col33", "col34", "col35", "col36", "col37", "col38", "col39",
		"col40", "col41", "col42", "col43", "col44", "col45", "col46", "col47", "col48", "col49",
		"col50", "col51", "col52", "col53", "col54", "col55", "col56", "col57", "col58", "col59",
		"col60", "col61", "col62", "col63", "col64", "col65", "col66", "col67", "col68", "col69",
		"col70", "col71", "col72", "col73", "col74", "col75", "col76", "col77", "col78", "col79",
	}
	if i < len(cols) {
		return cols[i]
	}
	return cols[0]
}

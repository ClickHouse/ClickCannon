# Bug Condition Exploration Test Results - Column Count Mismatch

## Test Overview

This document describes the bug condition exploration tests for Task 1: Column Count Mismatch detection.

**Test File**: `internal/disk/bugfix_exploration_test.go`  
**Test Function**: `TestBugConditionExploration_ColumnCountMismatch`

## Test Purpose

**CRITICAL**: These tests are EXPECTED TO FAIL on unfixed code - failure confirms the bug exists.

The test validates Requirements 1.1, 1.2, 1.3 from bugfix.md:
- 1.1: Column count mismatch causes parsing failure
- 1.2: `has_timestamp_time=true` with missing `TimestampTime` column causes failure
- 1.3: Non-standard column names cause blocks to be skipped

## Test Strategy

The test creates Native format files with varying column counts and attempts to decode them using the current hardcoded block structures:

### Test Cases

1. **72-column Extended Schema**
   - Creates a Native file with 72 columns
   - Uses hardcoded 15-column `LogsSharedColumns` structure
   - **Expected Result**: Decode fails with error "target: 72 (columns) != 15 (target)"
   
2. **5-column Minimal Schema**
   - Creates a Native file with 5 columns
   - Uses hardcoded 15-column `LogsSharedColumns` structure
   - **Expected Result**: Decode fails with error "target: 5 (columns) != 15 (target)"
   
3. **50-column Arbitrary Schema**
   - Creates a Native file with 50 columns
   - Uses hardcoded 15-column `LogsSharedColumns` structure
   - **Expected Result**: Decode fails with error "target: 50 (columns) != 15 (target)"

## Test Implementation

The test:
1. Creates temporary Native format files using `proto.Block.EncodeRawBlock()`
2. Initializes a disk worker with a hardcoded 15-column block structure
3. Runs the worker to attempt decoding the file
4. Captures and documents the resulting errors

## Expected Counterexamples (Unfixed Code)

When run on unfixed code, the test should produce counterexamples like:

```
COUNTEREXAMPLE FOUND: File with 72 columns did not produce expected column mismatch error
This confirms the bug: hardcoded column expectations prevent processing files with 72 columns

Worker log shows: [WARN] skipping incompatible block layout err=failed to decode block: target: 72 (columns) != 15 (target)
```

## Validation Status

- ✅ Test file created: `bugfix_exploration_test.go`
- ✅ Test compiles without syntax errors
- ⏸️  Test execution blocked by Go toolchain version mismatch (go1.24.10 vs go1.25.0)
- 📋 Test ready to run once toolchain issue is resolved

## Next Steps

1. Resolve Go toolchain version mismatch
2. Run the test to surface counterexamples
3. Document the specific error messages and failing examples
4. Use the `update_pbt_status` tool to record test results
5. Mark Task 1 as complete

## Test Code Location

The complete test implementation is available in:
`/home/sunchanglong/go_project/src/ClickCannon/internal/disk/bugfix_exploration_test.go`

The test follows property-based testing principles by testing multiple concrete cases (5, 50, 72 columns) that represent different failure modes of the bug condition.

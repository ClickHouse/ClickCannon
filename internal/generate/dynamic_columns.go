package generate

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"clickcannon/internal/block"

	"github.com/ClickHouse/ch-go/proto"
)

// DynamicColumns implements block.SharedColumns for custom field configurations.
type DynamicColumns struct {
	fieldNames        []string
	columns           []proto.Column
	generators        []Gen
	arrayGens         []*ArrayGen           // For array fields
	isArrayField      []bool                 // Track which fields are arrays
	nestedArrayMgr    *NestedArrayManager   // Manages Nested structure arrays
	cachedInput       proto.Input
}

// NewDynamicColumns creates a DynamicColumns from a CustomFieldsConfig.
func NewDynamicColumns(config *CustomFieldsConfig) (*DynamicColumns, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	
	if len(config.CustomFields) == 0 {
		return nil, fmt.Errorf("no custom fields defined")
	}
	
	dc := &DynamicColumns{
		fieldNames:     make([]string, 0, len(config.CustomFields)),
		columns:        make([]proto.Column, 0, len(config.CustomFields)),
		generators:     make([]Gen, 0, len(config.CustomFields)),
		arrayGens:      make([]*ArrayGen, 0, len(config.CustomFields)),
		isArrayField:   make([]bool, 0, len(config.CustomFields)),
		nestedArrayMgr: NewNestedArrayManager(),
	}
	
	// Use fieldOrder if available, otherwise iterate map
	fieldsToProcess := config.fieldOrder
	if len(fieldsToProcess) == 0 {
		fieldsToProcess = make([]string, 0, len(config.CustomFields))
		for name := range config.CustomFields {
			fieldsToProcess = append(fieldsToProcess, name)
		}
	}
	
	// Build columns in order
	for _, fieldName := range fieldsToProcess {
		fieldCfg, exists := config.CustomFields[fieldName]
		if !exists {
			continue
		}
		
		// Check if this is an array field
		if fieldCfg.IsArray && fieldCfg.ArrayConfig != nil {
			// Create array column
			col, err := createArrayColumnForType(fieldCfg.Type, fieldCfg.ClickHouseType)
			if err != nil {
				return nil, fmt.Errorf("failed to create array column %s: %w", fieldName, err)
			}
			
			// Create array generator
			arrayGen, err := BuildArrayGenerator(*fieldCfg.ArrayConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to create array generator for %s: %w", fieldName, err)
			}
			
			// Register with nested array manager if it's a nested field
			dc.nestedArrayMgr.RegisterArrayField(fieldName, arrayGen)
			
			dc.fieldNames = append(dc.fieldNames, fieldName)
			dc.columns = append(dc.columns, col)
			dc.generators = append(dc.generators, nil) // No regular generator for array fields
			dc.arrayGens = append(dc.arrayGens, arrayGen)
			dc.isArrayField = append(dc.isArrayField, true)
		} else {
			// Regular field
			// Create column based on type
			col, err := createColumnForType(fieldCfg.Type, fieldCfg.ClickHouseType)
			if err != nil {
				return nil, fmt.Errorf("failed to create column %s: %w", fieldName, err)
			}
			
			// Create generator
			gen, err := BuildGeneratorFromConfig(fieldCfg.Generator)
			if err != nil {
				return nil, fmt.Errorf("failed to create generator for %s: %w", fieldName, err)
			}
			
			dc.fieldNames = append(dc.fieldNames, fieldName)
			dc.columns = append(dc.columns, col)
			dc.generators = append(dc.generators, gen)
			dc.arrayGens = append(dc.arrayGens, nil)
			dc.isArrayField = append(dc.isArrayField, false)
		}
	}
	
	if len(dc.fieldNames) == 0 {
		return nil, fmt.Errorf("no fields were processed")
	}
	
	// Build cached Input
	dc.rebuildInput()
	
	return dc, nil
}

// createColumnForType creates a proto.Column based on type string.
func createColumnForType(typeName, chType string) (proto.Column, error) {
	switch typeName {
	case "string":
		if chType == "LowCardinality(String)" || chType == "LowCardinality(Nullable(String))" {
			return newGenLowCardinality(), nil
		}
		return &proto.ColStr{}, nil
	
	case "int64":
		return &proto.ColInt64{}, nil
	
	case "int32":
		return &proto.ColInt32{}, nil
	
	case "int16":
		return &proto.ColInt16{}, nil
	
	case "int8":
		return &proto.ColInt8{}, nil
	
	case "uint64":
		return &proto.ColUInt64{}, nil
	
	case "uint32":
		return &proto.ColUInt32{}, nil
	
	case "uint16":
		return &proto.ColUInt16{}, nil
	
	case "uint8":
		return &proto.ColUInt8{}, nil
	
	case "float64":
		return &proto.ColFloat64{}, nil
	
	case "float32":
		return &proto.ColFloat32{}, nil
	
	case "bool":
		return &proto.ColBool{}, nil
	
	case "datetime":
		return &proto.ColDateTime{Location: time.UTC}, nil
	
	case "datetime64":
		// Parse precision from ClickHouse type if provided
		// Default to nanosecond precision
		return &proto.ColDateTime64Raw{
			ColDateTime64: proto.ColDateTime64{
				Location:     time.UTC,
				Precision:    proto.PrecisionNano,
				PrecisionSet: true,
			},
		}, nil
	
	case "map":
		// Map(String, String)
		return newGenMap(), nil
	
	default:
		return nil, fmt.Errorf("unsupported type: %s", typeName)
	}
}

// createArrayColumnForType creates array columns
func createArrayColumnForType(typeName, chType string) (proto.Column, error) {
	switch typeName {
	case "array_string":
		return &proto.ColArr[string]{Data: &proto.ColStr{}}, nil
	
	case "array_uint8":
		return &proto.ColArr[uint8]{Data: &proto.ColUInt8{}}, nil
	
	case "array_uint16":
		return &proto.ColArr[uint16]{Data: &proto.ColUInt16{}}, nil
	
	case "array_uint32":
		return &proto.ColArr[uint32]{Data: &proto.ColUInt32{}}, nil
	
	case "array_uint64":
		return &proto.ColArr[uint64]{Data: &proto.ColUInt64{}}, nil
	
	case "array_int8":
		return &proto.ColArr[int8]{Data: &proto.ColInt8{}}, nil
	
	case "array_int16":
		return &proto.ColArr[int16]{Data: &proto.ColInt16{}}, nil
	
	case "array_int32":
		return &proto.ColArr[int32]{Data: &proto.ColInt32{}}, nil
	
	case "array_int64":
		return &proto.ColArr[int64]{Data: &proto.ColInt64{}}, nil
	
	default:
		return nil, fmt.Errorf("unsupported array type: %s", typeName)
	}
}

// rebuildInput creates the proto.Input for ClickHouse.
func (dc *DynamicColumns) rebuildInput() {
	dc.cachedInput = make(proto.Input, len(dc.fieldNames))
	for i := range dc.fieldNames {
		dc.cachedInput[i] = proto.InputColumn{
			Name: dc.fieldNames[i],
			Data: dc.columns[i],
		}
	}
}

// Reset resets all columns.
func (dc *DynamicColumns) Reset() {
	for _, col := range dc.columns {
		col.Reset()
	}
}

// Input returns the proto.Input for ClickHouse insertion.
func (dc *DynamicColumns) Input() proto.Input {
	return dc.cachedInput
}

// Results returns nil (not used for generation).
func (dc *DynamicColumns) Results() proto.Results {
	return nil
}

// appendArrayToColumn appends array values to the appropriate column type.
func appendArrayToColumn(col proto.Column, values []string, fieldName string) error {
	// Only filter empty strings for non-nested arrays (independent arrays like contentTags, xForwardIp)
	// Nested arrays must keep same size across all fields in the group
	isNestedArray := false
	for i := 0; i < len(fieldName); i++ {
		if fieldName[i] == '.' {
			isNestedArray = true
			break
		}
	}
	
	filteredValues := values
	if !isNestedArray {
		// For independent arrays, filter out empty strings
		temp := make([]string, 0, len(values))
		for _, v := range values {
			if v != "" {
				temp = append(temp, v)
			}
		}
		filteredValues = temp
	}
	
	switch c := col.(type) {
	case *proto.ColArr[string]:
		c.Append(filteredValues)
		return nil
	
	case *proto.ColArr[uint8]:
		arr := make([]uint8, len(filteredValues))
		for i, v := range filteredValues {
			val, err := strconv.ParseUint(v, 10, 8)
			if err != nil {
				return fmt.Errorf("field %s[%d]: failed to parse uint8: %w", fieldName, i, err)
			}
			arr[i] = uint8(val)
		}
		c.Append(arr)
		return nil
	
	case *proto.ColArr[uint16]:
		arr := make([]uint16, len(filteredValues))
		for i, v := range filteredValues {
			val, err := strconv.ParseUint(v, 10, 16)
			if err != nil {
				return fmt.Errorf("field %s[%d]: failed to parse uint16: %w", fieldName, i, err)
			}
			arr[i] = uint16(val)
		}
		c.Append(arr)
		return nil
	
	case *proto.ColArr[uint32]:
		arr := make([]uint32, len(filteredValues))
		for i, v := range filteredValues {
			val, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				return fmt.Errorf("field %s[%d]: failed to parse uint32: %w", fieldName, i, err)
			}
			arr[i] = uint32(val)
		}
		c.Append(arr)
		return nil
	
	case *proto.ColArr[uint64]:
		arr := make([]uint64, len(filteredValues))
		for i, v := range filteredValues {
			val, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return fmt.Errorf("field %s[%d]: failed to parse uint64: %w", fieldName, i, err)
			}
			arr[i] = val
		}
		c.Append(arr)
		return nil
	
	case *proto.ColArr[int8]:
		arr := make([]int8, len(filteredValues))
		for i, v := range filteredValues {
			val, err := strconv.ParseInt(v, 10, 8)
			if err != nil {
				return fmt.Errorf("field %s[%d]: failed to parse int8: %w", fieldName, i, err)
			}
			arr[i] = int8(val)
		}
		c.Append(arr)
		return nil
	
	case *proto.ColArr[int16]:
		arr := make([]int16, len(filteredValues))
		for i, v := range filteredValues {
			val, err := strconv.ParseInt(v, 10, 16)
			if err != nil {
				return fmt.Errorf("field %s[%d]: failed to parse int16: %w", fieldName, i, err)
			}
			arr[i] = int16(val)
		}
		c.Append(arr)
		return nil
	
	case *proto.ColArr[int32]:
		arr := make([]int32, len(filteredValues))
		for i, v := range filteredValues {
			val, err := strconv.ParseInt(v, 10, 32)
			if err != nil {
				return fmt.Errorf("field %s[%d]: failed to parse int32: %w", fieldName, i, err)
			}
			arr[i] = int32(val)
		}
		c.Append(arr)
		return nil
	
	case *proto.ColArr[int64]:
		arr := make([]int64, len(filteredValues))
		for i, v := range filteredValues {
			val, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return fmt.Errorf("field %s[%d]: failed to parse int64: %w", fieldName, i, err)
			}
			arr[i] = val
		}
		c.Append(arr)
		return nil
	
	default:
		return fmt.Errorf("field %s: unsupported array column type %T", fieldName, col)
	}
}
// FirstTimestamp returns the first timestamp (not applicable for dynamic columns).
func (dc *DynamicColumns) FirstTimestamp() time.Time {
	return time.Time{}
}

// LastTimestamp returns the last timestamp (not applicable for dynamic columns).
func (dc *DynamicColumns) LastTimestamp() time.Time {
	return time.Time{}
}

// UpdateDate is not applicable for dynamic columns.
func (dc *DynamicColumns) UpdateDate() {}

// ShiftTimestamp is not applicable for dynamic columns.
func (dc *DynamicColumns) ShiftTimestamp(_ block.ReplayTimeSnapshot) {}

// UpdateTimestampNow is not applicable for dynamic columns.
func (dc *DynamicColumns) UpdateTimestampNow() {}

// UpdateTimestampMinute is not applicable for dynamic columns.
func (dc *DynamicColumns) UpdateTimestampMinute() {}

// MutateIDs is not applicable for dynamic columns.
func (dc *DynamicColumns) MutateIDs(_ int) {}

// DynamicFiller fills DynamicColumns with generated data.
type DynamicFiller struct {
	config *CustomFieldsConfig
}

// NewDynamicFiller creates a new DynamicFiller.
func NewDynamicFiller(config *CustomFieldsConfig) *DynamicFiller {
	return &DynamicFiller{config: config}
}

// Fill populates the DynamicColumns with n rows of data.
func (f *DynamicFiller) Fill(ctx context.Context, rng *Rng, cols *DynamicColumns, n int) int {
	for i := 0; i < n; i++ {
		// Check context cancellation periodically
		if i&0xFF == 0 {
			select {
			case <-ctx.Done():
				return i
			default:
			}
		}
		
		// Track which nested groups have been generated for this row
		generatedNestedGroups := make(map[string]map[string][]string)
		
		// Generate value for each field
		for j := range cols.fieldNames {
			fieldName := cols.fieldNames[j]
			
			if cols.isArrayField[j] {
				// Check if this is a nested field
				if cols.nestedArrayMgr.IsNestedField(fieldName) {
					groupName := cols.nestedArrayMgr.GetGroupName(fieldName)
					
					// Generate all fields in this nested group once
					if _, exists := generatedNestedGroups[groupName]; !exists {
						generatedNestedGroups[groupName] = cols.nestedArrayMgr.GenerateSynchronizedArrays(groupName, rng)
					}
					
					// Get the pre-generated array for this specific field
					arrayValues := generatedNestedGroups[groupName][fieldName]
					if err := appendArrayToColumn(cols.columns[j], arrayValues, fieldName); err != nil {
						// Log error but continue
						continue
					}
				} else {
					// Regular (non-nested) array field
					arrayValues := cols.arrayGens[j].Generate(rng)
					if err := appendArrayToColumn(cols.columns[j], arrayValues, fieldName); err != nil {
						// Log error but continue
						continue
					}
				}
			} else {
				// Regular field
				value := cols.generators[j].Generate(rng)
				if err := appendToColumn(cols.columns[j], value, fieldName); err != nil {
					// Log error but continue
					continue
				}
			}
		}
	}
	return n
}

// appendToColumn appends a string value to the appropriate column type.
func appendToColumn(col proto.Column, value string, fieldName string) error {
	switch c := col.(type) {
	case *proto.ColStr:
		c.Append(value)
	
	case *proto.ColLowCardinality[string]:
		c.Append(value)
	
	case *proto.ColInt64:
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("field %s: failed to parse int64: %w", fieldName, err)
		}
		*c = append(*c, v)
	
	case *proto.ColInt32:
		v, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return fmt.Errorf("field %s: failed to parse int32: %w", fieldName, err)
		}
		*c = append(*c, int32(v))
	
	case *proto.ColInt16:
		v, err := strconv.ParseInt(value, 10, 16)
		if err != nil {
			return fmt.Errorf("field %s: failed to parse int16: %w", fieldName, err)
		}
		*c = append(*c, int16(v))
	
	case *proto.ColInt8:
		v, err := strconv.ParseInt(value, 10, 8)
		if err != nil {
			return fmt.Errorf("field %s: failed to parse int8: %w", fieldName, err)
		}
		*c = append(*c, int8(v))
	
	case *proto.ColUInt64:
		v, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("field %s: failed to parse uint64: %w", fieldName, err)
		}
		*c = append(*c, v)
	
	case *proto.ColUInt32:
		v, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return fmt.Errorf("field %s: failed to parse uint32: %w", fieldName, err)
		}
		*c = append(*c, uint32(v))
	
	case *proto.ColUInt16:
		v, err := strconv.ParseUint(value, 10, 16)
		if err != nil {
			return fmt.Errorf("field %s: failed to parse uint16: %w", fieldName, err)
		}
		*c = append(*c, uint16(v))
	
	case *proto.ColUInt8:
		v, err := strconv.ParseUint(value, 10, 8)
		if err != nil {
			return fmt.Errorf("field %s: failed to parse uint8: %w", fieldName, err)
		}
		*c = append(*c, uint8(v))
	
	case *proto.ColFloat64:
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("field %s: failed to parse float64: %w", fieldName, err)
		}
		*c = append(*c, v)
	
	case *proto.ColFloat32:
		v, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return fmt.Errorf("field %s: failed to parse float32: %w", fieldName, err)
		}
		*c = append(*c, float32(v))
	
	case *proto.ColBool:
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("field %s: failed to parse bool: %w", fieldName, err)
		}
		*c = append(*c, v)
	
	case *proto.ColDateTime:
		// Parse as unix timestamp
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("field %s: failed to parse datetime: %w", fieldName, err)
		}
		c.Append(time.Unix(v, 0))
	
	case *proto.ColDateTime64Raw:
		// Parse as unix timestamp (nano precision)
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("field %s: failed to parse datetime64: %w", fieldName, err)
		}
		c.Data = append(c.Data, proto.ToDateTime64(time.Unix(v, 0), c.Precision))
	
	default:
		return fmt.Errorf("field %s: unsupported column type %T", fieldName, col)
	}
	
	return nil
}

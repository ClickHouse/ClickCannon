package generate

// NestedArrayManager ensures all arrays in a Nested structure have the same size
type NestedArrayManager struct {
	groups map[string]*NestedArrayGroup // key: nested structure name (e.g., "otherQueries")
}

// NestedArrayGroup tracks array generators for a single Nested structure
type NestedArrayGroup struct {
	fieldNames []string    // e.g., ["otherQueries.name", "otherQueries.class", "otherQueries.type"]
	arrayGens  []*ArrayGen // Generators for each field
	minSize    int
	maxSize    int
}

// NewNestedArrayManager creates a new manager
func NewNestedArrayManager() *NestedArrayManager {
	return &NestedArrayManager{
		groups: make(map[string]*NestedArrayGroup),
	}
}

// RegisterArrayField registers a field belonging to a Nested structure
func (m *NestedArrayManager) RegisterArrayField(fieldName string, arrayGen *ArrayGen) {
	// Extract nested structure name (e.g., "otherQueries" from "otherQueries.name")
	groupName := extractNestedGroupName(fieldName)
	if groupName == "" {
		// Not a nested field, ignore
		return
	}
	
	group, exists := m.groups[groupName]
	if !exists {
		group = &NestedArrayGroup{
			fieldNames: []string{},
			arrayGens:  []*ArrayGen{},
			minSize:    arrayGen.minSize,
			maxSize:    arrayGen.maxSize,
		}
		m.groups[groupName] = group
	}
	
	// Ensure all fields in the group have the same size range
	if arrayGen.minSize < group.minSize {
		group.minSize = arrayGen.minSize
	}
	if arrayGen.maxSize > group.maxSize {
		group.maxSize = arrayGen.maxSize
	}
	
	group.fieldNames = append(group.fieldNames, fieldName)
	group.arrayGens = append(group.arrayGens, arrayGen)
}

// GenerateSynchronizedArrays generates arrays for a Nested structure with synchronized sizes
func (m *NestedArrayManager) GenerateSynchronizedArrays(groupName string, rng *Rng) map[string][]string {
	group, exists := m.groups[groupName]
	if !exists {
		return nil
	}
	
	// Decide array size once for all fields in this group
	size := group.minSize
	if group.maxSize > group.minSize {
		size = group.minSize + rng.IntN(group.maxSize-group.minSize+1)
	}
	
	result := make(map[string][]string)
	for i, fieldName := range group.fieldNames {
		// Generate exactly 'size' elements for this field
		arr := make([]string, size)
		for j := 0; j < size; j++ {
			arr[j] = group.arrayGens[i].elemGen.Generate(rng)
		}
		result[fieldName] = arr
	}
	
	return result
}

// IsNestedField checks if a field belongs to a Nested structure
func (m *NestedArrayManager) IsNestedField(fieldName string) bool {
	groupName := extractNestedGroupName(fieldName)
	return groupName != "" && m.groups[groupName] != nil
}

// GetGroupName extracts the nested group name from a field name
func (m *NestedArrayManager) GetGroupName(fieldName string) string {
	return extractNestedGroupName(fieldName)
}

// extractNestedGroupName extracts "otherQueries" from "otherQueries.name"
func extractNestedGroupName(fieldName string) string {
	// Check if this looks like a nested field (contains a dot)
	for i := 0; i < len(fieldName); i++ {
		if fieldName[i] == '.' {
			return fieldName[:i]
		}
	}
	return ""
}

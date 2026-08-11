package generate

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

// CustomFieldsConfig represents the configuration for custom field generation.
type CustomFieldsConfig struct {
	Version        string                   `yaml:"version"`
	DataType       string                   `yaml:"data_type"`
	BaseProfile    string                   `yaml:"base_profile"`
	CustomFields   map[string]FieldConfig   `yaml:"custom_fields"`
	OverrideFields map[string]FieldOverride `yaml:"override_fields"`
	
	// Ordered field names (filled after parsing)
	fieldOrder []string
}

// FieldConfig defines how a single field should be generated.
type FieldConfig struct {
	Type           string             `yaml:"type"`             // string, int64, int32, uint64, uint32, uint16, uint8, float64, bool, map
	ClickHouseType string             `yaml:"clickhouse_type"`  // Optional: LowCardinality(String), DateTime, Array(String), etc.
	Generator      GeneratorConfig    `yaml:"generator"`
	ArrayConfig    *ArrayFieldConfig  `yaml:"array_config,omitempty"` // For array fields
	IsArray        bool               `yaml:"is_array,omitempty"`      // Mark as array field
}

// FieldOverride allows overriding base profile fields.
type FieldOverride struct {
	Generator GeneratorConfig `yaml:"generator"`
}

// GeneratorConfig defines the generation strategy for a field.
type GeneratorConfig struct {
	Type   string `yaml:"type"` // const, pool, uuid, random_string, hex, int_range, float_range, ip_v4, timestamp, etc.
	
	// Common parameters
	Value  interface{}   `yaml:"value"`   // For const
	Values []interface{} `yaml:"values"`  // For pool
	
	// Numeric range parameters
	Min interface{} `yaml:"min"`
	Max interface{} `yaml:"max"`
	
	// Pool with weights
	Weights []float64 `yaml:"weights"`
	
	// String parameters
	Length  int    `yaml:"length"`
	Charset string `yaml:"charset"` // hex, alphanumeric, alpha
	Prefix  string `yaml:"prefix"`
	
	// Float parameters
	Precision int `yaml:"precision"`
	
	// Timestamp parameters
	Format    string `yaml:"format"`     // unix_sec, unix_milli, unix_nano, rfc3339
	OffsetSec int64  `yaml:"offset_sec"` // Time offset in seconds
	OffsetMs  int64  `yaml:"offset_ms"`  // Time offset in milliseconds
	
	// IP parameters
	IPFormat string `yaml:"ip_format"` // dotted, uint32, hex
	
	// Weighted pool (alternative to pool + weights)
	WeightedValues []WeightedValue `yaml:"weighted_values"`
	
	// Map parameters
	Keys []MapKeyConfig `yaml:"keys"`
	
	// Conditional parameters
	Condition *ConditionConfig `yaml:"condition"`
	IfTrue    *GeneratorConfig `yaml:"if_true"`
	IfFalse   *GeneratorConfig `yaml:"if_false"`
	
	// Template parameters
	Template string   `yaml:"template"`
	Fields   []string `yaml:"fields"`
}

// WeightedValue represents a value with its weight.
type WeightedValue struct {
	Value  interface{} `yaml:"value"`
	Weight float64     `yaml:"weight"`
}

// ArrayFieldConfig defines how to generate array fields
type ArrayFieldConfig struct {
	MinSize int             `yaml:"min_size"` // Minimum array length
	MaxSize int             `yaml:"max_size"` // Maximum array length
	Elements GeneratorConfig `yaml:"elements"` // Generator for array elements
}

// MapKeyConfig defines how to generate a map key-value pair.
type MapKeyConfig struct {
	Key             string          `yaml:"key"`
	Probability     float64         `yaml:"probability"`
	ValueGenerator  GeneratorConfig `yaml:"value_generator"`
}

// ConditionConfig defines a conditional check.
type ConditionConfig struct {
	Field    string      `yaml:"field"`
	Operator string      `yaml:"operator"` // ==, !=, <, >, <=, >=
	Value    interface{} `yaml:"value"`
}

// LoadCustomFieldsConfig loads configuration from a YAML file.
func LoadCustomFieldsConfig(path string) (*CustomFieldsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	
	var config CustomFieldsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	
	// Validate basic structure
	if config.Version == "" {
		return nil, fmt.Errorf("version field is required")
	}
	
	if config.DataType == "" {
		return nil, fmt.Errorf("data_type field is required")
	}
	
	if len(config.CustomFields) == 0 && len(config.OverrideFields) == 0 {
		return nil, fmt.Errorf("at least one custom_field or override_field is required")
	}
	
	// Build ordered field names (deterministic order by sorting)
	config.fieldOrder = make([]string, 0, len(config.CustomFields))
	for name := range config.CustomFields {
		config.fieldOrder = append(config.fieldOrder, name)
	}
	// Sort for deterministic ordering
	// Note: This ensures consistent column order across runs
	// Comment out sort if you want to preserve YAML order
	// sort.Strings(config.fieldOrder)
	
	return &config, nil
}

// ValidateGeneratorConfig validates a generator configuration.
func ValidateGeneratorConfig(cfg GeneratorConfig) error {
	switch cfg.Type {
	case "const":
		if cfg.Value == nil {
			return fmt.Errorf("const generator requires 'value' field")
		}
	case "pool":
		if len(cfg.Values) == 0 {
			return fmt.Errorf("pool generator requires non-empty 'values' field")
		}
	case "weighted_pool":
		// Relaxed validation - we'll check in the factory
		if len(cfg.WeightedValues) == 0 && len(cfg.Values) == 0 {
			return fmt.Errorf("weighted_pool generator requires 'values' or 'weighted_values'")
		}
	case "int_range":
		if cfg.Min == nil || cfg.Max == nil {
			return fmt.Errorf("int_range generator requires 'min' and 'max' fields")
		}
	case "float_range":
		if cfg.Min == nil || cfg.Max == nil {
			return fmt.Errorf("float_range generator requires 'min' and 'max' fields")
		}
	case "random_string", "hex":
		if cfg.Length <= 0 {
			return fmt.Errorf("%s generator requires positive 'length' field", cfg.Type)
		}
	case "uuid", "ip_v4", "timestamp":
		// No specific validation needed
	default:
		return fmt.Errorf("unknown generator type: %s", cfg.Type)
	}
	return nil
}

package generate

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"
)

// BuildGeneratorFromConfig creates a Gen from a GeneratorConfig.
func BuildGeneratorFromConfig(cfg GeneratorConfig) (Gen, error) {
	if err := ValidateGeneratorConfig(cfg); err != nil {
		return nil, err
	}
	
	switch cfg.Type {
	case "const":
		return Const(fmt.Sprint(cfg.Value)), nil
	
	case "pool":
		strVals := make([]string, len(cfg.Values))
		for i, v := range cfg.Values {
			strVals[i] = fmt.Sprint(v)
		}
		
		// If weights provided, use weighted pool
		if len(cfg.Weights) > 0 {
			return buildWeightedPool(strVals, cfg.Weights)
		}
		
		return Pool(strVals...), nil
	
	case "weighted_pool":
		// Support three formats:
		// 1. weighted_values: [{value: x, weight: y}]
		// 2. values: [x, y] + weights: [w1, w2] (simple arrays)
		// 3. values: [{value: x, weight: y}] (inline weights)
		
		var values []string
		var weights []float64
		
		if len(cfg.WeightedValues) > 0 {
			// Format 1: weighted_values field
			values = make([]string, len(cfg.WeightedValues))
			weights = make([]float64, len(cfg.WeightedValues))
			for i, wv := range cfg.WeightedValues {
				values[i] = fmt.Sprint(wv.Value)
				weights[i] = wv.Weight
			}
		} else if len(cfg.Values) > 0 {
			// Check if Values contains maps (Format 3) or simple values (Format 2)
			if len(cfg.Values) > 0 {
				// Try to parse first value
				firstVal := cfg.Values[0]
				if _, ok := firstVal.(map[string]interface{}); ok {
					// Format 3: values is array of {value, weight} maps
					values = make([]string, len(cfg.Values))
					weights = make([]float64, len(cfg.Values))
					for i, v := range cfg.Values {
						if m, ok := v.(map[string]interface{}); ok {
							if val, ok := m["value"]; ok {
								values[i] = fmt.Sprint(val)
							}
							// Handle both int and float64 for weight
							weightParsed := false
							if w, ok := m["weight"].(float64); ok {
								weights[i] = w
								weightParsed = true
							} else if w, ok := m["weight"].(int); ok {
								weights[i] = float64(w)
								weightParsed = true
							} else if w, ok := m["weight"].(int64); ok {
								weights[i] = float64(w)
								weightParsed = true
							}
							
							if !weightParsed {
								// Try generic conversion
								if wVal, exists := m["weight"]; exists {
									weights[i] = toFloat64(wVal)
								}
							}
						}
					}
				} else if len(cfg.Weights) > 0 {
					// Format 2: simple values + weights arrays
					values = make([]string, len(cfg.Values))
					for i, v := range cfg.Values {
						values[i] = fmt.Sprint(v)
					}
					weights = cfg.Weights
				} else {
					return nil, fmt.Errorf("weighted_pool with 'values' array requires either inline weights or separate 'weights' array")
				}
			}
		} else {
			return nil, fmt.Errorf("weighted_pool requires 'values' or 'weighted_values'")
		}
		
		if len(values) == 0 || len(weights) == 0 {
			return nil, fmt.Errorf("weighted_pool: failed to parse values and weights")
		}
		
		return buildWeightedPool(values, weights)
	
	case "uuid":
		return UUID(), nil
	
	case "random_string":
		gen := RandStr(cfg.Length)
		if cfg.Prefix != "" {
			gen = gen.Prefix(cfg.Prefix)
		}
		return gen, nil
	
	case "hex":
		gen := Hex(cfg.Length)
		if cfg.Prefix != "" {
			gen = gen.Prefix(cfg.Prefix)
		}
		return gen, nil
	
	case "int_range":
		min := toInt(cfg.Min)
		max := toInt(cfg.Max)
		return Int(min, max), nil
	
	case "float_range":
		min := toFloat64(cfg.Min)
		max := toFloat64(cfg.Max)
		precision := cfg.Precision
		if precision <= 0 {
			precision = 2
		}
		return buildFloatRange(min, max, precision), nil
	
	case "ip_v4":
		gen := IP()
		if cfg.IPFormat == "uint32" {
			gen = gen.AsU32()
		} else if cfg.IPFormat == "hex" {
			gen = gen.AsHex()
		}
		return gen, nil
	
	case "timestamp":
		return buildTimestampGen(cfg), nil
	
	default:
		return nil, fmt.Errorf("unsupported generator type: %s", cfg.Type)
	}
}

// buildWeightedPool creates a weighted pool generator.
func buildWeightedPool(values []string, weights []float64) (Gen, error) {
	if len(values) != len(weights) {
		return nil, fmt.Errorf("values and weights must have same length (got %d values, %d weights)", len(values), len(weights))
	}
	
	// Calculate sum
	var sum float64
	for i, w := range weights {
		if w < 0 {
			return nil, fmt.Errorf("weight at index %d is negative: %f", i, w)
		}
		sum += w
	}
	
	if sum <= 0 {
		return nil, fmt.Errorf("sum of weights must be positive (got %f, weights=%v)", sum, weights)
	}
	
	normalized := make([]float64, len(weights))
	for i, w := range weights {
		normalized[i] = w / sum
	}
	
	// Create cumulative distribution
	cumulative := make([]float64, len(normalized))
	cumulative[0] = normalized[0]
	for i := 1; i < len(normalized); i++ {
		cumulative[i] = cumulative[i-1] + normalized[i]
	}
	
	return Func(func(rng *Rng) string {
		r := rand.Float64()
		for i, c := range cumulative {
			if r <= c {
				return values[i]
			}
		}
		return values[len(values)-1]
	}), nil
}

// buildFloatRange creates a float range generator.
func buildFloatRange(min, max float64, precision int) Gen {
	format := fmt.Sprintf("%%.%df", precision)
	return Func(func(rng *Rng) string {
		val := min + rand.Float64()*(max-min)
		return fmt.Sprintf(format, val)
	})
}

// buildTimestampGen creates a timestamp generator.
func buildTimestampGen(cfg GeneratorConfig) Gen {
	return Func(func(rng *Rng) string {
		now := time.Now()
		
		// Apply offset
		if cfg.OffsetSec != 0 {
			now = now.Add(time.Duration(cfg.OffsetSec) * time.Second)
		}
		if cfg.OffsetMs != 0 {
			now = now.Add(time.Duration(cfg.OffsetMs) * time.Millisecond)
		}
		
		// Format based on type
		switch cfg.Format {
		case "unix_sec":
			return strconv.FormatInt(now.Unix(), 10)
		case "unix_milli":
			return strconv.FormatInt(now.UnixMilli(), 10)
		case "unix_nano":
			return strconv.FormatInt(now.UnixNano(), 10)
		case "rfc3339":
			return now.Format(time.RFC3339)
		default:
			// Default to unix seconds
			return strconv.FormatInt(now.Unix(), 10)
		}
	})
}

// Helper functions to convert interface{} to specific types
func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int8:
		return int(val)
	case int16:
		return int(val)
	case int32:
		return int(val)
	case int64:
		return int(val)
	case uint:
		return int(val)
	case uint8:
		return int(val)
	case uint16:
		return int(val)
	case uint32:
		return int(val)
	case uint64:
		return int(val)
	case float64:
		return int(val)
	case float32:
		return int(val)
	case string:
		i, _ := strconv.Atoi(val)
		return i
	default:
		return 0
	}
}

func toFloat64(v interface{}) float64 {
	if v == nil {
		return 0.0
	}
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int8:
		return float64(val)
	case int16:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	case uint:
		return float64(val)
	case uint8:
		return float64(val)
	case uint16:
		return float64(val)
	case uint32:
		return float64(val)
	case uint64:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		// Try to convert via fmt
		s := fmt.Sprint(v)
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
}

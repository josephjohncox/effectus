package types

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RegisterStandardLibrary registers built-in expression functions.
func (ts *TypeSystem) RegisterStandardLibrary() {
	for _, spec := range StandardLibrary() {
		ts.RegisterFunctionSpec(spec)
	}
}

// StandardLibrary returns the built-in expression function specs.
func StandardLibrary() []*FunctionSpec {
	return []*FunctionSpec{
		{
			Name:       "lower",
			Func:       func(value string) string { return strings.ToLower(value) },
			ArgTypes:   []string{"string"},
			ReturnType: "string",
		},
		{
			Name:       "upper",
			Func:       func(value string) string { return strings.ToUpper(value) },
			ArgTypes:   []string{"string"},
			ReturnType: "string",
		},
		{
			Name:       "trim",
			Func:       func(value string) string { return strings.TrimSpace(value) },
			ArgTypes:   []string{"string"},
			ReturnType: "string",
		},
		{
			Name:       "trimPrefix",
			Func:       func(value, prefix string) string { return strings.TrimPrefix(value, prefix) },
			ArgTypes:   []string{"string", "string"},
			ReturnType: "string",
		},
		{
			Name:       "trimSuffix",
			Func:       func(value, suffix string) string { return strings.TrimSuffix(value, suffix) },
			ArgTypes:   []string{"string", "string"},
			ReturnType: "string",
		},
		{
			Name:       "contains",
			Func:       func(value, substr string) bool { return strings.Contains(value, substr) },
			ArgTypes:   []string{"string", "string"},
			ReturnType: "bool",
		},
		{
			Name:       "hasPrefix",
			Func:       func(value, prefix string) bool { return strings.HasPrefix(value, prefix) },
			ArgTypes:   []string{"string", "string"},
			ReturnType: "bool",
		},
		{
			Name:       "hasSuffix",
			Func:       func(value, suffix string) bool { return strings.HasSuffix(value, suffix) },
			ArgTypes:   []string{"string", "string"},
			ReturnType: "bool",
		},
		{
			Name:       "replace",
			Func:       func(value, oldValue, newValue string) string { return strings.ReplaceAll(value, oldValue, newValue) },
			ArgTypes:   []string{"string", "string", "string"},
			ReturnType: "string",
		},
		{
			Name:       "length",
			Func:       func(value string) int { return len(value) },
			ArgTypes:   []string{"string"},
			ReturnType: "int",
		},
		{
			Name:       "size",
			Func:       func(value interface{}) int { return sizeOf(value) },
			ArgTypes:   []string{"any"},
			ReturnType: "int",
		},
		{
			Name:       "split",
			Func:       func(value, sep string) []string { return strings.Split(value, sep) },
			ArgTypes:   []string{"string", "string"},
			ReturnType: "list<string>",
		},
		{
			Name:       "join",
			Func:       func(values interface{}, sep string) string { return joinValues(values, sep) },
			ArgTypes:   []string{"list<any>", "string"},
			ReturnType: "string",
		},
		{
			Name:       "substr",
			Func:       func(value string, start int, length int) string { return substr(value, start, length) },
			ArgTypes:   []string{"string", "int", "int"},
			ReturnType: "string",
		},
		{
			Name:       "toString",
			Func:       func(value interface{}) string { return fmt.Sprint(value) },
			ArgTypes:   []string{"any"},
			ReturnType: "string",
		},
		{
			Name:       "toInt",
			Func:       func(value interface{}) int { return toInt(value) },
			ArgTypes:   []string{"any"},
			ReturnType: "int",
		},
		{
			Name:       "toFloat",
			Func:       func(value interface{}) float64 { return toFloat(value) },
			ArgTypes:   []string{"any"},
			ReturnType: "float",
		},
		{
			Name:       "toBool",
			Func:       func(value interface{}) bool { return toBool(value) },
			ArgTypes:   []string{"any"},
			ReturnType: "bool",
		},
		{
			Name:       "abs",
			Func:       func(value float64) float64 { return math.Abs(value) },
			ArgTypes:   []string{"float"},
			ReturnType: "float",
		},
		{
			Name:       "floor",
			Func:       func(value float64) float64 { return math.Floor(value) },
			ArgTypes:   []string{"float"},
			ReturnType: "float",
		},
		{
			Name:       "ceil",
			Func:       func(value float64) float64 { return math.Ceil(value) },
			ArgTypes:   []string{"float"},
			ReturnType: "float",
		},
		{
			Name:       "round",
			Func:       func(value float64) float64 { return math.Round(value) },
			ArgTypes:   []string{"float"},
			ReturnType: "float",
		},
		{
			Name:       "min",
			Func:       func(a, b float64) float64 { return math.Min(a, b) },
			ArgTypes:   []string{"float", "float"},
			ReturnType: "float",
		},
		{
			Name:       "max",
			Func:       func(a, b float64) float64 { return math.Max(a, b) },
			ArgTypes:   []string{"float", "float"},
			ReturnType: "float",
		},
		{
			Name:       "pow",
			Func:       func(a, b float64) float64 { return math.Pow(a, b) },
			ArgTypes:   []string{"float", "float"},
			ReturnType: "float",
		},
		{
			Name:       "sqrt",
			Func:       func(value float64) float64 { return math.Sqrt(value) },
			ArgTypes:   []string{"float"},
			ReturnType: "float",
		},
		{
			Name:        "now",
			Func:        func() time.Time { return time.Now() },
			ArgTypes:    []string{},
			ReturnType:  "time",
			Unsafe:      true,
			Description: "Current time (nondeterministic; prefer injected clocks)",
		},
		{
			Name:        "nowUTC",
			Func:        func() time.Time { return time.Now().UTC() },
			ArgTypes:    []string{},
			ReturnType:  "time",
			Unsafe:      true,
			Description: "Current UTC time (nondeterministic; prefer injected clocks)",
		},
		{
			Name:        "regexMatch",
			Func:        func(pattern, value string) bool { matched, _ := regexp.MatchString(pattern, value); return matched },
			ArgTypes:    []string{"string", "string"},
			ReturnType:  "bool",
			Unsafe:      true,
			Description: "Regex match (potentially unsafe for untrusted patterns)",
		},
	}
}

func sizeOf(value interface{}) int {
	switch v := value.(type) {
	case string:
		return len(v)
	case []interface{}:
		return len(v)
	case []string:
		return len(v)
	case []int:
		return len(v)
	case []float64:
		return len(v)
	case map[string]interface{}:
		return len(v)
	case map[string]string:
		return len(v)
	default:
		return 0
	}
}

func joinValues(values interface{}, sep string) string {
	switch v := values.(type) {
	case []string:
		return strings.Join(v, sep)
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, sep)
	default:
		return ""
	}
}

func substr(value string, start int, length int) string {
	runes := []rune(value)
	if start < 0 {
		start = 0
	}
	if start >= len(runes) {
		return ""
	}
	end := start + length
	if length < 0 {
		end = len(runes)
	}
	if end > len(runes) {
		end = len(runes)
	}
	return string(runes[start:end])
}

func toInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return parsed
		}
	case bool:
		if v {
			return 1
		}
		return 0
	}
	return 0
}

func toFloat(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return parsed
		}
	case bool:
		if v {
			return 1
		}
		return 0
	}
	return 0
}

func toBool(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		trimmed := strings.TrimSpace(strings.ToLower(v))
		switch trimmed {
		case "true", "1", "yes", "y", "on":
			return true
		case "false", "0", "no", "n", "off":
			return false
		}
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case float32:
		return v != 0
	}
	return false
}

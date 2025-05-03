package pathutil

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// GjsonProvider is a FactProvider implementation backed by gjson
type GjsonProvider struct {
	// Raw JSON data
	rawJSON string
}

// NewGjsonProvider creates a new gjson-backed provider with the given data
func NewGjsonProvider(data interface{}) *GjsonProvider {
	// Convert data to JSON string
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		// In production code, you might want to handle this differently
		panic(fmt.Sprintf("failed to marshal data: %v", err))
	}

	return &GjsonProvider{
		rawJSON: string(jsonBytes),
	}
}

// NewGjsonProviderFromJSON creates a new provider from raw JSON string
func NewGjsonProviderFromJSON(jsonData string) *GjsonProvider {
	return &GjsonProvider{
		rawJSON: jsonData,
	}
}

// Get retrieves a value using a path
func (p *GjsonProvider) Get(path string) (interface{}, bool) {
	// For GjsonProvider, we ignore namespaces as they are handled by the Registry
	// Just use the path directly with gjson

	// Check if the path contains array notation like [0]
	// GJSON expects array access without brackets for numeric indices
	path = strings.ReplaceAll(path, "[", ".")
	path = strings.ReplaceAll(path, "]", "")

	result := gjson.Get(p.rawJSON, path)
	if !result.Exists() {
		return nil, false
	}

	// Convert to Go type
	value := gjsonResultToInterface(result)
	return value, true
}

// GetWithContext retrieves a value with detailed resolution information
func (p *GjsonProvider) GetWithContext(path string) (interface{}, *ResolutionResult) {
	// For GjsonProvider, we ignore namespaces as they are handled by the Registry
	// Just use the path directly with gjson

	// Check if the path contains array notation like [0]
	// GJSON expects array access without brackets for numeric indices
	formattedPath := strings.ReplaceAll(path, "[", ".")
	formattedPath = strings.ReplaceAll(formattedPath, "]", "")

	result := gjson.Get(p.rawJSON, formattedPath)

	if !result.Exists() {
		return nil, &ResolutionResult{
			Path:   path,
			Exists: false,
			Error:  fmt.Errorf("path not found: %s", path),
		}
	}

	// Convert to Go type
	value := gjsonResultToInterface(result)

	return value, &ResolutionResult{
		Path:   path,
		Value:  value,
		Exists: true,
	}
}

// Helper to convert gjson.Result to interface{}
func gjsonResultToInterface(result gjson.Result) interface{} {
	switch result.Type {
	case gjson.Null:
		return nil
	case gjson.False:
		return false
	case gjson.True:
		return true
	case gjson.Number:
		if result.Float() == float64(result.Int()) {
			return result.Int()
		}
		return result.Float()
	case gjson.String:
		return result.String()
	case gjson.JSON:
		if result.IsArray() {
			arr := result.Array()
			interfaceArray := make([]interface{}, len(arr))
			for i, v := range arr {
				interfaceArray[i] = gjsonResultToInterface(v)
			}
			return interfaceArray
		} else {
			m := result.Map()
			interfaceMap := make(map[string]interface{})
			for k, v := range m {
				interfaceMap[k] = gjsonResultToInterface(v)
			}
			return interfaceMap
		}
	default:
		return nil
	}
}

// WithJSON creates a new provider with updated JSON data
func (p *GjsonProvider) WithJSON(jsonData string) *GjsonProvider {
	return &GjsonProvider{
		rawJSON: jsonData,
	}
}

// WithData creates a new provider with updated structured data
func (p *GjsonProvider) WithData(data interface{}) *GjsonProvider {
	return NewGjsonProvider(data)
}

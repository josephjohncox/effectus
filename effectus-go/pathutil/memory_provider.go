package pathutil

import (
	"fmt"
)

// MemoryProvider is a simple in-memory implementation of FactProvider
type MemoryProvider struct {
	// Data is a map of key-value pairs
	data map[string]interface{}
}

// NewMemoryProvider creates a new in-memory provider with the given data
func NewMemoryProvider(data map[string]interface{}) *MemoryProvider {
	// Make a deep copy of the data for immutability
	dataCopy := deepCopyMap(data)
	return &MemoryProvider{
		data: dataCopy,
	}
}

// Get retrieves a value using a structured path
func (p *MemoryProvider) Get(path Path) (interface{}, bool) {
	value, result := p.GetWithContext(path)
	return value, result != nil && result.Exists
}

// GetWithContext retrieves a value with detailed resolution information
func (p *MemoryProvider) GetWithContext(path Path) (interface{}, *ResolutionResult) {
	// Start at the root of the data
	var current interface{} = p.data

	// Navigate through elements
	for i, elem := range path.Elements {
		// If we have a null value, we can't navigate further
		if current == nil {
			return nil, &ResolutionResult{
				Path:   path,
				Exists: false,
				Error:  fmt.Errorf("path element is null: %s", elem.Name),
			}
		}

		currentMap, ok := current.(map[string]interface{})
		if !ok {
			return nil, &ResolutionResult{
				Path:   path,
				Exists: false,
				Error:  fmt.Errorf("cannot navigate: not a map"),
			}
		}

		// Try to get the value by key
		value, valueExists := currentMap[elem.Name]
		if !valueExists {
			return nil, &ResolutionResult{
				Path:   path,
				Exists: false,
				Error:  fmt.Errorf("field not found: %s", elem.Name),
			}
		}

		if elem.HasIndex() {
			// Array indexing
			arr, ok := value.([]interface{})
			if !ok {
				return nil, &ResolutionResult{
					Path:   path,
					Exists: false,
					Error:  fmt.Errorf("not an array: %s", elem.Name),
				}
			}

			idx, _ := elem.GetIndex()
			if idx < 0 || idx >= len(arr) {
				return nil, &ResolutionResult{
					Path:   path,
					Exists: false,
					Error:  fmt.Errorf("index out of bounds: %d", idx),
				}
			}

			// If this is the last element, return the value
			if i == len(path.Elements)-1 {
				return arr[idx], &ResolutionResult{
					Path:   path,
					Value:  arr[idx],
					Exists: true,
				}
			}

			// Otherwise, continue navigating
			current = arr[idx]
		} else if elem.HasStringKey() {
			// Map key access
			mapVal, ok := value.(map[string]interface{})
			if !ok {
				return nil, &ResolutionResult{
					Path:   path,
					Exists: false,
					Error:  fmt.Errorf("not a map: %s", elem.Name),
				}
			}

			key, _ := elem.GetStringKey()
			val, exists := mapVal[key]
			if !exists {
				return nil, &ResolutionResult{
					Path:   path,
					Exists: false,
					Error:  fmt.Errorf("key not found: %s", key),
				}
			}

			// If this is the last element, return the value
			if i == len(path.Elements)-1 {
				return val, &ResolutionResult{
					Path:   path,
					Value:  val,
					Exists: true,
				}
			}

			// Otherwise, continue navigating
			current = val
		} else {
			// Regular field access
			// If this is the last element, return the value
			if i == len(path.Elements)-1 {
				return value, &ResolutionResult{
					Path:   path,
					Value:  value,
					Exists: true,
				}
			}

			// Otherwise, continue navigating
			current = value
		}
	}

	// If we get here with no elements, we're looking for the root
	if len(path.Elements) == 0 {
		return current, &ResolutionResult{
			Path:   path,
			Value:  current,
			Exists: true,
		}
	}

	// If we get here, something went wrong
	return nil, &ResolutionResult{
		Path:   path,
		Exists: false,
		Error:  fmt.Errorf("navigation error"),
	}
}

// WithData creates a new provider with updated data
func (p *MemoryProvider) WithData(data map[string]interface{}) *MemoryProvider {
	return NewMemoryProvider(data)
}

// Merge merges another provider's data into this one
func (p *MemoryProvider) Merge(other *MemoryProvider) *MemoryProvider {
	result := make(map[string]interface{})

	// Copy this provider's data
	for k, v := range p.data {
		result[k] = deepCopyValue(v)
	}

	// Merge the other provider's data
	for k, v := range other.data {
		result[k] = deepCopyValue(v)
	}

	return NewMemoryProvider(result)
}

// Helper function for deep-copying maps to ensure immutability
func deepCopyMap(original map[string]interface{}) map[string]interface{} {
	if original == nil {
		return nil
	}

	copied := make(map[string]interface{}, len(original))
	for key, value := range original {
		copied[key] = deepCopyValue(value)
	}

	return copied
}

// Helper function for deep-copying values of any type
func deepCopyValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case map[string]interface{}:
		return deepCopyMap(v)
	case []interface{}:
		copiedSlice := make([]interface{}, len(v))
		for i, item := range v {
			copiedSlice[i] = deepCopyValue(item)
		}
		return copiedSlice
	default:
		// For primitive types, they're already passed by value
		return v
	}
}

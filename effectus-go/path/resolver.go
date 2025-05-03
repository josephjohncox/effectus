package path

import (
	"fmt"
)

// ResolutionResult contains the result of path resolution
type ResolutionResult struct {
	// Path is the resolved path
	Path Path

	// Value is the resolved value
	Value interface{}

	// Exists indicates if the path exists in the data
	Exists bool

	// Error is any error encountered during resolution
	Error error
}

// PathResolver is responsible for resolving paths against data
type PathResolver struct {
	typeSystem interface{} // Interface to the type system
	debug      bool
}

// NewPathResolver creates a new path resolver
func NewPathResolver(typeSystem interface{}, debug bool) *PathResolver {
	return &PathResolver{
		typeSystem: typeSystem,
		debug:      debug,
	}
}

// ResolvePath resolves a path against data
func (r *PathResolver) ResolvePath(pathStr string, data interface{}) (*ResolutionResult, error) {
	// Parse the path
	parsedPath, err := ParseString(pathStr)
	if err != nil {
		return &ResolutionResult{
			Path:   Path{},
			Exists: false,
			Error:  fmt.Errorf("invalid path: %s: %w", pathStr, err),
		}, err
	}

	return r.ResolvePathObject(parsedPath, data)
}

// ResolvePathObject resolves a path object against data
func (r *PathResolver) ResolvePathObject(path Path, data interface{}) (*ResolutionResult, error) {
	if data == nil {
		return &ResolutionResult{
			Path:   path,
			Exists: false,
			Error:  fmt.Errorf("nil data"),
		}, nil
	}

	// Try to extract the namespace data
	var current interface{} = data

	// If data is a map, extract the namespace
	if m, ok := data.(map[string]interface{}); ok {
		if nsData, ok := m[path.Namespace]; ok {
			current = nsData
		} else {
			return &ResolutionResult{
				Path:   path,
				Exists: false,
				Error:  fmt.Errorf("namespace not found: %s", path.Namespace),
			}, nil
		}
	}

	// Navigate the path elements
	for i, elem := range path.Elements {
		switch currentMap := current.(type) {
		case map[string]interface{}:
			// Try to get the value by key
			if value, ok := currentMap[elem.Name]; ok {
				if elem.HasIndex() {
					// Handle array access
					if arr, ok := value.([]interface{}); ok {
						idx, _ := elem.GetIndex()
						if idx >= 0 && idx < len(arr) {
							current = arr[idx]
						} else {
							return &ResolutionResult{
								Path:   path,
								Exists: false,
								Error:  fmt.Errorf("index out of bounds: %d", idx),
							}, nil
						}
					} else {
						return &ResolutionResult{
							Path:   path,
							Exists: false,
							Error:  fmt.Errorf("not an array: %s", elem.Name),
						}, nil
					}
				} else if elem.HasStringKey() {
					// Handle map key access
					if mapVal, ok := value.(map[string]interface{}); ok {
						key, _ := elem.GetStringKey()
						if val, ok := mapVal[key]; ok {
							current = val
						} else {
							return &ResolutionResult{
								Path:   path,
								Exists: false,
								Error:  fmt.Errorf("key not found: %s", key),
							}, nil
						}
					} else {
						return &ResolutionResult{
							Path:   path,
							Exists: false,
							Error:  fmt.Errorf("not a map: %s", elem.Name),
						}, nil
					}
				} else {
					// Regular field access
					current = value
				}
			} else {
				return &ResolutionResult{
					Path:   path,
					Exists: false,
					Error:  fmt.Errorf("field not found: %s", elem.Name),
				}, nil
			}

		case []interface{}:
			// Array elements need to have an index
			if !elem.HasIndex() {
				return &ResolutionResult{
					Path:   path,
					Exists: false,
					Error:  fmt.Errorf("array access without index: %s", elem.Name),
				}, nil
			}

			idx, _ := elem.GetIndex()
			if idx >= 0 && idx < len(currentMap) {
				current = currentMap[idx]
			} else {
				return &ResolutionResult{
					Path:   path,
					Exists: false,
					Error:  fmt.Errorf("index out of bounds: %d", idx),
				}, nil
			}

		default:
			// If we're at a leaf, return the current value
			if i == len(path.Elements)-1 {
				return &ResolutionResult{
					Path:   path,
					Value:  current,
					Exists: true,
				}, nil
			}

			// Otherwise, we can't navigate further
			return &ResolutionResult{
				Path:   path,
				Exists: false,
				Error:  fmt.Errorf("cannot navigate past leaf value at: %s", elem.Name),
			}, nil
		}
	}

	// If we get here, we've successfully navigated the path
	return &ResolutionResult{
		Path:   path,
		Value:  current,
		Exists: true,
	}, nil
}

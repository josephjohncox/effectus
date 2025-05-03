package schema

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/effectus/effectus-go"
)

// JSONFacts implements the Facts interface for JSON data
type JSONFacts struct {
	data      map[string]interface{}
	schema    effectus.SchemaInfo
	resolver  FactPathResolver
	pathCache *PathCache // Added path cache
}

// NewJSONFacts creates a new JSONFacts instance from a map
func NewJSONFacts(data map[string]interface{}) *JSONFacts {
	// Create a resolver using the unified resolver
	typeSystem := NewTypeSystem()
	resolver := NewUnifiedPathResolver(typeSystem, false)

	return &JSONFacts{
		data:      data,
		schema:    &JSONSchema{data: data},
		resolver:  resolver,
		pathCache: NewPathCache(), // Initialize path cache
	}
}

// NewJSONFactsFromBytes parses JSON bytes into a JSONFacts instance
func NewJSONFactsFromBytes(jsonData []byte) (*JSONFacts, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}
	return NewJSONFacts(data), nil
}

// NewJSONFactsFromString parses a JSON string into a JSONFacts instance
func NewJSONFactsFromString(jsonStr string) (*JSONFacts, error) {
	return NewJSONFactsFromBytes([]byte(jsonStr))
}

// ResolveJSONPath directly resolves a path in JSON data without using a resolver
// Exported for use by resolver implementations
func ResolveJSONPath(data map[string]interface{}, path FactPath) (interface{}, bool) {
	// Special case for empty path
	if len(path.Segments()) == 0 && path.Namespace() == "" {
		return nil, false
	}

	// Debug output
	log.Printf("ResolveJSONPath - Path: %s, Namespace: %s, Segments: %d",
		path.String(), path.Namespace(), len(path.Segments()))

	// Start with data map or namespace field
	var current interface{} = data
	if path.Namespace() != "" {
		var ok bool
		current, ok = data[path.Namespace()]
		if !ok {
			log.Printf("ResolveJSONPath - Namespace '%s' not found in data", path.Namespace())
			return nil, false
		}
		log.Printf("ResolveJSONPath - Found namespace '%s': %v (%T)",
			path.Namespace(), current, current)
	}

	// Return early if no segments (just namespace)
	if len(path.Segments()) == 0 {
		return current, true
	}

	// Traverse each segment
	for i, segment := range path.Segments() {
		log.Printf("ResolveJSONPath - Processing segment %d: %s", i, segment.String())

		// Handle different data types
		switch curr := current.(type) {
		case map[string]interface{}:
			log.Printf("ResolveJSONPath - Current is map, accessing field '%s'", segment.Name)
			// Regular field access
			var exists bool
			current, exists = curr[segment.Name]
			if !exists {
				log.Printf("ResolveJSONPath - Field '%s' not found in map", segment.Name)
				return nil, false
			}
			log.Printf("ResolveJSONPath - Found field '%s': %v (%T)",
				segment.Name, current, current)

			// Check for array indexing or map key access
			if idx, hasIndex := segment.GetIndex(); hasIndex {
				log.Printf("ResolveJSONPath - Segment has index: %d", *idx)
				// After field access, check if result is an array
				arr, ok := current.([]interface{})
				if !ok {
					log.Printf("ResolveJSONPath - Field '%s' is not an array, got %T",
						segment.Name, current)
					return nil, false
				}

				// Validate index bounds
				if *idx < 0 || *idx >= len(arr) {
					log.Printf("ResolveJSONPath - Index %d out of bounds (0-%d)",
						*idx, len(arr)-1)
					return nil, false
				}

				// Get array element
				current = arr[*idx]
				log.Printf("ResolveJSONPath - Array element at index %d: %v (%T)",
					*idx, current, current)
			} else if segment.HasStringKey() {
				// Map key access (for string keys)
				mapVal, ok := current.(map[string]interface{})
				if !ok {
					log.Printf("ResolveJSONPath - Field '%s' is not a map, got %T",
						segment.Name, current)
					return nil, false
				}

				key := segment.StringKey()
				current, ok = mapVal[key]
				if !ok {
					log.Printf("ResolveJSONPath - String key '%s' not found in map", key)
					return nil, false
				}
				log.Printf("ResolveJSONPath - Map value for key '%s': %v (%T)",
					key, current, current)
			}

		case []interface{}:
			log.Printf("ResolveJSONPath - Current is array, need index")
			// Direct array access - should have an index
			idx, hasIndex := segment.GetIndex()
			if !hasIndex {
				log.Printf("ResolveJSONPath - Array access without index")
				return nil, false // Array requires index
			}

			// Validate index bounds
			if *idx < 0 || *idx >= len(curr) {
				log.Printf("ResolveJSONPath - Index %d out of bounds (0-%d)",
					*idx, len(curr)-1)
				return nil, false
			}

			// Get array element
			current = curr[*idx]
			log.Printf("ResolveJSONPath - Array element at index %d: %v (%T)",
				*idx, current, current)

		default:
			// Can't navigate further
			log.Printf("ResolveJSONPath - Can't navigate on type %T", curr)
			return nil, false
		}
	}

	log.Printf("ResolveJSONPath - Final result: %v (%T)", current, current)
	return current, true
}

// ResolveJSONPathWithContext is an enhanced version that returns detailed error information
func ResolveJSONPathWithContext(data map[string]interface{}, path FactPath) (interface{}, *PathResolutionResult) {
	result := &PathResolutionResult{
		Path: path.String(),
	}

	// Special case for empty path
	if len(path.Segments()) == 0 && path.Namespace() == "" {
		result.Error = fmt.Errorf("empty path")
		return nil, result
	}

	// Start with data map or namespace field
	var current interface{} = data
	if path.Namespace() != "" {
		var ok bool
		current, ok = data[path.Namespace()]
		if !ok {
			result.Error = fmt.Errorf("namespace '%s' not found", path.Namespace())
			result.FailedSegment = path.Namespace()
			return nil, result
		}
	}

	// Return early if no segments (just namespace)
	if len(path.Segments()) == 0 {
		result.Exists = true
		result.Value = current
		result.ValueType = determineType(current)
		return current, result
	}

	// Keep track of current path for error reporting
	currentPath := path.Namespace()

	// Traverse each segment
	for i, segment := range path.Segments() {
		segmentStr := segment.String()
		currentPath += "." + segmentStr

		// Handle different data types
		switch curr := current.(type) {
		case map[string]interface{}:
			// Regular field access
			var exists bool
			current, exists = curr[segment.Name]
			if !exists {
				result.Error = fmt.Errorf("field '%s' not found", segment.Name)
				result.FailedSegment = segmentStr
				result.FailedAt = currentPath
				return nil, result
			}

			// Check for array indexing or map key access
			if idx, hasIndex := segment.GetIndex(); hasIndex {
				// After field access, check if result is an array
				arr, ok := current.([]interface{})
				if !ok {
					result.Error = fmt.Errorf("expected array at '%s', got %T", segment.Name, current)
					result.FailedSegment = segmentStr
					result.FailedAt = currentPath
					return nil, result
				}

				// Validate index bounds
				if *idx < 0 || *idx >= len(arr) {
					result.Error = fmt.Errorf("array index %d out of bounds (0-%d)", *idx, len(arr)-1)
					result.FailedSegment = segmentStr
					result.FailedAt = currentPath
					return nil, result
				}

				// Get array element
				current = arr[*idx]
			} else if segment.HasStringKey() {
				// Map key access (for string keys)
				mapVal, ok := current.(map[string]interface{})
				if !ok {
					result.Error = fmt.Errorf("expected map at '%s', got %T", segment.Name, current)
					result.FailedSegment = segmentStr
					result.FailedAt = currentPath
					return nil, result
				}

				key := segment.StringKey()
				current, ok = mapVal[key]
				if !ok {
					result.Error = fmt.Errorf("map key '%s' not found", key)
					result.FailedSegment = segmentStr
					result.FailedAt = currentPath
					return nil, result
				}
			}

		case []interface{}:
			// Direct array access - should have an index
			idx, hasIndex := segment.GetIndex()
			if !hasIndex {
				result.Error = fmt.Errorf("array access requires an index at '%s'", currentPath)
				result.FailedSegment = segmentStr
				result.FailedAt = currentPath
				return nil, result
			}

			// Validate index bounds
			if *idx < 0 || *idx >= len(curr) {
				result.Error = fmt.Errorf("array index %d out of bounds (0-%d)", *idx, len(curr)-1)
				result.FailedSegment = segmentStr
				result.FailedAt = currentPath
				return nil, result
			}

			// Get array element
			current = curr[*idx]

		default:
			// Can't navigate further
			result.Error = fmt.Errorf("can't access '%s' on type %T", segment.Name, curr)
			result.FailedSegment = segmentStr
			result.FailedAt = currentPath
			return nil, result
		}

		// If this is the last segment, set success result
		if i == len(path.Segments())-1 {
			result.Exists = true
			result.Value = current
			result.ValueType = determineType(current)
		}
	}

	return current, result
}

// PathResolutionResult provides detailed information about path resolution
type PathResolutionResult struct {
	Path          string
	Exists        bool
	Value         interface{}
	ValueType     *Type
	Error         error
	FailedSegment string
	FailedAt      string
}

// Get returns the value at the given path, or false if not found
func (f *JSONFacts) Get(path string) (interface{}, bool) {
	// Use cached path if available
	factPath, err := f.pathCache.Get(path)
	if err != nil {
		log.Printf("JSONFacts: error parsing path: %v", err)
		return nil, false
	}

	// Direct path resolution to avoid infinite recursion through resolver
	return ResolveJSONPath(f.data, factPath)
}

// EnhancedGet provides additional context in the return values
// including type information and path validation errors
func (f *JSONFacts) EnhancedGet(path string) (value interface{}, exists bool, info *PathResolutionInfo) {
	info = &PathResolutionInfo{
		Path: path,
	}

	// Try to parse the path
	factPath, err := f.pathCache.Get(path)
	if err != nil {
		info.Error = fmt.Errorf("invalid path syntax: %w", err)
		return nil, false, info
	}

	// Use the enhanced resolver capabilities
	if unifiedResolver, ok := f.resolver.(*UnifiedPathResolver); ok {
		value, result := unifiedResolver.ResolveWithContext(f, factPath)
		exists = result.Exists

		if !exists {
			info.Error = result.Error
			return nil, false, info
		}

		info.ValueType = result.ValueType
		return value, true, info
	}

	// Fallback to basic resolution
	value, exists = ResolveJSONPath(f.data, factPath)

	if !exists {
		info.Error = fmt.Errorf("path not found in data")
		return nil, false, info
	}

	// Add type information to the result
	if value != nil {
		// Use the determineType function directly from schema package
		// instead of duplicating the logic
		info.ValueType = determineType(value)
	}

	return value, true, info
}

// PathResolutionInfo provides additional context for path resolution
type PathResolutionInfo struct {
	Path      string
	ValueType *Type
	Error     error
}

// determineType infers the type of a value
func determineType(value interface{}) *Type {
	switch v := value.(type) {
	case string:
		return &Type{PrimType: TypeString}
	case float64:
		return &Type{PrimType: TypeFloat}
	case int:
		return &Type{PrimType: TypeInt}
	case bool:
		return &Type{PrimType: TypeBool}
	case []interface{}:
		elemType := &Type{PrimType: TypeUnknown}
		if len(v) > 0 {
			elemType = determineType(v[0])
		}
		return &Type{
			PrimType: TypeList,
			ListType: elemType,
		}
	case map[string]interface{}:
		return &Type{
			PrimType:   TypeMap,
			MapKeyType: &Type{PrimType: TypeString},
			MapValType: &Type{PrimType: TypeUnknown},
		}
	default:
		return &Type{PrimType: TypeUnknown}
	}
}

// Schema returns the schema information
func (f *JSONFacts) Schema() effectus.SchemaInfo {
	return f.schema
}

// JSONSchema implements SchemaInfo for JSON data
type JSONSchema struct {
	data map[string]interface{}
}

// ValidatePath checks if a path is valid according to the JSON structure
func (s *JSONSchema) ValidatePath(path string) bool {
	// Basic validation
	if path == "" {
		return false
	}

	if strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") {
		return false
	}

	if strings.Contains(path, "..") {
		return false
	}

	// Try to parse and resolve the path
	factPath, err := ParseFactPath(path)
	if err != nil {
		return false
	}

	// Actually check if the path exists in the data
	_, exists := ResolveJSONPath(s.data, factPath)
	return exists
}

// RegisterJSONTypes registers JSON structure types with the type system
func RegisterJSONTypes(data map[string]interface{}, ts *TypeSystem, namespace string) {
	for key, value := range data {
		path := namespace + "." + key
		registerJSONValue(value, ts, path)
	}
}

// registerJSONValue registers a single JSON value with the type system
func registerJSONValue(value interface{}, ts *TypeSystem, path string) {
	switch v := value.(type) {
	case string:
		ts.RegisterFactType(path, &Type{PrimType: TypeString})
	case float64:
		ts.RegisterFactType(path, &Type{PrimType: TypeFloat})
	case int:
		ts.RegisterFactType(path, &Type{PrimType: TypeInt})
	case bool:
		ts.RegisterFactType(path, &Type{PrimType: TypeBool})
	case []interface{}:
		// For arrays, register the array type and then register element types
		// if we have any elements we can examine
		elemType := &Type{PrimType: TypeUnknown}
		if len(v) > 0 {
			// Infer type from first element
			switch v[0].(type) {
			case string:
				elemType = &Type{PrimType: TypeString}
			case float64:
				elemType = &Type{PrimType: TypeFloat}
			case int:
				elemType = &Type{PrimType: TypeInt}
			case bool:
				elemType = &Type{PrimType: TypeBool}
			case map[string]interface{}:
				elemType = &Type{PrimType: TypeUnknown, Name: "object"}
				// Register nested objects
				for i, elem := range v {
					if elemMap, ok := elem.(map[string]interface{}); ok {
						elemPath := fmt.Sprintf("%s[%d]", path, i)
						for k, v := range elemMap {
							registerJSONValue(v, ts, elemPath+"."+k)
						}
					}
				}
			}
		}

		ts.RegisterFactType(path, &Type{
			PrimType: TypeList,
			ListType: elemType,
		})
	case map[string]interface{}:
		// For objects, register each field recursively
		ts.RegisterFactType(path, &Type{
			PrimType:   TypeMap,
			MapKeyType: &Type{PrimType: TypeString},
			MapValType: &Type{PrimType: TypeUnknown},
		})

		for k, v := range v {
			registerJSONValue(v, ts, path+"."+k)
		}
	case nil:
		// Register nil as unknown type
		ts.RegisterFactType(path, &Type{PrimType: TypeUnknown})
	}
}

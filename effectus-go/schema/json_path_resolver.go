package schema

import (
	"log"
)

// JSONPathResolver is a PathResolver implementation for JSON data
type JSONPathResolver struct{}

// CanResolve returns true if this resolver can handle JSON data
func (r *JSONPathResolver) CanResolve(data interface{}) bool {
	// JSON data is typically represented as map[string]interface{} or []interface{}
	_, isMap := data.(map[string]interface{})
	_, isArray := data.([]interface{})
	return isMap || isArray
}

// ResolveSegment resolves a single path segment in JSON data
func (r *JSONPathResolver) ResolveSegment(data interface{}, segment PathSegment) (interface{}, bool) {
	log.Printf("JSONPathResolver: resolving segment %s in %T", segment.String(), data)

	switch v := data.(type) {
	case map[string]interface{}:
		// Get the field value
		value, exists := v[segment.Name]
		if !exists {
			log.Printf("JSONPathResolver: field %s not found in map", segment.Name)
			return nil, false
		}

		// If there's an index expression, handle it
		if segment.IndexExpr != nil {
			// Must be an array to index into
			array, ok := value.([]interface{})
			if !ok {
				log.Printf("JSONPathResolver: value %v is not an array, can't apply index", value)
				return nil, false
			}

			// Check bounds
			index := segment.IndexExpr.Value
			if index < 0 || index >= len(array) {
				log.Printf("JSONPathResolver: index %d out of bounds (0-%d)", index, len(array)-1)
				return nil, false
			}

			return array[index], true
		}

		return value, true

	case []interface{}:
		// Array can only be indexed, not have fields
		if segment.IndexExpr == nil {
			log.Printf("JSONPathResolver: cannot access field %s on array", segment.Name)
			return nil, false
		}

		// Check bounds
		index := segment.IndexExpr.Value
		if index < 0 || index >= len(v) {
			log.Printf("JSONPathResolver: index %d out of bounds (0-%d)", index, len(v)-1)
			return nil, false
		}

		return v[index], true

	default:
		log.Printf("JSONPathResolver: unhandled data type %T", data)
		return nil, false
	}
}

// GetType returns the type of a value at a path segment
func (r *JSONPathResolver) GetType(data interface{}, segment PathSegment) *Type {
	// Get the value first
	value, found := r.ResolveSegment(data, segment)
	if !found {
		return &Type{PrimType: TypeUnknown}
	}

	// Infer type from value
	return inferTypeFromValue(value)
}

package path

// DefaultResolver is a SegmentResolver for maps and slices
type DefaultResolver struct{}

// CanResolve checks if this resolver can handle the given data source
func (r *DefaultResolver) CanResolve(data interface{}) bool {
	switch data.(type) {
	case map[string]interface{}, []interface{}:
		return true
	default:
		return false
	}
}

// ResolveSegment resolves a segment in a map or slice
func (r *DefaultResolver) ResolveSegment(data interface{}, segment PathSegment) (interface{}, bool) {
	// Handle maps
	if m, ok := data.(map[string]interface{}); ok {
		return r.resolveMapSegment(m, segment)
	}

	// Handle arrays/slices
	if arr, ok := data.([]interface{}); ok {
		return r.resolveArraySegment(arr, segment)
	}

	return nil, false
}

// GetSegmentType returns a generic type for the segment
func (r *DefaultResolver) GetSegmentType(data interface{}, segment PathSegment) interface{} {
	// This is a generic resolver that doesn't know about specific type systems.
	// In practice, this would return a proper type from your type system.
	value, found := r.ResolveSegment(data, segment)
	if !found {
		return nil
	}

	// Just return the Go type for demonstration
	return value
}

// resolveMapSegment handles path resolution within maps
func (r *DefaultResolver) resolveMapSegment(m map[string]interface{}, segment PathSegment) (interface{}, bool) {
	// First, try to get the value by name
	value, exists := m[segment.Name]
	if !exists {
		return nil, false
	}

	// If there's no index or key, we're done
	if !segment.HasIndex() && !segment.HasStringKey() {
		return value, true
	}

	// If there's an index, handle it for arrays
	if segment.HasIndex() {
		idx, _ := segment.GetIndex()
		if arr, ok := value.([]interface{}); ok {
			if idx < 0 || idx >= len(arr) {
				return nil, false
			}
			return arr[idx], true
		}
		return nil, false
	}

	// If there's a string key, handle it for nested maps
	if segment.HasStringKey() {
		key, _ := segment.GetStringKey()
		if nestedMap, ok := value.(map[string]interface{}); ok {
			nestedValue, exists := nestedMap[key]
			return nestedValue, exists
		}
		return nil, false
	}

	return nil, false
}

// resolveArraySegment handles path resolution within arrays
func (r *DefaultResolver) resolveArraySegment(arr []interface{}, segment PathSegment) (interface{}, bool) {
	// Array access requires an index
	if !segment.HasIndex() {
		return nil, false
	}

	idx, _ := segment.GetIndex()
	if idx < 0 || idx >= len(arr) {
		return nil, false
	}

	// Get the item at the index
	item := arr[idx]

	// If the segment also has a name, it's referring to a field in the item
	if segment.Name != "" && segment.Name != "*" {
		// Item must be a map to access fields
		if itemMap, ok := item.(map[string]interface{}); ok {
			value, exists := itemMap[segment.Name]
			return value, exists
		}
		return nil, false
	}

	// Otherwise, just return the item
	return item, true
}

// NewDefaultResolver creates a new default resolver
func NewDefaultResolver() *DefaultResolver {
	return &DefaultResolver{}
}

package schema

import (
	"log"

	"github.com/effectus/effectus-go"
	"github.com/iancoleman/strcase"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// PathResolver is the interface for resolving paths in different data sources
type PathResolver interface {
	// CanResolve returns true if this resolver can handle the given data source
	CanResolve(data interface{}) bool

	// ResolveSegment resolves a single path segment in the given data
	// Returns the value at the segment and whether it was found
	ResolveSegment(data interface{}, segment PathSegment) (interface{}, bool)

	// GetType returns the type of data at the given path
	GetType(data interface{}, segment PathSegment) *Type
}

// PathResolutionManager manages multiple path resolvers
type PathResolutionManager struct {
	resolvers []PathResolver
}

// NewPathResolutionManager creates a new path resolution manager
func NewPathResolutionManager(resolvers ...PathResolver) *PathResolutionManager {
	return &PathResolutionManager{
		resolvers: resolvers,
	}
}

// RegisterResolver adds a new resolver to the manager
func (m *PathResolutionManager) RegisterResolver(resolver PathResolver) {
	m.resolvers = append(m.resolvers, resolver)
}

// FindResolver finds a resolver that can handle the given data
func (m *PathResolutionManager) FindResolver(data interface{}) (PathResolver, bool) {
	for _, resolver := range m.resolvers {
		if resolver.CanResolve(data) {
			return resolver, true
		}
	}
	return nil, false
}

// ResolvePath resolves a complete path in the given data
func (m *PathResolutionManager) ResolvePath(data interface{}, path FactPath) (interface{}, bool) {
	current := data

	// Traverse each segment
	for _, segment := range path.Segments() {
		// Find a resolver for the current data
		resolver, found := m.FindResolver(current)
		if !found {
			return nil, false
		}

		// Resolve the segment
		var ok bool
		current, ok = resolver.ResolveSegment(current, segment)
		if !ok {
			return nil, false
		}
	}

	return current, true
}

// GetPathType determines the type at the given path
func (m *PathResolutionManager) GetPathType(data interface{}, path FactPath) *Type {
	if path.Type() != nil {
		return path.Type()
	}

	current := data
	var segmentType *Type

	// Traverse each segment
	for _, segment := range path.Segments() {
		// Find a resolver for the current data
		resolver, found := m.FindResolver(current)
		if !found {
			return &Type{PrimType: TypeUnknown}
		}

		// Get the type of the segment
		segmentType = resolver.GetType(current, segment)

		// Resolve the segment to continue traversal
		var ok bool
		current, ok = resolver.ResolveSegment(current, segment)
		if !ok {
			return segmentType
		}
	}

	return segmentType
}

// DefaultPathResolver is a fallback resolver for generic data
type DefaultPathResolver struct{}

// CanResolve returns true if this resolver can handle the given data source
func (r *DefaultPathResolver) CanResolve(data interface{}) bool {
	// This is a fallback resolver that handles maps
	_, ok := data.(map[string]interface{})
	return ok
}

// ResolveSegment resolves a single path segment in a map
func (r *DefaultPathResolver) ResolveSegment(data interface{}, segment PathSegment) (interface{}, bool) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return nil, false
	}

	// Get the value for the field
	value, exists := m[segment.Name]
	if !exists {
		return nil, false
	}

	// If there's an index expression, handle it
	if segment.IndexExpr != nil {
		// Array indexing
		arr, ok := value.([]interface{})
		if !ok {
			return nil, false
		}

		index := segment.IndexExpr.Value
		if index < 0 || index >= len(arr) {
			return nil, false
		}

		return arr[index], true
	}

	return value, true
}

// GetType determines the type of a value
func (r *DefaultPathResolver) GetType(data interface{}, segment PathSegment) *Type {
	value, found := r.ResolveSegment(data, segment)
	if !found {
		return &Type{PrimType: TypeUnknown}
	}

	return inferTypeFromValue(value)
}

// inferTypeFromValue infers the Effectus type from a Go value
func inferTypeFromValue(value interface{}) *Type {
	switch v := value.(type) {
	case bool:
		return &Type{PrimType: TypeBool}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return &Type{PrimType: TypeInt}
	case float32, float64:
		return &Type{PrimType: TypeFloat}
	case string:
		return &Type{PrimType: TypeString}
	case []interface{}:
		// For arrays, infer type from the first element if available
		elemType := &Type{PrimType: TypeUnknown}
		if len(v) > 0 {
			elemType = inferTypeFromValue(v[0])
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

// UnifiedPathResolver is a resolver that can handle multiple data types
type UnifiedPathResolver struct {
	typeSystem *TypeSystem
	debug      bool
}

// NewUnifiedPathResolver creates a new unified path resolver
func NewUnifiedPathResolver(typeSystem *TypeSystem, debug bool) *UnifiedPathResolver {
	return &UnifiedPathResolver{
		typeSystem: typeSystem,
		debug:      debug,
	}
}

// Resolve resolves a path in facts
func (r *UnifiedPathResolver) Resolve(facts effectus.Facts, path FactPath) (interface{}, bool) {
	// Extract raw data from facts
	var data interface{}

	switch f := facts.(type) {
	case *JSONFacts:
		data = f.data
	case *ProtoFacts:
		data = f.message
	default:
		// Legacy fallback
		return facts.Get(path.String())
	}

	// Traverse path segments
	current := data
	for _, segment := range path.segments {
		var ok bool
		current, ok = r.resolveSegment(current, segment)
		if !ok {
			return nil, false
		}
	}

	return current, true
}

// resolveSegment resolves a single segment based on data type
func (r *UnifiedPathResolver) resolveSegment(data interface{}, segment PathSegment) (interface{}, bool) {
	// JSON/Map handling
	if m, ok := data.(map[string]interface{}); ok {
		return r.resolveMapSegment(m, segment)
	}

	// Array handling
	if arr, ok := data.([]interface{}); ok {
		return r.resolveArraySegment(arr, segment)
	}

	// Proto handling
	if msg, ok := data.(proto.Message); ok {
		return r.resolveProtoSegment(msg, segment)
	}

	// Unknown type
	if r.debug {
		log.Printf("UnifiedPathResolver: unknown data type %T", data)
	}
	return nil, false
}

// resolveMapSegment resolves a segment in a map
func (r *UnifiedPathResolver) resolveMapSegment(m map[string]interface{}, segment PathSegment) (interface{}, bool) {
	// Get the field value
	value, exists := m[segment.Name]
	if !exists {
		if r.debug {
			log.Printf("Resolver: field %s not found in map", segment.Name)
		}
		return nil, false
	}

	// Handle indexing if present
	index, hasIndex := segment.GetIndex()
	if hasIndex {
		arr, ok := value.([]interface{})
		if !ok {
			if r.debug {
				log.Printf("Resolver: value at %s is not an array", segment.Name)
			}
			return nil, false
		}

		idx := *index
		if idx < 0 || idx >= len(arr) {
			if r.debug {
				log.Printf("Resolver: index %d out of bounds (0-%d)", idx, len(arr)-1)
			}
			return nil, false
		}

		return arr[idx], true
	}

	return value, true
}

// resolveArraySegment resolves a segment in an array
func (r *UnifiedPathResolver) resolveArraySegment(arr []interface{}, segment PathSegment) (interface{}, bool) {
	// Arrays can only be indexed
	index, hasIndex := segment.GetIndex()
	if !hasIndex {
		if r.debug {
			log.Printf("Resolver: cannot access field %s on array", segment.Name)
		}
		return nil, false
	}

	idx := *index
	if idx < 0 || idx >= len(arr) {
		if r.debug {
			log.Printf("Resolver: index %d out of bounds (0-%d)", idx, len(arr)-1)
		}
		return nil, false
	}

	return arr[idx], true
}

// resolveProtoSegment resolves a segment in a Protocol Buffer message
func (r *UnifiedPathResolver) resolveProtoSegment(protoMsg proto.Message, segment PathSegment) (interface{}, bool) {
	msg := protoMsg.ProtoReflect()

	// Try to find the field by name
	field := msg.Descriptor().Fields().ByName(protoreflect.Name(segment.Name))
	if field == nil {
		// Try with camelCase to snake_case conversion
		snakeFieldName := strcase.ToSnake(segment.Name)
		field = msg.Descriptor().Fields().ByName(protoreflect.Name(snakeFieldName))
		if field == nil {
			if r.debug {
				log.Printf("Resolver: field %s not found in proto", segment.Name)
			}
			return nil, false
		}
	}

	// Get the field value
	fieldValue := msg.Get(field)

	// Handle array indexing if present
	index, hasIndex := segment.GetIndex()
	if hasIndex {
		if !field.IsList() {
			if r.debug {
				log.Printf("Resolver: field %s is not a list", segment.Name)
			}
			return nil, false
		}

		list := fieldValue.List()
		idx := *index
		if idx < 0 || idx >= list.Len() {
			if r.debug {
				log.Printf("Resolver: index %d out of bounds (0-%d)", idx, list.Len()-1)
			}
			return nil, false
		}

		indexValue := list.Get(idx)
		return convertProtoValueToGo(indexValue, field), true
	}

	return convertProtoValueToGo(fieldValue, field), true
}

// Type returns the expected type at a path
func (r *UnifiedPathResolver) Type(path FactPath) *Type {
	// If the path has type information, use it
	if path.Type() != nil {
		return path.Type()
	}

	// Otherwise, look up the type in the type system
	typ, exists := r.typeSystem.GetFactType(path.String())
	if !exists {
		return &Type{PrimType: TypeUnknown}
	}
	return typ
}

// InferType infers the type of a value
func (r *UnifiedPathResolver) InferType(value interface{}) *Type {
	switch v := value.(type) {
	case bool:
		return &Type{PrimType: TypeBool}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return &Type{PrimType: TypeInt}
	case float32, float64:
		return &Type{PrimType: TypeFloat}
	case string:
		return &Type{PrimType: TypeString}
	case []interface{}:
		// For arrays, infer type from the first element if available
		elemType := &Type{PrimType: TypeUnknown}
		if len(v) > 0 {
			elemType = r.InferType(v[0])
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
	case proto.Message:
		// Handle proto messages
		return &Type{PrimType: TypeMap}
	default:
		return &Type{PrimType: TypeUnknown}
	}
}

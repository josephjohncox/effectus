package schema

import "github.com/effectus/effectus-go"

// FactPathResolverAdapter adapts the PathResolutionManager to the FactPathResolver interface
type FactPathResolverAdapter struct {
	typeSystem *TypeSystem
	manager    *PathResolutionManager
}

// Resolve implements the FactPathResolver interface
func (a *FactPathResolverAdapter) Resolve(facts effectus.Facts, path FactPath) (interface{}, bool) {
	// Try to get the raw data based on the facts type
	var data interface{}
	var ok bool

	// Handle known fact types
	switch f := facts.(type) {
	case *JSONFacts:
		data = f.data
		ok = true
	case *ProtoFacts:
		data = f.message
		ok = true
	default:
		// Fall back to using the Facts interface directly for backward compatibility
		return facts.Get(path.String())
	}

	if !ok {
		return nil, false
	}

	// Use the resolution manager to resolve the path
	return a.manager.ResolvePath(data, path)
}

// Type implements the FactPathResolver interface
func (a *FactPathResolverAdapter) Type(path FactPath) *Type {
	// If the path has type information, use it
	if path.Type() != nil {
		return path.Type()
	}

	// Otherwise, look up the type in the type system
	typ, exists := a.typeSystem.GetFactType(path.String())
	if !exists {
		return &Type{PrimType: TypeUnknown}
	}
	return typ
}

// CreateJSONFactPathResolver creates a FactPathResolver that uses JSON resolution
func CreateJSONFactPathResolver(typeSystem *TypeSystem) FactPathResolver {
	return &JSONPathResolverImpl{
		typeSystem: typeSystem,
	}
}

// JSONPathResolverImpl implements FactPathResolver for JSON data
type JSONPathResolverImpl struct {
	typeSystem *TypeSystem
}

// Resolve implements the FactPathResolver interface for JSON data
func (r *JSONPathResolverImpl) Resolve(facts effectus.Facts, path FactPath) (interface{}, bool) {
	// Try to get the JSON data
	jsonFacts, ok := facts.(*JSONFacts)
	if !ok {
		// Fall back to using the Facts interface
		return facts.Get(path.String())
	}

	return ResolveJSONPath(jsonFacts.data, path)
}

// Type implements the FactPathResolver interface
func (r *JSONPathResolverImpl) Type(path FactPath) *Type {
	// If the path has type information, use it
	if path.Type() != nil {
		return path.Type()
	}

	// Otherwise, look up the type in the type system
	if r.typeSystem != nil {
		typ, exists := r.typeSystem.GetFactType(path.String())
		if exists {
			return typ
		}
	}

	return &Type{PrimType: TypeUnknown}
}

// CreateProtoFactPathResolver creates a FactPathResolver that uses Proto resolution
func CreateProtoFactPathResolver(typeSystem *TypeSystem) FactPathResolver {
	return &ProtoFactPathResolver{
		typeSystem: typeSystem,
	}
}

// CreateUniversalFactPathResolver creates a FactPathResolver that can handle all data types
func CreateUniversalFactPathResolver(typeSystem *TypeSystem) FactPathResolver {
	// For now, return a JSONPathResolverImpl since it's the most complete implementation
	return &JSONPathResolverImpl{
		typeSystem: typeSystem,
	}
}

// CreateFactPathResolver creates a new fact path resolver with the given type system
// This is renamed from NewFactPathResolver to avoid conflicts
func CreateFactPathResolver(typeSystem *TypeSystem) FactPathResolver {
	// For now, return a JSONPathResolverImpl since it's the most complete implementation
	return &JSONPathResolverImpl{
		typeSystem: typeSystem,
	}
}

package schema

// FactPathResolverFactory provides factory methods for creating path resolvers
type FactPathResolverFactory struct {
	typeSystem *TypeSystem
}

// NewFactPathResolverFactory creates a new factory
func NewFactPathResolverFactory(typeSystem *TypeSystem) *FactPathResolverFactory {
	return &FactPathResolverFactory{
		typeSystem: typeSystem,
	}
}

// CreateResolver creates a unified resolver suitable for any fact type
func (f *FactPathResolverFactory) CreateResolver() FactPathResolver {
	return NewUnifiedPathResolver(f.typeSystem, false)
}

// CreateDebugResolver creates a resolver with debug logging enabled
func (f *FactPathResolverFactory) CreateDebugResolver() FactPathResolver {
	return NewUnifiedPathResolver(f.typeSystem, true)
}

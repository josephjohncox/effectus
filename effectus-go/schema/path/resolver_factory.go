package path

import (
	"github.com/effectus/effectus-go/schema/types"
)

// PathResolverFactory provides factory methods for creating path resolvers
type PathResolverFactory struct {
	typeSystem *types.TypeSystem
}

// NewPathResolverFactory creates a new factory
func NewPathResolverFactory(typeSystem *types.TypeSystem) *PathResolverFactory {
	return &PathResolverFactory{
		typeSystem: typeSystem,
	}
}

// CreateFactPathResolver creates a path resolver for facts
func (f *PathResolverFactory) CreateFactPathResolver(debug bool) FactPathResolver {
	return NewUnifiedPathResolver(f.typeSystem, debug)
}

// CreateEnhancedFactPathResolver creates an enhanced path resolver with caching
func (f *PathResolverFactory) CreateEnhancedFactPathResolver(debug bool) FactPathResolver {
	resolver := NewUnifiedPathResolver(f.typeSystem, debug)
	return NewCachedPathResolver(resolver)
}

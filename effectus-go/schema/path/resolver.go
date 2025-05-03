package path

import "github.com/effectus/effectus-go"

// ResolutionResult contains information about path resolution
type ResolutionResult struct {
	// Found indicates whether the value was found
	Found bool

	// Value is the resolved value, if found
	Value interface{}

	// Path is the path that was resolved
	Path FactPath

	// Type is the type information for the resolved value
	Type interface{}

	// IsIndexed indicates whether the path involves indexing
	IsIndexed bool

	// MatchedIndex is the actual index matched when resolving array paths
	MatchedIndex int
}

// SegmentResolver resolves individual path segments against data sources
type SegmentResolver interface {
	// CanResolve returns true if this resolver can handle the given data
	CanResolve(data interface{}) bool

	// ResolveSegment resolves a single path segment in the given data
	ResolveSegment(data interface{}, segment PathSegment) (interface{}, bool)

	// GetSegmentType returns the type information for a resolved segment
	GetSegmentType(data interface{}, segment PathSegment) interface{}
}

// PathResolver provides a higher-level interface for resolving entire paths
type PathResolver interface {
	// Resolve resolves a complete path against facts
	Resolve(facts effectus.Facts, path FactPath) (interface{}, bool)

	// ResolveWithContext resolves a path and provides detailed context
	ResolveWithContext(facts effectus.Facts, path FactPath) (interface{}, *ResolutionResult)

	// Type returns the type information for a path
	Type(path FactPath) interface{}
}

// Manager provides a registry for segment resolvers
type Manager struct {
	resolvers []SegmentResolver
}

// NewManager creates a new path resolution manager
func NewManager(resolvers ...SegmentResolver) *Manager {
	return &Manager{
		resolvers: resolvers,
	}
}

// RegisterResolver adds a new resolver to the manager
func (m *Manager) RegisterResolver(resolver SegmentResolver) {
	m.resolvers = append(m.resolvers, resolver)
}

// FindResolver finds a resolver that can handle the given data
func (m *Manager) FindResolver(data interface{}) (SegmentResolver, bool) {
	for _, resolver := range m.resolvers {
		if resolver.CanResolve(data) {
			return resolver, true
		}
	}
	return nil, false
}

// ResolvePath resolves a complete path in the given data using registered resolvers
func (m *Manager) ResolvePath(data interface{}, path FactPath) (interface{}, bool) {
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

// GetPathType determines the type at the given path using resolvers
func (m *Manager) GetPathType(data interface{}, path FactPath) interface{} {
	// Handle path with existing type information
	if typeInfo := path.Type(); typeInfo != nil {
		return typeInfo
	}

	current := data
	var segmentType interface{}

	// Traverse each segment
	for i, segment := range path.Segments() {
		// Find a resolver for the current data
		resolver, found := m.FindResolver(current)
		if !found {
			return nil
		}

		// Get the type for this segment
		segmentType = resolver.GetSegmentType(current, segment)

		// If this is the last segment, return its type
		if i == len(path.Segments())-1 {
			return segmentType
		}

		// Otherwise, resolve the segment and continue
		var ok bool
		current, ok = resolver.ResolveSegment(current, segment)
		if !ok {
			return segmentType
		}
	}

	return segmentType
}

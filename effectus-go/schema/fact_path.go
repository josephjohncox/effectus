package schema

import (
	"fmt"
	"strings"
	"sync"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/unified/pathutil"
)

// PathSegment represents a single segment in a fact path
// We're keeping IndexExpr for backward compatibility
type PathSegment struct {
	// Name of the segment
	Name string

	// IndexExpr for array or map access
	IndexExpr *IndexExpression

	// Index is a direct reference to the array index (new method)
	// Not exported to avoid breaking existing code
	index *int

	// StringKey is for map key access (e.g., ["key"])
	stringKey *string
}

// IndexExpression represents an index or key access in a path
type IndexExpression struct {
	// Value for array index (e.g., [0])
	Value int

	// KeyExpr for map key (future support for map[key] syntax)
	KeyExpr string
}

// String returns the string representation of the path segment
func (s PathSegment) String() string {
	if s.IndexExpr != nil {
		return fmt.Sprintf("%s[%d]", s.Name, s.IndexExpr.Value)
	}
	if s.stringKey != nil {
		return fmt.Sprintf(`%s["%s"]`, s.Name, *s.stringKey)
	}
	return s.Name
}

// GetIndex returns the index value regardless of which field it's stored in
func (s PathSegment) GetIndex() (*int, bool) {
	if s.IndexExpr != nil {
		val := s.IndexExpr.Value
		return &val, true
	}
	return s.index, s.index != nil
}

// HasStringKey returns true if this segment has a string key
func (s PathSegment) HasStringKey() bool {
	return s.stringKey != nil
}

// StringKey returns the string key value, or empty string if none
func (s PathSegment) StringKey() string {
	if s.stringKey == nil {
		return ""
	}
	return *s.stringKey
}

// SetStringKey sets the string key for map access
func (s *PathSegment) SetStringKey(key string) {
	s.stringKey = &key
}

// FactPath represents a strongly-typed path to a fact in the system
type FactPath struct {
	namespace string
	segments  []PathSegment
	typ       *Type // The expected type at this path
}

// NewFactPath creates a new fact path with the given namespace, segments, and type
func NewFactPath(namespace string, typ *Type, segments ...PathSegment) FactPath {
	return FactPath{
		namespace: namespace,
		segments:  segments,
		typ:       typ,
	}
}

// NewFactPathFromStrings creates a fact path from string segments (without indexes)
func NewFactPathFromStrings(namespace string, typ *Type, segments ...string) FactPath {
	pathSegments := make([]PathSegment, len(segments))
	for i, seg := range segments {
		pathSegments[i] = PathSegment{Name: seg}
	}
	return NewFactPath(namespace, typ, pathSegments...)
}

// String returns the full path as a dot-separated string
func (p FactPath) String() string {
	if len(p.segments) == 0 {
		return p.namespace
	}

	segmentStrs := make([]string, len(p.segments))
	for i, seg := range p.segments {
		segmentStrs[i] = seg.String()
	}

	return p.namespace + "." + strings.Join(segmentStrs, ".")
}

// Namespace returns the namespace of this path
func (p FactPath) Namespace() string {
	return p.namespace
}

// Segments returns the path segments (excluding namespace)
func (p FactPath) Segments() []PathSegment {
	return p.segments
}

// SegmentStrings returns the path segments as simple strings
func (p FactPath) SegmentStrings() []string {
	result := make([]string, len(p.segments))
	for i, seg := range p.segments {
		result[i] = seg.String()
	}
	return result
}

// Child creates a new path by appending segments to this path
func (p FactPath) Child(childType *Type, segments ...PathSegment) FactPath {
	newSegments := make([]PathSegment, len(p.segments)+len(segments))
	copy(newSegments, p.segments)
	copy(newSegments[len(p.segments):], segments)

	return FactPath{
		namespace: p.namespace,
		segments:  newSegments,
		typ:       childType,
	}
}

// Type returns the expected type at this path
func (p FactPath) Type() *Type {
	return p.typ
}

// ParseFactPath parses a string path into a FactPath
func ParseFactPath(path string) (FactPath, error) {
	// Use the shared path parser for consistent parsing across packages
	namespace, elements, err := pathutil.ParsePath(path)
	if err != nil {
		return FactPath{}, err
	}

	// Convert pathutil.PathElement to schema.PathSegment
	segments := make([]PathSegment, len(elements))
	for i, elem := range elements {
		segment := PathSegment{
			Name: elem.Name(),
		}

		// Handle indexing
		if elem.HasIndex() {
			segment.IndexExpr = &IndexExpression{
				Value: elem.Index(),
			}
		}

		// Handle string keys (new feature)
		if elem.HasStringKey() {
			key := elem.StringKey()
			segment.SetStringKey(key)
		}

		segments[i] = segment
	}

	return FactPath{
		namespace: namespace,
		segments:  segments,
	}, nil
}

// FromPathElements creates a FactPath from pathutil elements
func FromPathElements(namespace string, elements []pathutil.PathElement, typ *Type) FactPath {
	segments := make([]PathSegment, len(elements))
	for i, elem := range elements {
		if elem.HasIndex() {
			segments[i] = PathSegment{
				Name: elem.Name(),
				IndexExpr: &IndexExpression{
					Value: elem.Index(),
				},
			}
		} else {
			segments[i] = PathSegment{Name: elem.Name()}
		}
	}

	return FactPath{
		namespace: namespace,
		segments:  segments,
		typ:       typ,
	}
}

// AsPathElements returns the path elements in the shared format
func (p FactPath) AsPathElements() []pathutil.PathElement {
	elements := make([]pathutil.PathElement, len(p.segments))
	for i, seg := range p.segments {
		var index *int
		if seg.IndexExpr != nil {
			val := seg.IndexExpr.Value
			index = &val
		} else if idx, ok := seg.GetIndex(); ok {
			index = idx
		}
		elements[i] = pathutil.NewPathElement(seg.Name, index)
	}
	return elements
}

// FactPathResolver resolves fact paths to values
type FactPathResolver interface {
	// Resolve returns the value at the given path
	Resolve(facts effectus.Facts, path FactPath) (interface{}, bool)

	// ResolveWithContext returns the value with detailed resolution information
	ResolveWithContext(facts effectus.Facts, path FactPath) (interface{}, *PathResolutionResult)

	// Type returns the expected type at a path
	Type(path FactPath) *Type
}

// PathCache provides efficient caching of parsed paths
type PathCache struct {
	cache map[string]FactPath
	mu    sync.RWMutex
}

// NewPathCache creates a new path cache
func NewPathCache() *PathCache {
	return &PathCache{
		cache: make(map[string]FactPath),
	}
}

// Get retrieves a cached path or parses it if not found
func (c *PathCache) Get(path string) (FactPath, error) {
	// First check the cache with a read lock
	c.mu.RLock()
	if cached, ok := c.cache[path]; ok {
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	// Not in cache, parse it
	parsed, err := ParseFactPath(path)
	if err != nil {
		return FactPath{}, err
	}

	// Store in cache with a write lock
	c.mu.Lock()
	c.cache[path] = parsed
	c.mu.Unlock()

	return parsed, nil
}

// Clear empties the cache
func (c *PathCache) Clear() {
	c.mu.Lock()
	c.cache = make(map[string]FactPath)
	c.mu.Unlock()
}

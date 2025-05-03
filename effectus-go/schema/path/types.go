// Package path provides path representation and resolution for Effectus facts.
package path

import (
	"fmt"
	"strings"

	pathutil "../../unified/pathutil"
)

// PathSegment represents a single segment in a fact path
type PathSegment struct {
	// Name of the segment
	Name string

	// Index for array access (e.g., [0])
	Index *int

	// StringKey for map key access (e.g., ["key"])
	StringKey *string
}

// String returns the string representation of the path segment
func (s PathSegment) String() string {
	if s.Index != nil {
		return fmt.Sprintf("%s[%d]", s.Name, *s.Index)
	}
	if s.StringKey != nil {
		return fmt.Sprintf(`%s["%s"]`, s.Name, *s.StringKey)
	}
	return s.Name
}

// HasIndex returns true if this segment has an array index
func (s PathSegment) HasIndex() bool {
	return s.Index != nil
}

// GetIndex returns the index value if it exists
func (s PathSegment) GetIndex() (int, bool) {
	if s.Index != nil {
		return *s.Index, true
	}
	return 0, false
}

// HasStringKey returns true if this segment has a string key
func (s PathSegment) HasStringKey() bool {
	return s.StringKey != nil
}

// GetStringKey returns the string key value if it exists
func (s PathSegment) GetStringKey() (string, bool) {
	if s.StringKey != nil {
		return *s.StringKey, true
	}
	return "", false
}

// FactPath represents a strongly-typed path to a fact in the system
type FactPath struct {
	namespace string
	segments  []PathSegment
	typeInfo  interface{} // Type information - intentionally generic
}

// NewFactPath creates a new fact path with the given namespace and segments
func NewFactPath(namespace string, segments ...PathSegment) FactPath {
	return FactPath{
		namespace: namespace,
		segments:  segments,
	}
}

// WithType returns a copy of this path with the specified type information
func (p FactPath) WithType(typeInfo interface{}) FactPath {
	newPath := p
	newPath.typeInfo = typeInfo
	return newPath
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

// SegmentNames returns just the names of the segments as strings
func (p FactPath) SegmentNames() []string {
	result := make([]string, len(p.segments))
	for i, seg := range p.segments {
		result[i] = seg.Name
	}
	return result
}

// Child creates a new path by appending segments to this path
func (p FactPath) Child(segments ...PathSegment) FactPath {
	newSegments := make([]PathSegment, len(p.segments)+len(segments))
	copy(newSegments, p.segments)
	copy(newSegments[len(p.segments):], segments)

	return FactPath{
		namespace: p.namespace,
		segments:  newSegments,
		typeInfo:  nil, // Type information is cleared for child paths
	}
}

// Type returns the type information associated with this path
func (p FactPath) Type() interface{} {
	return p.typeInfo
}

// IsEmpty returns true if the path has no namespace and no segments
func (p FactPath) IsEmpty() bool {
	return p.namespace == "" && len(p.segments) == 0
}

// ToPathElements converts this path to pathutil.PathElement format
func (p FactPath) ToPathElements() []pathutil.PathElement {
	elements := make([]pathutil.PathElement, len(p.segments))
	for i, seg := range p.segments {
		elem := pathutil.NewElement(seg.Name)

		if seg.Index != nil {
			elem = elem.WithIndex(*seg.Index)
		}

		if seg.StringKey != nil {
			elem = elem.WithStringKey(*seg.StringKey)
		}

		elements[i] = elem
	}
	return elements
}

// FromPathElements creates a FactPath from pathutil elements
func FromPathElements(namespace string, elements []pathutil.PathElement) FactPath {
	segments := make([]PathSegment, len(elements))
	for i, elem := range elements {
		segment := PathSegment{
			Name: elem.Name(),
		}

		if elem.HasIndex() {
			idx := elem.Index()
			segment.Index = &idx
		}

		if elem.HasStringKey() {
			key := elem.StringKey()
			segment.StringKey = &key
		}

		segments[i] = segment
	}

	return FactPath{
		namespace: namespace,
		segments:  segments,
	}
}

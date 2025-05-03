// Package path provides core path manipulation functionality with no external dependencies.
package path

import (
	"fmt"
)

// PathElement represents a single segment in a path with optional indexing
type PathElement struct {
	Name      string  // The segment name
	Index     *int    // Optional array index
	StringKey *string // Optional string key
	// Type information can be attached to each element
	Type interface{} // The type of this element (can be any type system representation)
}

// HasIndex returns true if this element has an array index
func (e PathElement) HasIndex() bool {
	return e.Index != nil
}

// GetIndex returns the index value if it exists
func (e PathElement) GetIndex() (int, bool) {
	if e.Index != nil {
		return *e.Index, true
	}
	return 0, false
}

// HasStringKey returns true if this element has a string key
func (e PathElement) HasStringKey() bool {
	return e.StringKey != nil
}

// GetStringKey returns the string key value if it exists
func (e PathElement) GetStringKey() (string, bool) {
	if e.StringKey != nil {
		return *e.StringKey, true
	}
	return "", false
}

// String returns the string representation of this element
func (e PathElement) String() string {
	if e.Index != nil {
		return fmt.Sprintf("%s[%d]", e.Name, *e.Index)
	}
	if e.StringKey != nil {
		return fmt.Sprintf(`%s["%s"]`, e.Name, *e.StringKey)
	}
	return e.Name
}

// Path represents a complete path including namespace and elements
type Path struct {
	Namespace string        // The path namespace
	Elements  []PathElement // The path elements
	Type      interface{}   // The type of this path (can be any type system representation)
}

// String returns the string representation of the entire path
func (p Path) String() string {
	if len(p.Elements) == 0 {
		return p.Namespace
	}

	parts := make([]string, len(p.Elements))
	for i, elem := range p.Elements {
		parts[i] = elem.String()
	}

	return p.Namespace + "." + Join(parts)
}

// Join joins string segments with dots
func Join(segments []string) string {
	if len(segments) == 0 {
		return ""
	}

	result := segments[0]
	for i := 1; i < len(segments); i++ {
		result += "." + segments[i]
	}
	return result
}

// NewElement creates a basic path element with just a name
func NewElement(name string) PathElement {
	return PathElement{
		Name: name,
	}
}

// WithIndex returns a copy of the element with the given index
func (e PathElement) WithIndex(index int) PathElement {
	newElem := e
	newElem.Index = &index
	newElem.StringKey = nil // Clear string key when index is set
	return newElem
}

// WithStringKey returns a copy of the element with the given string key
func (e PathElement) WithStringKey(key string) PathElement {
	newElem := e
	newElem.StringKey = &key
	newElem.Index = nil // Clear index when string key is set
	return newElem
}

// WithType returns a copy of the element with the given type
func (e PathElement) WithType(typ interface{}) PathElement {
	newElem := e
	newElem.Type = typ
	return newElem
}

// NewPath creates a new Path with the given namespace and elements
func NewPath(namespace string, elements []PathElement) Path {
	return Path{
		Namespace: namespace,
		Elements:  elements,
	}
}

// WithType returns a copy of the path with the given type
func (p Path) WithType(typ interface{}) Path {
	newPath := p
	newPath.Type = typ
	return newPath
}

// GetSegments returns all segments as strings
func (p Path) GetSegments() []string {
	result := make([]string, len(p.Elements))
	for i, elem := range p.Elements {
		result[i] = elem.String()
	}
	return result
}

// IsEmpty returns true if the path has no namespace and no elements
func (p Path) IsEmpty() bool {
	return p.Namespace == "" && len(p.Elements) == 0
}

// Child returns a new path by appending elements to this path
func (p Path) Child(elements ...PathElement) Path {
	newElements := make([]PathElement, len(p.Elements)+len(elements))
	copy(newElements, p.Elements)
	copy(newElements[len(p.Elements):], elements)

	return Path{
		Namespace: p.Namespace,
		Elements:  newElements,
		Type:      p.Type, // Preserve the parent path type
	}
}

// Clone creates a deep copy of the path
func (p Path) Clone() Path {
	elements := make([]PathElement, len(p.Elements))
	for i, elem := range p.Elements {
		elements[i] = elem
	}

	return Path{
		Namespace: p.Namespace,
		Elements:  elements,
		Type:      p.Type,
	}
}

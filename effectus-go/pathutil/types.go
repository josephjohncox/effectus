// Package pathutil provides utilities for working with paths
package pathutil

import (
	"fmt"
	"strconv"
	"strings"
)

// PathElement represents a single segment in a path with optional indexing
type PathElement struct {
	// Name is the segment name
	Name string

	// Index is the array index (-1 if not an array access)
	index int

	// StringKey is the map key (empty if not a map access)
	stringKey string
}

// NewElement creates a basic path element with just a name
func NewElement(name string) PathElement {
	return PathElement{
		Name:  name,
		index: -1,
	}
}

// WithIndex creates a copy of the element with an index
func (e PathElement) WithIndex(index int) PathElement {
	e.index = index
	e.stringKey = ""
	return e
}

// WithStringKey creates a copy of the element with a string key
func (e PathElement) WithStringKey(key string) PathElement {
	e.stringKey = key
	e.index = -1
	return e
}

// HasIndex returns true if this element has an index
func (e PathElement) HasIndex() bool {
	return e.index >= 0
}

// HasStringKey returns true if this element has a string key
func (e PathElement) HasStringKey() bool {
	return e.stringKey != ""
}

// GetIndex returns the index and whether it exists
func (e PathElement) GetIndex() (int, bool) {
	return e.index, e.index >= 0
}

// GetStringKey returns the string key and whether it exists
func (e PathElement) GetStringKey() (string, bool) {
	return e.stringKey, e.stringKey != ""
}

// String returns a string representation of the element
func (e PathElement) String() string {
	if e.index >= 0 {
		return fmt.Sprintf("%s[%d]", e.Name, e.index)
	}
	if e.stringKey != "" {
		return fmt.Sprintf("%s[\"%s\"]", e.Name, e.stringKey)
	}
	return e.Name
}

// Path represents a complete path including namespace and elements
type Path struct {
	// Namespace is the top-level namespace
	Namespace string

	// Elements are the path elements
	Elements []PathElement
}

// NewPath creates a new Path with the given namespace and elements
func NewPath(namespace string, elements []PathElement) Path {
	return Path{
		Namespace: namespace,
		Elements:  elements,
	}
}

// String returns a string representation of the entire path
func (p Path) String() string {
	if len(p.Elements) == 0 {
		return p.Namespace
	}

	var sb strings.Builder
	sb.WriteString(p.Namespace)

	for _, elem := range p.Elements {
		sb.WriteString(".")
		sb.WriteString(elem.Name)

		if elem.index >= 0 {
			sb.WriteString("[")
			sb.WriteString(strconv.Itoa(elem.index))
			sb.WriteString("]")
		} else if elem.stringKey != "" {
			sb.WriteString("[\"")
			sb.WriteString(elem.stringKey)
			sb.WriteString("\"]")
		}
	}

	return sb.String()
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
	}
}

// Clone creates a deep copy of the path
func (p Path) Clone() Path {
	elements := make([]PathElement, len(p.Elements))
	for i, elem := range p.Elements {
		elements[i] = PathElement{
			Name:      elem.Name,
			index:     elem.index,
			stringKey: elem.stringKey,
		}
	}

	return Path{
		Namespace: p.Namespace,
		Elements:  elements,
	}
}

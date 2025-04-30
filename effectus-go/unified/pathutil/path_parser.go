// Package pathutil provides utilities for working with paths across different parts
// of the Effectus system without creating import cycles.
package pathutil

import (
	"fmt"
	"strconv"
	"strings"
)

// PathElement represents a single segment in a path, with optional indexing
type PathElement interface {
	// Name returns the segment name
	Name() string

	// HasIndex returns true if this segment has an index
	HasIndex() bool

	// Index returns the index value, or -1 if there is no index
	Index() int

	// String returns the string representation of this segment (e.g., "orders[0]")
	String() string
}

// PathInfo represents a path to a fact or other value in the system
type PathInfo interface {
	// Namespace returns the namespace of this path
	Namespace() string

	// Elements returns the path elements (excluding namespace)
	Elements() []PathElement

	// String returns the full path as a dot-separated string
	String() string
}

// SimplePathElement is a basic implementation of PathElement
type SimplePathElement struct {
	name     string
	hasIndex bool
	indexVal int
}

// Name returns the segment name
func (e SimplePathElement) Name() string {
	return e.name
}

// HasIndex returns true if this segment has an index
func (e SimplePathElement) HasIndex() bool {
	return e.hasIndex
}

// Index returns the index value, or -1 if there is no index
func (e SimplePathElement) Index() int {
	if !e.hasIndex {
		return -1
	}
	return e.indexVal
}

// String returns the string representation of this segment
func (e SimplePathElement) String() string {
	if !e.hasIndex {
		return e.name
	}
	return fmt.Sprintf("%s[%d]", e.name, e.indexVal)
}

// NewPathElement creates a new SimplePathElement with the given name and optional index
func NewPathElement(name string, index *int) SimplePathElement {
	if index == nil {
		return SimplePathElement{
			name:     name,
			hasIndex: false,
		}
	}

	return SimplePathElement{
		name:     name,
		hasIndex: true,
		indexVal: *index,
	}
}

// ParsePath parses a string path into a namespace and path elements
func ParsePath(path string) (string, []SimplePathElement, error) {
	// Check for empty path
	if path == "" {
		return "", nil, fmt.Errorf("empty path")
	}

	// Split the path into parts
	parts := strings.Split(path, ".")
	if len(parts) < 1 {
		return "", nil, fmt.Errorf("invalid path: %s", path)
	}

	// Extract namespace and initialize segments
	namespace := parts[0]
	elements := make([]SimplePathElement, 0, len(parts)-1)

	// Process each segment for potential array indices
	for i := 1; i < len(parts); i++ {
		part := parts[i]

		// Check for array indexing (field[0] syntax)
		indexStart := strings.Index(part, "[")
		if indexStart > 0 && strings.HasSuffix(part, "]") {
			fieldName := part[:indexStart]
			indexStr := part[indexStart+1 : len(part)-1]

			// Parse the index
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				return "", nil, fmt.Errorf("invalid array index in path segment '%s': %w", part, err)
			}

			// Create a new indexed element
			elements = append(elements, SimplePathElement{
				name:     fieldName,
				hasIndex: true,
				indexVal: index,
			})
		} else {
			// Regular field without indexing
			elements = append(elements, SimplePathElement{
				name:     part,
				hasIndex: false,
			})
		}
	}

	return namespace, elements, nil
}

// RenderPath converts a namespace and elements back to a string path
func RenderPath(namespace string, elements []SimplePathElement) string {
	if len(elements) == 0 {
		return namespace
	}

	parts := make([]string, len(elements))
	for i, elem := range elements {
		parts[i] = elem.String()
	}

	return namespace + "." + strings.Join(parts, ".")
}

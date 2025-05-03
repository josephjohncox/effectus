// Package pathutil provides utilities for working with paths across different parts
// of the Effectus system without creating import cycles.
package pathutil

import (
	"fmt"
	"regexp"
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

	// HasStringKey returns true if this segment has a string key
	HasStringKey() bool

	// StringKey returns the string key, or empty string if none
	StringKey() string

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
	name      string
	hasIndex  bool
	indexVal  int
	hasStrKey bool
	stringKey string
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

// HasStringKey returns true if this segment has a string key
func (e SimplePathElement) HasStringKey() bool {
	return e.hasStrKey
}

// StringKey returns the string key, or empty string if none
func (e SimplePathElement) StringKey() string {
	if !e.hasStrKey {
		return ""
	}
	return e.stringKey
}

// String returns the string representation of this segment
func (e SimplePathElement) String() string {
	if e.hasIndex {
		return fmt.Sprintf("%s[%d]", e.name, e.indexVal)
	}
	if e.hasStrKey {
		return fmt.Sprintf(`%s["%s"]`, e.name, e.stringKey)
	}
	return e.name
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

// NewPathElementWithStringKey creates a new SimplePathElement with string key access
func NewPathElementWithStringKey(name string, key string) SimplePathElement {
	return SimplePathElement{
		name:      name,
		hasStrKey: true,
		stringKey: key,
	}
}

// ParsePath parses a string path into a namespace and path elements
func ParsePath(path string) (string, []SimplePathElement, error) {
	// Check for empty path
	if path == "" {
		return "", nil, fmt.Errorf("empty path")
	}

	// First check if path contains any indexing brackets
	if !strings.Contains(path, "[") {
		// Simple path with just dots, use the simpler parsing
		return parseSimplePath(path)
	}

	// Path with indexes requires more complex parsing
	return parseComplexPath(path)
}

// parseSimplePath handles dot-separated paths without any indexing
func parseSimplePath(path string) (string, []SimplePathElement, error) {
	parts := strings.Split(path, ".")
	if len(parts) < 1 {
		return "", nil, fmt.Errorf("invalid path: %s", path)
	}

	namespace := parts[0]
	elements := make([]SimplePathElement, 0, len(parts)-1)

	for i := 1; i < len(parts); i++ {
		elements = append(elements, SimplePathElement{
			name: parts[i],
		})
	}

	return namespace, elements, nil
}

// parseComplexPath handles paths that may contain array indexing or map keys
func parseComplexPath(path string) (string, []SimplePathElement, error) {
	// First get the namespace (everything before the first dot)
	firstDot := strings.Index(path, ".")
	if firstDot < 0 {
		return path, nil, nil
	}

	namespace := path[:firstDot]
	remainingPath := path[firstDot+1:]

	// Check for invalid array indices - this will catch syntax like [x] that the regex won't match
	// but is clearly meant to be an array index
	invalidIndexPattern := `\[[^0-9"\]]+\]`
	invalidRe := regexp.MustCompile(invalidIndexPattern)
	if invalidRe.MatchString(remainingPath) {
		return "", nil, fmt.Errorf("invalid array index in path: %s", path)
	}

	// Now parse the rest of the path with a regex pattern
	elements := make([]SimplePathElement, 0)

	// Match patterns like:
	// - field
	// - field[0]
	// - field["key"]
	pattern := `([a-zA-Z_]\w*)(?:\[(\d+)\]|\["([^"]+)"\])?`
	re := regexp.MustCompile(pattern)

	// Split by dots, but handle special case where dots appear in string keys
	segments := strings.Split(remainingPath, ".")

	for _, segment := range segments {
		matches := re.FindStringSubmatch(segment)
		if matches == nil {
			return "", nil, fmt.Errorf("invalid path segment: %s", segment)
		}

		fieldName := matches[1]

		// Check for array index
		if matches[2] != "" {
			index, _ := strconv.Atoi(matches[2])
			elements = append(elements, SimplePathElement{
				name:     fieldName,
				hasIndex: true,
				indexVal: index,
			})
			continue
		}

		// Check for string key
		if matches[3] != "" {
			elements = append(elements, SimplePathElement{
				name:      fieldName,
				hasStrKey: true,
				stringKey: matches[3],
			})
			continue
		}

		// Just a field name
		elements = append(elements, SimplePathElement{
			name: fieldName,
		})
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

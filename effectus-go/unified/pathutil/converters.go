package pathutil

import (
	"strings"
)

// SimplePath is a simple implementation of PathInfo
type SimplePath struct {
	namespace string
	elements  []SimplePathElement
}

// Namespace returns the namespace of this path
func (p SimplePath) Namespace() string {
	return p.namespace
}

// Elements returns the path elements (excluding namespace)
func (p SimplePath) Elements() []PathElement {
	result := make([]PathElement, len(p.elements))
	for i, elem := range p.elements {
		result[i] = elem
	}
	return result
}

// String returns the full path as a dot-separated string
func (p SimplePath) String() string {
	return RenderPath(p.namespace, p.elements)
}

// FromString parses a string path into a SimplePath
func FromString(path string) (SimplePath, error) {
	namespace, elements, err := ParsePath(path)
	if err != nil {
		return SimplePath{}, err
	}

	return SimplePath{
		namespace: namespace,
		elements:  elements,
	}, nil
}

// PathToString converts a PathInfo to its string representation
func PathToString(path PathInfo) string {
	if path == nil {
		return ""
	}
	return path.String()
}

// ElementsToStrings converts path elements to string segments
func ElementsToStrings(elements []PathElement) []string {
	result := make([]string, len(elements))
	for i, elem := range elements {
		result[i] = elem.String()
	}
	return result
}

// StringToElements converts a string path to elements
func StringToElements(path string) (string, []PathElement, error) {
	namespace, elements, err := ParsePath(path)
	if err != nil {
		return "", nil, err
	}

	result := make([]PathElement, len(elements))
	for i, elem := range elements {
		result[i] = elem
	}

	return namespace, result, nil
}

// JoinPath joins a namespace and segments into a string path
func JoinPath(namespace string, segments []string) string {
	if len(segments) == 0 {
		return namespace
	}
	return namespace + "." + strings.Join(segments, ".")
}

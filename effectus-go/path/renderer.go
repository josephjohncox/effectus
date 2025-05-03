package path

import (
	"strconv"
	"strings"
)

// RenderPath converts a namespace and elements back to a string path
func RenderPath(namespace string, elements []PathElement) string {
	if len(elements) == 0 {
		return namespace
	}

	parts := make([]string, len(elements))
	for i, elem := range elements {
		parts[i] = elem.String()
	}

	return namespace + "." + strings.Join(parts, ".")
}

// ElementsToStrings converts path elements to string segments
func ElementsToStrings(elements []PathElement) []string {
	result := make([]string, len(elements))
	for i, elem := range elements {
		result[i] = elem.String()
	}
	return result
}

// JoinPathSegments joins a namespace and string segments into a path
func JoinPathSegments(namespace string, segments []string) string {
	if len(segments) == 0 {
		return namespace
	}
	return namespace + "." + strings.Join(segments, ".")
}

// GetNameAndSubscript splits a segment like "items[0]" into ("items", 0)
func GetNameAndSubscript(segment string) (string, *int, *string) {
	nameEnd := strings.IndexRune(segment, '[')
	if nameEnd == -1 {
		return segment, nil, nil
	}

	if !strings.HasSuffix(segment, "]") {
		return segment, nil, nil
	}

	name := segment[:nameEnd]
	bracketContent := segment[nameEnd+1 : len(segment)-1]

	// Check if it's a numeric index
	if idx, err := strconv.Atoi(bracketContent); err == nil {
		return name, &idx, nil
	}

	// Check if it's a string key with quotes
	if strings.HasPrefix(bracketContent, "\"") && strings.HasSuffix(bracketContent, "\"") {
		strVal := bracketContent[1 : len(bracketContent)-1]
		return name, nil, &strVal
	}

	// Neither a valid number nor a quoted string
	return segment, nil, nil
}

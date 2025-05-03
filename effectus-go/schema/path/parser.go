package path

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	// Regular expressions for parsing paths
	segmentRegex   = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_]*)(\[\d+\]|\["[^"]+"\])?$`)
	indexRegex     = regexp.MustCompile(`\[(\d+)\]`)
	stringKeyRegex = regexp.MustCompile(`\["([^"]+)"\]`)
)

// ParsePath parses a string path into a FactPath
func ParsePath(path string) (FactPath, error) {
	if path == "" {
		return FactPath{}, fmt.Errorf("empty path")
	}

	// Split into namespace and segments
	parts := strings.Split(path, ".")

	// Handle paths with and without namespace
	var namespace string
	var segmentParts []string

	if len(parts) == 1 {
		// No dots, just segments or namespace
		if strings.Contains(parts[0], "[") {
			// Must be a segment
			namespace = ""
			segmentParts = parts
		} else {
			// Must be a namespace
			namespace = parts[0]
			segmentParts = []string{}
		}
	} else {
		// Has dots, first part is namespace, rest are segments
		namespace = parts[0]
		segmentParts = parts[1:]
	}

	// Parse segments
	segments := make([]PathSegment, 0, len(segmentParts))
	for _, part := range segmentParts {
		if part == "" {
			continue
		}

		segment, err := parseSegment(part)
		if err != nil {
			return FactPath{}, fmt.Errorf("invalid segment '%s': %w", part, err)
		}

		segments = append(segments, segment)
	}

	return FactPath{
		namespace: namespace,
		segments:  segments,
	}, nil
}

// parseSegment parses a path segment string into a PathSegment
func parseSegment(segmentStr string) (PathSegment, error) {
	if !segmentRegex.MatchString(segmentStr) {
		return PathSegment{}, fmt.Errorf("invalid segment format: %s", segmentStr)
	}

	// Extract segment name
	matches := segmentRegex.FindStringSubmatch(segmentStr)
	name := matches[1]

	segment := PathSegment{
		Name: name,
	}

	// Check for array index
	if idxMatches := indexRegex.FindStringSubmatch(segmentStr); len(idxMatches) > 1 {
		idx, err := strconv.Atoi(idxMatches[1])
		if err != nil {
			return PathSegment{}, fmt.Errorf("invalid index: %s", idxMatches[1])
		}
		segment.Index = &idx
	}

	// Check for string key
	if keyMatches := stringKeyRegex.FindStringSubmatch(segmentStr); len(keyMatches) > 1 {
		key := keyMatches[1]
		segment.StringKey = &key
	}

	return segment, nil
}

// ValidatePath checks if a path string is valid
func ValidatePath(path string) bool {
	_, err := ParsePath(path)
	return err == nil
}

// NewSegment creates a simple segment with just a name
func NewSegment(name string) PathSegment {
	return PathSegment{
		Name: name,
	}
}

// NewIndexSegment creates a segment with an array index
func NewIndexSegment(name string, index int) PathSegment {
	return PathSegment{
		Name:  name,
		Index: &index,
	}
}

// NewStringKeySegment creates a segment with a string key
func NewStringKeySegment(name string, key string) PathSegment {
	return PathSegment{
		Name:      name,
		StringKey: &key,
	}
}

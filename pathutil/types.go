package pathutil

import (
	"fmt"
	"strings"
)

// Path is a string using gjson path syntax
// Examples:
//   - "user.profile.name"
//   - "posts[0].title"
//   - "data.items[2].tags[\"priority\"]"
type Path string

// Namespace returns the first segment of the path (before first dot)
func (p Path) Namespace() string {
	str := string(p)
	idx := strings.Index(str, ".")
	if idx == -1 {
		return str
	}
	return str[:idx]
}

// WithNamespace creates a new path with the specified namespace
func (p Path) WithNamespace(namespace string) Path {
	if p == "" {
		return Path(namespace)
	}

	// Check if path already has a namespace
	currentNs := p.Namespace()
	if currentNs != "" {
		// Remove the current namespace from the path
		rest := string(p[len(currentNs):])
		if len(rest) > 0 && rest[0] == '.' {
			rest = rest[1:]
		}

		if rest == "" {
			return Path(namespace)
		}
		return Path(namespace + "." + rest)
	}

	return Path(namespace + "." + string(p))
}

// String returns the path as a string
func (p Path) String() string {
	return string(p)
}

// Child returns a new path by appending a child segment
func (p Path) Child(segment string) Path {
	if p == "" {
		return Path(segment)
	}
	return Path(fmt.Sprintf("%s.%s", p, segment))
}

// ResolutionResult contains details about path resolution
type ResolutionResult struct {
	Path   string      // The original path
	Exists bool        // Whether the path exists
	Value  interface{} // The resolved value (if exists)
	Error  error       // Error that occurred during resolution (if any)
}

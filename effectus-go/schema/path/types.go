// Package path provides path representation and resolution for Effectus facts.
package path

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/effectus/effectus-go/common"
	basepath "github.com/effectus/effectus-go/path"
)

// Path represents a structured path with namespace and elements
type Path struct {
	// Namespace is the top-level namespace
	Namespace string

	// Elements are the path elements
	Elements []PathElement

	// Type is the type information for this path (may be nil)
	Type *common.Type
}

// PathElement represents a single path segment
type PathElement struct {
	// Name is the segment name
	Name string

	// Index is the array index (-1 if not an array access)
	index int

	// StringKey is the map key (empty if not a map access)
	stringKey string

	// Type is the type information for this element (may be nil)
	Type *common.Type
}

// NewElement creates a new path element with a name
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

// WithType creates a copy of the element with type information
func (e PathElement) WithType(typ *common.Type) PathElement {
	e.Type = typ
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

// Clone creates a deep copy of the element
func (e PathElement) Clone() PathElement {
	return PathElement{
		Name:      e.Name,
		index:     e.index,
		stringKey: e.stringKey,
		Type:      e.Type,
	}
}

// String returns a string representation of the path
func (p Path) String() string {
	return RenderPath(p.Namespace, p.Elements)
}

// RenderPath renders a path as a string
func RenderPath(namespace string, elements []PathElement) string {
	var sb strings.Builder
	sb.WriteString(namespace)

	for _, elem := range elements {
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

// NewPath creates a new path
func NewPath(namespace string, elements []PathElement) Path {
	return Path{
		Namespace: namespace,
		Elements:  elements,
	}
}

// WithType creates a copy of the path with type information
func (p Path) WithType(typ *common.Type) Path {
	p.Type = typ
	return p
}

// Clone creates a deep copy of the path
func (p Path) Clone() Path {
	elements := make([]PathElement, len(p.Elements))
	for i, e := range p.Elements {
		elements[i] = e.Clone()
	}

	return Path{
		Namespace: p.Namespace,
		Elements:  elements,
		Type:      p.Type,
	}
}

// IsSubpathOf returns true if this path is a subpath of the given path
func (p Path) IsSubpathOf(parent Path) bool {
	if p.Namespace != parent.Namespace {
		return false
	}

	if len(p.Elements) <= len(parent.Elements) {
		return false
	}

	for i, elem := range parent.Elements {
		if p.Elements[i].Name != elem.Name {
			return false
		}
		if elem.HasIndex() && (!p.Elements[i].HasIndex() || p.Elements[i].index != elem.index) {
			return false
		}
		if elem.HasStringKey() && (!p.Elements[i].HasStringKey() || p.Elements[i].stringKey != elem.stringKey) {
			return false
		}
	}

	return true
}

// GetParentPath returns the parent path (removes the last element)
func (p Path) GetParentPath() (Path, bool) {
	if len(p.Elements) == 0 {
		return Path{}, false
	}

	return Path{
		Namespace: p.Namespace,
		Elements:  p.Elements[:len(p.Elements)-1],
	}, true
}

// GetLastElement returns the last element of the path
func (p Path) GetLastElement() (PathElement, bool) {
	if len(p.Elements) == 0 {
		return PathElement{}, false
	}

	return p.Elements[len(p.Elements)-1], true
}

// Empty returns true if the path is empty
func (p Path) Empty() bool {
	return p.Namespace == "" && len(p.Elements) == 0
}

// Equal returns true if the paths are equal
func (p Path) Equal(other Path) bool {
	if p.Namespace != other.Namespace {
		return false
	}

	if len(p.Elements) != len(other.Elements) {
		return false
	}

	for i, elem := range p.Elements {
		if elem.Name != other.Elements[i].Name {
			return false
		}
		if elem.HasIndex() != other.Elements[i].HasIndex() ||
			(elem.HasIndex() && elem.index != other.Elements[i].index) {
			return false
		}
		if elem.HasStringKey() != other.Elements[i].HasStringKey() ||
			(elem.HasStringKey() && elem.stringKey != other.Elements[i].stringKey) {
			return false
		}
	}

	return true
}

// ToBasePath converts to the base path type
func (p Path) ToBasePath() basepath.Path {
	elements := make([]basepath.PathElement, len(p.Elements))
	for i, elem := range p.Elements {
		baseElem := basepath.NewElement(elem.Name)

		if elem.HasIndex() {
			idx, _ := elem.GetIndex()
			baseElem = baseElem.WithIndex(idx)
		}

		if elem.HasStringKey() {
			key, _ := elem.GetStringKey()
			baseElem = baseElem.WithStringKey(key)
		}

		// Copy type information if available
		if elem.Type != nil {
			baseElem = baseElem.WithType(elem.Type)
		}

		elements[i] = baseElem
	}

	basePath := basepath.NewPath(p.Namespace, elements)
	if p.Type != nil {
		basePath = basePath.WithType(p.Type)
	}

	return basePath
}

// FromBasePath creates a Path from a basepath.Path
func FromBasePath(path basepath.Path) Path {
	elements := make([]PathElement, len(path.Elements))
	for i, elem := range path.Elements {
		element := NewElement(elem.Name)

		if elem.HasIndex() {
			idx, _ := elem.GetIndex()
			element = element.WithIndex(idx)
		}

		if elem.HasStringKey() {
			key, _ := elem.GetStringKey()
			element = element.WithStringKey(key)
		}

		// Copy type information if available
		if elem.Type != nil {
			// Safely convert the interface{} type to *common.Type if possible
			if typePtr, ok := elem.Type.(*common.Type); ok {
				element = element.WithType(typePtr)
			}
		}

		elements[i] = element
	}

	result := NewPath(path.Namespace, elements)
	if path.Type != nil {
		// Safely convert the interface{} type to *common.Type if possible
		if typePtr, ok := path.Type.(*common.Type); ok {
			result = result.WithType(typePtr)
		}
	}

	return result
}

// ParseString parses a string into a Path using the base path parser
func ParseString(pathStr string) (Path, error) {
	path, err := basepath.ParseString(pathStr)
	if err != nil {
		return Path{}, err
	}

	return FromBasePath(path), nil
}

// PathCache provides caching for parsed paths
type PathCache struct {
	cache map[string]Path
}

// NewPathCache creates a new path cache
func NewPathCache() *PathCache {
	return &PathCache{
		cache: make(map[string]Path),
	}
}

// Get retrieves a parsed path from the cache, parsing it if not found
func (pc *PathCache) Get(pathStr string) (Path, error) {
	if path, found := pc.cache[pathStr]; found {
		return path, nil
	}

	path, err := ParseString(pathStr)
	if err != nil {
		return Path{}, err
	}

	pc.cache[pathStr] = path
	return path, nil
}

// PathResolutionResult provides resolution info for paths
type PathResolutionResult struct {
	// The resolved path
	Path Path

	// Embed the common resolution result
	*common.ResolutionResult
}

// NewPathResolutionResult creates a path resolution result from a common result
func NewPathResolutionResult(result *common.ResolutionResult, path Path) *PathResolutionResult {
	return &PathResolutionResult{
		Path:             path,
		ResolutionResult: result,
	}
}

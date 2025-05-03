// Package common contains shared type definitions used across Effectus packages
package common

import (
	"fmt"
	"strconv"
	"strings"
)

// ResolutionResult contains the result of path resolution
type ResolutionResult struct {
	// Path is the path that was resolved
	Path Path

	// Value is the resolved value
	Value interface{}

	// Exists indicates if the path exists in the data
	Exists bool

	// Error is any error encountered during resolution
	Error error

	// Type is the type information for the resolved value
	Type *Type
}

// SchemaInfo defines the interface for schema information providers
type SchemaInfo interface {
	// ValidatePath validates a path
	ValidatePath(string) bool

	// GetPathType gets the type for a path
	GetPathType(string) *Type

	// RegisterPathType registers a type for a path
	RegisterPathType(string, *Type)
}

// Facts defines the interface for immutable fact providers
type Facts interface {
	// Get gets a value by path
	Get(path string) (interface{}, bool)

	// GetWithContext gets a value with resolution information
	GetWithContext(path string) (interface{}, *ResolutionResult)

	// Schema gets the schema information
	Schema() SchemaInfo

	// HasPath checks if a path exists
	HasPath(path string) bool
}

// PathElement represents a single segment in a path with optional indexing
type PathElement struct {
	// Name is the segment name
	Name string

	// Index is the array index (-1 if not an array access)
	index int

	// StringKey is the map key (empty if not a map access)
	stringKey string

	// Type is the type information for this element (may be nil)
	Type *Type
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

// WithType creates a copy of the element with type information
func (e PathElement) WithType(typ *Type) PathElement {
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

// Path represents a complete path including namespace and elements
type Path struct {
	// Namespace is the top-level namespace
	Namespace string

	// Elements are the path elements
	Elements []PathElement

	// Type is the type information for this path (may be nil)
	Type *Type
}

// NewPath creates a new Path with the given namespace and elements
func NewPath(namespace string, elements []PathElement) Path {
	return Path{
		Namespace: namespace,
		Elements:  elements,
	}
}

// WithType creates a copy of the path with type information
func (p Path) WithType(typ *Type) Path {
	p.Type = typ
	return p
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
		Type:      p.Type, // Preserve the parent path type
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
			Type:      elem.Type,
		}
	}

	return Path{
		Namespace: p.Namespace,
		Elements:  elements,
		Type:      p.Type,
	}
}

// PrimitiveType represents a basic type in the type system
type PrimitiveType int

const (
	// TypeUnknown represents an unknown type
	TypeUnknown PrimitiveType = iota

	// TypeBool represents a boolean type
	TypeBool

	// TypeInt represents an integer type
	TypeInt

	// TypeFloat represents a floating-point number
	TypeFloat

	// TypeString represents a string
	TypeString

	// TypeList represents a list/array type
	TypeList

	// TypeMap represents a map type
	TypeMap

	// TypeTime represents a timestamp
	TypeTime

	// TypeDate represents a date
	TypeDate

	// TypeDuration represents a time duration
	TypeDuration

	// TypeObject represents an object type
	TypeObject
)

// Type represents a type in the Effectus type system
type Type struct {
	// PrimType is the primitive type
	PrimType PrimitiveType

	// Name is a custom type name (for named types)
	Name string

	// ListType is the element type for lists
	ListType *Type

	// MapKeyType is the key type for maps
	MapKeyType *Type

	// MapValType is the value type for maps
	MapValType *Type

	// ReferenceType is for types defined elsewhere
	ReferenceType string
}

// IsNumeric returns true if the type is a numeric type
func (t *Type) IsNumeric() bool {
	return t.PrimType == TypeInt || t.PrimType == TypeFloat
}

// IsPrimitive returns true if the type is a primitive type
func (t *Type) IsPrimitive() bool {
	return t.PrimType != TypeUnknown && t.PrimType != TypeList && t.PrimType != TypeMap
}

// IsContainer returns true if the type is a container type (list or map)
func (t *Type) IsContainer() bool {
	return t.PrimType == TypeList || t.PrimType == TypeMap
}

// IsNamed returns true if the type has a name
func (t *Type) IsNamed() bool {
	return t.Name != ""
}

// IsReference returns true if the type references another type
func (t *Type) IsReference() bool {
	return t.ReferenceType != ""
}

// Clone creates a deep copy of this type
func (t *Type) Clone() *Type {
	if t == nil {
		return nil
	}

	clone := &Type{
		PrimType:      t.PrimType,
		Name:          t.Name,
		ReferenceType: t.ReferenceType,
	}

	if t.ListType != nil {
		clone.ListType = t.ListType.Clone()
	}

	if t.MapKeyType != nil {
		clone.MapKeyType = t.MapKeyType.Clone()
	}

	if t.MapValType != nil {
		clone.MapValType = t.MapValType.Clone()
	}

	return clone
}

// Equals checks if two types are equivalent
func (t *Type) Equals(other *Type) bool {
	if t == nil && other == nil {
		return true
	}

	if t == nil || other == nil {
		return false
	}

	if t.PrimType != other.PrimType {
		return false
	}

	// For named types, compare names
	if t.IsNamed() && other.IsNamed() {
		return t.Name == other.Name
	}

	// For reference types, compare reference names
	if t.IsReference() && other.IsReference() {
		return t.ReferenceType == other.ReferenceType
	}

	// For list types, compare element types
	if t.PrimType == TypeList {
		return t.ListType.Equals(other.ListType)
	}

	// For map types, compare key and value types
	if t.PrimType == TypeMap {
		return t.MapKeyType.Equals(other.MapKeyType) && t.MapValType.Equals(other.MapValType)
	}

	// For primitive types, equality is determined by the PrimType value
	return true
}

// String returns the string representation of the type
func (t *Type) String() string {
	if t == nil {
		return "unknown"
	}

	switch t.PrimType {
	case TypeUnknown:
		return "unknown"
	case TypeBool:
		return "bool"
	case TypeInt:
		return "int"
	case TypeFloat:
		return "float"
	case TypeString:
		return "string"
	case TypeList:
		if t.ListType != nil {
			elemType := t.ListType.String()
			return "[]" + elemType
		}
		return "[]unknown"
	case TypeMap:
		keyType := "string"
		valType := "unknown"
		if t.MapKeyType != nil {
			keyType = t.MapKeyType.String()
		}
		if t.MapValType != nil {
			valType = t.MapValType.String()
		}
		return "map[" + keyType + "]" + valType
	case TypeTime:
		return "time"
	case TypeDate:
		return "date"
	case TypeDuration:
		return "duration"
	case TypeObject:
		if t.Name != "" {
			return t.Name
		}
		return "object"
	default:
		if t.Name != "" {
			return t.Name
		}
		return "unknown"
	}
}

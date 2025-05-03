// Package types provides type representation and validation for Effectus
package types

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

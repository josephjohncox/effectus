// Package types provides the unified type system for Effectus
package types

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

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
	// Name is a custom type name (for named types)
	Name string

	// PrimType is the primitive type
	PrimType PrimitiveType

	// ElementType is the element type for lists (replaces ListType)
	ElementType *Type

	// Properties is a map of property types (for objects and maps)
	// For maps, "__key" and "__value" represent key and value types
	Properties map[string]*Type

	// ReferenceType is for types defined elsewhere
	ReferenceType string
}

// NewBoolType creates a new boolean type
func NewBoolType() *Type {
	return &Type{
		PrimType: TypeBool,
	}
}

// NewIntType creates a new integer type
func NewIntType() *Type {
	return &Type{
		PrimType: TypeInt,
	}
}

// NewFloatType creates a new float type
func NewFloatType() *Type {
	return &Type{
		PrimType: TypeFloat,
	}
}

// NewStringType creates a new string type
func NewStringType() *Type {
	return &Type{
		PrimType: TypeString,
	}
}

// NewTimeType creates a new time type
func NewTimeType() *Type {
	return &Type{
		PrimType: TypeTime,
	}
}

// NewDateType creates a new date type
func NewDateType() *Type {
	return &Type{
		PrimType: TypeDate,
	}
}

// NewDurationType creates a new duration type
func NewDurationType() *Type {
	return &Type{
		PrimType: TypeDuration,
	}
}

// NewListType creates a new list type with the given element type
func NewListType(elemType *Type) *Type {
	return &Type{
		PrimType:    TypeList,
		ElementType: elemType,
	}
}

// NewMapType creates a new map type
func NewMapType(keyType, valueType *Type) *Type {
	obj := &Type{
		PrimType:   TypeMap,
		Properties: make(map[string]*Type),
	}
	// Store key and value types in special properties
	obj.Properties["__key"] = keyType
	obj.Properties["__value"] = valueType
	return obj
}

// NewObjectType creates a new object type
func NewObjectType() *Type {
	return &Type{
		PrimType:   TypeObject,
		Properties: make(map[string]*Type),
	}
}

// NewAnyType creates a type that can hold any value
func NewAnyType() *Type {
	return &Type{
		PrimType: TypeUnknown,
	}
}

// MapKeyType returns the key type of a map
func (t *Type) MapKeyType() *Type {
	if t.PrimType != TypeMap || t.Properties == nil {
		return nil
	}
	return t.Properties["__key"]
}

// MapValueType returns the value type of a map
func (t *Type) MapValueType() *Type {
	if t.PrimType != TypeMap || t.Properties == nil {
		return nil
	}
	return t.Properties["__value"]
}

// ListType returns the element type for list types.
func (t *Type) ListType() *Type {
	return t.ElementType
}

// AddProperty adds a property to an object type
func (t *Type) AddProperty(name string, propType *Type) error {
	if t.PrimType != TypeObject {
		return fmt.Errorf("cannot add property to non-object type")
	}

	if t.Properties == nil {
		t.Properties = make(map[string]*Type)
	}

	t.Properties[name] = propType
	return nil
}

// IsNumeric returns true if the type is a numeric type
func (t *Type) IsNumeric() bool {
	return t.PrimType == TypeInt || t.PrimType == TypeFloat
}

// IsPrimitive returns true if the type is a primitive type
func (t *Type) IsPrimitive() bool {
	return t.PrimType != TypeUnknown && t.PrimType != TypeList && t.PrimType != TypeMap && t.PrimType != TypeObject
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

	if t.ElementType != nil {
		clone.ElementType = t.ElementType.Clone()
	}

	if t.Properties != nil {
		clone.Properties = make(map[string]*Type, len(t.Properties))
		for k, v := range t.Properties {
			clone.Properties[k] = v.Clone()
		}
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
		return t.ElementType.Equals(other.ElementType)
	}

	// For map types, compare key and value types
	if t.PrimType == TypeMap {
		keyType := t.MapKeyType()
		otherKeyType := other.MapKeyType()
		valueType := t.MapValueType()
		otherValueType := other.MapValueType()

		return keyType.Equals(otherKeyType) && valueType.Equals(otherValueType)
	}

	// For object types, compare properties
	if t.PrimType == TypeObject {
		if len(t.Properties) != len(other.Properties) {
			return false
		}

		for name, prop := range t.Properties {
			otherProp, exists := other.Properties[name]
			if !exists || !prop.Equals(otherProp) {
				return false
			}
		}
		return true
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
		if t.ElementType != nil {
			elemType := t.ElementType.String()
			return "[]" + elemType
		}
		return "[]unknown"
	case TypeMap:
		keyType := "string"
		valType := "unknown"
		if keyT := t.MapKeyType(); keyT != nil {
			keyType = keyT.String()
		}
		if valT := t.MapValueType(); valT != nil {
			valType = valT.String()
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

// InferTypeFromValue infers a Type from a Go value
func InferTypeFromValue(value interface{}) *Type {
	if value == nil {
		return &Type{PrimType: TypeUnknown}
	}

	switch v := value.(type) {
	case bool:
		return NewBoolType()

	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return NewIntType()

	case float32, float64:
		return NewFloatType()

	case string:
		return NewStringType()

	case []interface{}:
		// For arrays/lists, infer element type from first element if available
		var elemType *Type
		if len(v) > 0 {
			elemType = InferTypeFromValue(v[0])
		} else {
			elemType = &Type{PrimType: TypeUnknown}
		}
		return NewListType(elemType)

	case map[string]interface{}:
		// Object type with properties
		objType := NewObjectType()

		// Infer property types
		for key, val := range v {
			propType := InferTypeFromValue(val)
			objType.AddProperty(key, propType)
		}

		return objType

	default:
		return &Type{PrimType: TypeUnknown}
	}
}

// AreTypesCompatible checks if two types are compatible (actual can be assigned to expected)
func AreTypesCompatible(actual, expected *Type) bool {
	// Same types are always compatible
	if actual.Equals(expected) {
		return true
	}

	// Numeric types are compatible with each other
	if actual.IsNumeric() && expected.IsNumeric() {
		return true
	}

	// For lists, check element compatibility
	if actual.PrimType == TypeList && expected.PrimType == TypeList {
		if actual.ElementType == nil || expected.ElementType == nil {
			return true // One side has unknown element type
		}
		return AreTypesCompatible(actual.ElementType, expected.ElementType)
	}

	// For maps, check key and value compatibility
	if actual.PrimType == TypeMap && expected.PrimType == TypeMap {
		actualKeyType := actual.MapKeyType()
		expectedKeyType := expected.MapKeyType()
		actualValueType := actual.MapValueType()
		expectedValueType := expected.MapValueType()

		// Key types must match
		if actualKeyType == nil || expectedKeyType == nil ||
			!AreTypesCompatible(actualKeyType, expectedKeyType) {
			return false
		}

		// Value types must be compatible
		if actualValueType == nil || expectedValueType == nil {
			return true // One side has unknown value type
		}
		return AreTypesCompatible(actualValueType, expectedValueType)
	}

	// Object types are compatible if expected properties are present in actual
	if actual.PrimType == TypeObject && expected.PrimType == TypeObject {
		for propName, expectedProp := range expected.Properties {
			actualProp, exists := actual.Properties[propName]
			if !exists || !AreTypesCompatible(actualProp, expectedProp) {
				return false
			}
		}
		return true
	}

	// Unknown types can be assigned to any type
	if actual.PrimType == TypeUnknown || expected.PrimType == TypeUnknown {
		return true
	}

	return false
}

// InferTypeFromInterface infers a Type from any interface value
func InferTypeFromInterface(value interface{}) *Type {
	if value == nil {
		return &Type{PrimType: TypeUnknown}
	}

	switch v := value.(type) {
	case bool:
		return NewBoolType()
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return NewIntType()
	case float32, float64:
		return NewFloatType()
	case string:
		return NewStringType()
	case []interface{}:
		// For arrays/lists, infer element type from first element if available
		var elemType *Type
		if len(v) > 0 {
			elemType = InferTypeFromInterface(v[0])
		} else {
			elemType = &Type{PrimType: TypeUnknown}
		}
		return NewListType(elemType)
	case map[string]interface{}:
		// Object type with properties
		objType := NewObjectType()
		for key, val := range v {
			propType := InferTypeFromInterface(val)
			objType.AddProperty(key, propType)
		}
		return objType
	case map[interface{}]interface{}:
		// More complex map type
		if len(v) == 0 {
			return NewMapType(NewStringType(), &Type{PrimType: TypeUnknown})
		}

		// Try to infer key and value types from first entry
		var keyType, valueType *Type
		for k, val := range v {
			keyType = InferTypeFromInterface(k)
			valueType = InferTypeFromInterface(val)
			break
		}

		return NewMapType(keyType, valueType)
	case time.Time:
		return NewTimeType()
	default:
		// Try reflection for structs
		val := reflect.ValueOf(value)
		if val.Kind() == reflect.Struct {
			objType := NewObjectType()
			typ := val.Type()

			for i := 0; i < val.NumField(); i++ {
				field := typ.Field(i)
				if field.PkgPath == "" { // Exported field
					fieldValue := val.Field(i).Interface()
					fieldType := InferTypeFromInterface(fieldValue)
					objType.AddProperty(field.Name, fieldType)
				}
			}
			return objType
		}

		return &Type{PrimType: TypeUnknown}
	}
}

// Create type inference wrapper for JSON data
func InferTypeFromJSON(jsonData []byte) (*Type, error) {
	var value interface{}
	if err := json.Unmarshal(jsonData, &value); err != nil {
		return nil, fmt.Errorf("failed to parse JSON for type inference: %w", err)
	}

	return InferTypeFromInterface(value), nil
}

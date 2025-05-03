package types

import (
	"fmt"
)

// TypeChecker provides type checking functionality for expressions and rules
type TypeChecker struct {
	typeSystem *TypeSystem
}

// NewTypeChecker creates a new type checker with the given type system
func NewTypeChecker(typeSystem *TypeSystem) *TypeChecker {
	if typeSystem == nil {
		typeSystem = NewTypeSystem()
	}

	return &TypeChecker{
		typeSystem: typeSystem,
	}
}

// GetTypeSystem returns the type system used by this checker
func (tc *TypeChecker) GetTypeSystem() *TypeSystem {
	return tc.typeSystem
}

// CheckType validates a type against the type system
func (tc *TypeChecker) CheckType(typ *Type) error {
	return tc.typeSystem.ValidateType(typ)
}

// TypesAreCompatible checks if two types are compatible
func (tc *TypeChecker) TypesAreCompatible(t1, t2 *Type) bool {
	// Handle nil types
	if t1 == nil || t2 == nil {
		return false
	}

	// Same primitive types are compatible
	if t1.PrimType == t2.PrimType {
		// For primitive types, this is enough
		if t1.IsPrimitive() {
			return true
		}

		// For lists, check element types
		if t1.PrimType == TypeList {
			return tc.TypesAreCompatible(t1.ListType, t2.ListType)
		}

		// For maps, check key and value types
		if t1.PrimType == TypeMap {
			return tc.TypesAreCompatible(t1.MapKeyType, t2.MapKeyType) &&
				tc.TypesAreCompatible(t1.MapValType, t2.MapValType)
		}
	}

	// Named types must be equal
	if t1.IsNamed() && t2.IsNamed() {
		return t1.Name == t2.Name
	}

	// Reference types must be equal
	if t1.IsReference() && t2.IsReference() {
		return t1.ReferenceType == t2.ReferenceType
	}

	// Special case: unknown types are compatible with anything
	// This allows for more flexible schema evolution
	if t1.PrimType == TypeUnknown || t2.PrimType == TypeUnknown {
		return true
	}

	// Special case: numeric types are compatible with each other
	if t1.IsNumeric() && t2.IsNumeric() {
		return true
	}

	return false
}

// CheckPathType checks if a path has the expected type
func (tc *TypeChecker) CheckPathType(path string, expectedType *Type) (bool, error) {
	actualType, err := tc.typeSystem.GetFactType(path)
	if err != nil {
		return false, fmt.Errorf("no type information for path: %s", path)
	}

	if !tc.TypesAreCompatible(actualType, expectedType) {
		return false, fmt.Errorf("type mismatch for path %s: expected %v, got %v",
			path, formatType(expectedType), formatType(actualType))
	}

	return true, nil
}

// formatType returns a string representation of a type for error messages
func formatType(t *Type) string {
	if t == nil {
		return "nil"
	}

	switch t.PrimType {
	case TypeUnknown:
		if t.Name != "" {
			return t.Name
		}
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
			return fmt.Sprintf("list<%s>", formatType(t.ListType))
		}
		return "list<unknown>"

	case TypeMap:
		keyType := "unknown"
		if t.MapKeyType != nil {
			keyType = formatType(t.MapKeyType)
		}

		valType := "unknown"
		if t.MapValType != nil {
			valType = formatType(t.MapValType)
		}

		return fmt.Sprintf("map<%s,%s>", keyType, valType)

	case TypeTime:
		return "time"

	case TypeDate:
		return "date"

	case TypeDuration:
		return "duration"

	default:
		return "invalid"
	}
}

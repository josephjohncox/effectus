package types

import (
	"fmt"
	"strings"
)

// ParseTypeName converts a string type name into a Type.
func ParseTypeName(name string) (*Type, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return NewAnyType(), nil
	}

	lower := strings.ToLower(trimmed)
	switch lower {
	case "unknown", "any":
		return NewAnyType(), nil
	case "string":
		return NewStringType(), nil
	case "bool", "boolean":
		return NewBoolType(), nil
	case "int", "integer":
		return NewIntType(), nil
	case "float", "number", "double":
		return NewFloatType(), nil
	case "time", "timestamp":
		return NewTimeType(), nil
	case "date":
		return NewDateType(), nil
	case "duration":
		return NewDurationType(), nil
	case "array", "list", "[]":
		return NewListType(NewAnyType()), nil
	case "map":
		return NewMapType(NewStringType(), NewAnyType()), nil
	case "object":
		return NewObjectType(), nil
	}

	if strings.HasPrefix(lower, "[]") {
		elemName := strings.TrimPrefix(trimmed, "[]")
		elemType, _ := ParseTypeName(elemName)
		if elemType == nil {
			elemType = NewAnyType()
		}
		return NewListType(elemType), nil
	}

	if strings.HasPrefix(lower, "list<") && strings.HasSuffix(lower, ">") {
		inner := strings.TrimSuffix(strings.TrimPrefix(trimmed, "list<"), ">")
		elemType, _ := ParseTypeName(inner)
		if elemType == nil {
			elemType = NewAnyType()
		}
		return NewListType(elemType), nil
	}

	if strings.HasPrefix(lower, "array<") && strings.HasSuffix(lower, ">") {
		inner := strings.TrimSuffix(strings.TrimPrefix(trimmed, "array<"), ">")
		elemType, _ := ParseTypeName(inner)
		if elemType == nil {
			elemType = NewAnyType()
		}
		return NewListType(elemType), nil
	}

	if strings.HasPrefix(lower, "map<") && strings.HasSuffix(lower, ">") {
		inner := strings.TrimSuffix(strings.TrimPrefix(trimmed, "map<"), ">")
		parts := strings.Split(inner, ",")
		if len(parts) == 2 {
			keyType, _ := ParseTypeName(strings.TrimSpace(parts[0]))
			valueType, _ := ParseTypeName(strings.TrimSpace(parts[1]))
			if keyType == nil {
				keyType = NewStringType()
			}
			if valueType == nil {
				valueType = NewAnyType()
			}
			return NewMapType(keyType, valueType), nil
		}
		return NewMapType(NewStringType(), NewAnyType()), nil
	}

	if strings.HasPrefix(lower, "map[") && strings.HasSuffix(lower, "]") {
		inner := strings.TrimSuffix(strings.TrimPrefix(trimmed, "map["), "]")
		parts := strings.Split(inner, ",")
		if len(parts) == 2 {
			keyType, _ := ParseTypeName(strings.TrimSpace(parts[0]))
			valueType, _ := ParseTypeName(strings.TrimSpace(parts[1]))
			if keyType == nil {
				keyType = NewStringType()
			}
			if valueType == nil {
				valueType = NewAnyType()
			}
			return NewMapType(keyType, valueType), nil
		}
		return NewMapType(NewStringType(), NewAnyType()), nil
	}

	return NewAnyType(), fmt.Errorf("unknown type name: %s", name)
}

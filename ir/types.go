package ir

import (
	"fmt"
	"strings"

	effectusv1 "github.com/effectus/effectus-go/gen/effectus/v1"
)

type typeKind uint8

const (
	typeInvalid typeKind = iota
	typeBool
	typeInt
	typeFloat
	typeString
	typeBytes
	typeNull
	typeVoid
	typeList
	typeMap
	typeObject
	typeNamed
)

type typeRef struct {
	kind    typeKind
	name    string
	element *typeRef
	fields  map[string]*typeRef
}

type typeChecker struct {
	environment Environment
}

func (c typeChecker) parse(name string, allowVoid bool) (*typeRef, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("type name is empty")
	}
	lower := strings.ToLower(name)
	switch lower {
	case "bool", "boolean":
		return &typeRef{kind: typeBool}, nil
	case "int", "integer":
		return &typeRef{kind: typeInt}, nil
	case "float", "double", "number":
		return &typeRef{kind: typeFloat}, nil
	case "string":
		return &typeRef{kind: typeString}, nil
	case "bytes":
		return &typeRef{kind: typeBytes}, nil
	case "null":
		return &typeRef{kind: typeNull}, nil
	case "void":
		if allowVoid {
			return &typeRef{kind: typeVoid}, nil
		}
		return nil, fmt.Errorf("void is not valid here")
	case "any", "unknown", "object", "list", "map":
		return nil, fmt.Errorf("open type %q is forbidden in checked IR", name)
	}
	if strings.HasPrefix(name, "[]") {
		element, err := c.parse(strings.TrimSpace(name[2:]), false)
		if err != nil {
			return nil, fmt.Errorf("list element: %w", err)
		}
		return &typeRef{kind: typeList, element: element}, nil
	}
	if inner, ok := genericInner(name, "list"); ok {
		element, err := c.parse(inner, false)
		if err != nil {
			return nil, fmt.Errorf("list element: %w", err)
		}
		return &typeRef{kind: typeList, element: element}, nil
	}
	if inner, ok := genericInner(name, "map"); ok {
		element, err := c.parse(inner, false)
		if err != nil {
			return nil, fmt.Errorf("map element: %w", err)
		}
		return &typeRef{kind: typeMap, element: element}, nil
	}
	if _, ok := c.environment.Types[name]; ok {
		return &typeRef{kind: typeNamed, name: name}, nil
	}
	return nil, fmt.Errorf("unknown type %q", name)
}

func genericInner(name, constructor string) (string, bool) {
	trimmed := strings.TrimSpace(name)
	prefix := constructor + "<"
	if len(trimmed) <= len(prefix) || !strings.EqualFold(trimmed[:len(prefix)], prefix) || !strings.HasSuffix(trimmed, ">") {
		return "", false
	}
	inner := strings.TrimSpace(trimmed[len(prefix) : len(trimmed)-1])
	if inner == "" {
		return "", false
	}
	return inner, true
}

func (c typeChecker) resolve(ref *typeRef, seen map[string]struct{}) (*typeRef, error) {
	if ref == nil || ref.kind != typeNamed {
		return ref, nil
	}
	if _, cycle := seen[ref.name]; cycle {
		return nil, fmt.Errorf("cyclic type definition involving %q", ref.name)
	}
	definition, ok := c.environment.Types[ref.name]
	if !ok {
		return nil, fmt.Errorf("unknown type %q", ref.name)
	}
	seen[ref.name] = struct{}{}
	defer delete(seen, ref.name)
	switch definition.Kind {
	case TypeKindList:
		element, err := c.parse(definition.ElementType, false)
		if err != nil {
			return nil, err
		}
		return &typeRef{kind: typeList, name: ref.name, element: element}, nil
	case TypeKindMap:
		element, err := c.parse(definition.ElementType, false)
		if err != nil {
			return nil, err
		}
		return &typeRef{kind: typeMap, name: ref.name, element: element}, nil
	case TypeKindObject:
		fields := make(map[string]*typeRef, len(definition.Fields))
		for name, typeName := range definition.Fields {
			fieldType, err := c.parse(typeName, false)
			if err != nil {
				return nil, err
			}
			fields[name] = fieldType
		}
		return &typeRef{kind: typeObject, name: ref.name, fields: fields}, nil
	default:
		return nil, fmt.Errorf("unsupported type kind %q", definition.Kind)
	}
}

func (c typeChecker) assignable(actual, expected *typeRef) bool {
	return c.assignableDepth(actual, expected, 0)
}

func (c typeChecker) assignableDepth(actual, expected *typeRef, depth int) bool {
	if actual == nil || expected == nil || depth > 64 {
		return false
	}
	if actual.kind == typeNamed && expected.kind == typeNamed && actual.name == expected.name {
		return true
	}
	var err error
	if actual.kind == typeNamed {
		actual, err = c.resolve(actual, make(map[string]struct{}))
		if err != nil {
			return false
		}
	}
	if expected.kind == typeNamed {
		expected, err = c.resolve(expected, make(map[string]struct{}))
		if err != nil {
			return false
		}
	}
	if actual.kind == typeInt && expected.kind == typeFloat {
		return true
	}
	if actual.kind != expected.kind {
		return false
	}
	switch actual.kind {
	case typeList, typeMap:
		return c.assignableDepth(actual.element, expected.element, depth+1)
	case typeObject:
		for name, expectedField := range expected.fields {
			actualField, ok := actual.fields[name]
			if !ok || !c.assignableDepth(actualField, expectedField, depth+1) {
				return false
			}
		}
		return true
	default:
		return actual.kind != typeInvalid && actual.kind != typeVoid
	}
}

func (c typeChecker) literalType(literal *effectusv1.Literal, depth int) (*typeRef, error) {
	if literal == nil || depth > 64 {
		return nil, fmt.Errorf("invalid literal")
	}
	switch kind := literal.Kind.(type) {
	case *effectusv1.Literal_Null:
		if kind.Null != effectusv1.NullValue_NULL_VALUE_NULL {
			return nil, fmt.Errorf("invalid null literal")
		}
		return &typeRef{kind: typeNull}, nil
	case *effectusv1.Literal_BoolValue:
		return &typeRef{kind: typeBool}, nil
	case *effectusv1.Literal_IntValue:
		return &typeRef{kind: typeInt}, nil
	case *effectusv1.Literal_DoubleValue:
		return &typeRef{kind: typeFloat}, nil
	case *effectusv1.Literal_StringValue:
		return &typeRef{kind: typeString}, nil
	case *effectusv1.Literal_BytesValue:
		return &typeRef{kind: typeBytes}, nil
	case *effectusv1.Literal_ListValue:
		if kind.ListValue == nil {
			return nil, fmt.Errorf("list literal is nil")
		}
		if len(kind.ListValue.Values) == 0 {
			return nil, fmt.Errorf("empty list literal has no closed element type")
		}
		first, err := c.literalType(kind.ListValue.Values[0], depth+1)
		if err != nil {
			return nil, err
		}
		for i := 1; i < len(kind.ListValue.Values); i++ {
			current, err := c.literalType(kind.ListValue.Values[i], depth+1)
			if err != nil {
				return nil, err
			}
			if c.assignable(current, first) {
				continue
			}
			if c.assignable(first, current) {
				first = current
				continue
			}
			return nil, fmt.Errorf("list literal contains incompatible element types")
		}
		return &typeRef{kind: typeList, element: first}, nil
	case *effectusv1.Literal_ObjectValue:
		if kind.ObjectValue == nil {
			return nil, fmt.Errorf("object literal is nil")
		}
		fields := make(map[string]*typeRef, len(kind.ObjectValue.Fields))
		for _, field := range kind.ObjectValue.Fields {
			if field == nil {
				return nil, fmt.Errorf("object field is nil")
			}
			fieldType, err := c.literalType(field.Value, depth+1)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", field.Name, err)
			}
			fields[field.Name] = fieldType
		}
		return &typeRef{kind: typeObject, fields: fields}, nil
	default:
		return nil, fmt.Errorf("literal kind is not set")
	}
}

func (c typeChecker) literalAssignable(literal *effectusv1.Literal, expected *typeRef) error {
	resolved, err := c.resolve(expected, make(map[string]struct{}))
	if err != nil {
		return err
	}

	// Structural collections are checked against their closed expected type so
	// empty lists and objects do not degrade to an open "any" type.
	if resolved.kind == typeObject {
		object := literal.GetObjectValue()
		if object == nil {
			return fmt.Errorf("literal type is incompatible with %s", c.describe(expected))
		}
		definition := c.environment.Types[resolved.name]
		seen := make(map[string]struct{}, len(object.Fields))
		for _, field := range object.Fields {
			seen[field.Name] = struct{}{}
			fieldExpected := resolved.fields[field.Name]
			if fieldExpected == nil {
				return fmt.Errorf("unexpected object field %q", field.Name)
			}
			if err := c.literalAssignable(field.Value, fieldExpected); err != nil {
				return fmt.Errorf("field %q: %w", field.Name, err)
			}
		}
		for _, required := range definition.RequiredFields {
			if _, ok := seen[required]; !ok {
				return fmt.Errorf("missing required object field %q", required)
			}
		}
		return nil
	}
	if resolved.kind == typeList {
		list := literal.GetListValue()
		if list == nil {
			return fmt.Errorf("literal type is incompatible with %s", c.describe(expected))
		}
		for i, value := range list.Values {
			if err := c.literalAssignable(value, resolved.element); err != nil {
				return fmt.Errorf("list item %d: %w", i, err)
			}
		}
		return nil
	}
	if resolved.kind == typeMap {
		object := literal.GetObjectValue()
		if object == nil {
			return fmt.Errorf("literal type is incompatible with %s", c.describe(expected))
		}
		for _, field := range object.Fields {
			if err := c.literalAssignable(field.Value, resolved.element); err != nil {
				return fmt.Errorf("map field %q: %w", field.Name, err)
			}
		}
		return nil
	}

	actual, err := c.literalType(literal, 0)
	if err != nil {
		return err
	}
	if !c.assignable(actual, expected) {
		return fmt.Errorf("literal type is incompatible with %s", c.describe(expected))
	}
	return nil
}

func (c typeChecker) describe(ref *typeRef) string {
	if ref == nil {
		return "<invalid>"
	}
	if ref.name != "" {
		return ref.name
	}
	switch ref.kind {
	case typeBool:
		return "bool"
	case typeInt:
		return "int"
	case typeFloat:
		return "float"
	case typeString:
		return "string"
	case typeBytes:
		return "bytes"
	case typeNull:
		return "null"
	case typeVoid:
		return "void"
	case typeList:
		return "list<" + c.describe(ref.element) + ">"
	case typeMap:
		return "map<" + c.describe(ref.element) + ">"
	case typeObject:
		return "object"
	default:
		return "<invalid>"
	}
}

// schema/reflect_loader.go
package schema

import (
	"fmt"
	"reflect"
	"strings"
)

// RegisterGoStruct registers a Go struct as a schema
func (sr *SchemaRegistry) RegisterGoStruct(structValue interface{}, namespace string) error {
	t := reflect.TypeOf(structValue)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct, got %v", t.Kind())
	}
	
	registerStructFields(sr.typeSystem, t, namespace, "")
	return nil
}

// registerStructFields recursively registers struct fields
func registerStructFields(ts *TypeSystem, t reflect.Type, namespace, prefix string) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		
		// Skip unexported fields
		if !field.IsExported() {
			continue
		}
		
		// Get field name from json tag or field name
		fieldName := field.Tag.Get("json")
		if fieldName == "" || fieldName == "-" {
			fieldName = strings.ToLower(field.Name)
		}
		
		// Remove json options (like omitempty)
		if commaIdx := strings.Index(fieldName, ","); commaIdx > 0 {
			fieldName = fieldName[:commaIdx]
		}
		
		// Build path
		path := namespace
		if prefix != "" {
			path += "." + prefix
		}
		if path != "" {
			path += "."
		}
		path += fieldName
		
		// Convert Go type to Effectus type
		typ := goTypeToEffectusType(field.Type)
		ts.RegisterFactType(path, typ)
		
		// For struct fields, recursively register
		if field.Type.Kind() == reflect.Struct {
			// Skip certain standard library types
			pkgPath := field.Type.PkgPath()
			if pkgPath == "time" && field.Type.Name() == "Time" {
				// Special handling for time.Time
				continue
			}
			
			// Recurse for nested structs
			registerStructFields(ts, field.Type, namespace, prefix+fieldName)
		}
	}
}

// goTypeToEffectusType converts Go types to Effectus types
func goTypeToEffectusType(t reflect.Type) *Type {
	switch t.Kind() {
	case reflect.String:
		return &Type{PrimType: TypeString}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Type{PrimType: TypeInt}
	case reflect.Float32, reflect.Float64:
		return &Type{PrimType: TypeFloat}
	case reflect.Bool:
		return &Type{PrimType: TypeBool}
	case reflect.Slice, reflect.Array:
		elemType := goTypeToEffectusType(t.Elem())
		return &Type{
			PrimType: TypeList,
			ListType: elemType,
		}
	case reflect.Map:
		keyType := goTypeToEffectusType(t.Key())
		valType := goTypeToEffectusType(t.Elem())
		return &Type{
			PrimType:   TypeMap,
			MapKeyType: keyType,
			MapValType: valType,
		}
	case reflect.Struct:
		// Handle special types
		if t.PkgPath() == "time" && t.Name() == "Time" {
			// time.Time is represented as string
			return &Type{PrimType: TypeString}
		}
		return &Type{
			PrimType: TypeUnknown,
			Name:     t.Name(),
		}
	case reflect.Interface:
		return &Type{PrimType: TypeUnknown}
	default:
		return &Type{PrimType: TypeUnknown}
	}
}
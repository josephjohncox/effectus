// Package registry provides schema management for Effectus
package registry

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/effectus/effectus-go/schema/types"
)

// ReflectLoader loads schemas from Go structs using reflection
type ReflectLoader struct {
	// Namespace prefix for fact paths
	namespace string
}

// NewReflectLoader creates a new loader for struct reflection
func NewReflectLoader(namespace string) *ReflectLoader {
	return &ReflectLoader{
		namespace: namespace,
	}
}

// CanLoad always returns false for ReflectLoader (it doesn't load from files)
func (l *ReflectLoader) CanLoad(path string) bool {
	return false
}

// Load is a no-op for ReflectLoader (it doesn't load from files)
func (l *ReflectLoader) Load(path string, ts *types.TypeSystem) error {
	return fmt.Errorf("ReflectLoader doesn't load from files")
}

// LoadStruct loads type definitions from a struct
func (l *ReflectLoader) LoadStruct(v interface{}, ts *types.TypeSystem) error {
	t := reflect.TypeOf(v)

	// Handle pointers
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Make sure it's a struct
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct, got %s", t.Kind())
	}

	return l.loadStructFields(t, "", ts)
}

// loadStructFields processes struct fields recursively
func (l *ReflectLoader) loadStructFields(t reflect.Type, prefix string, ts *types.TypeSystem) error {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if field.PkgPath != "" {
			continue
		}

		// Get field name from JSON tag or field name
		name := field.Name
		if jsonTag := field.Tag.Get("json"); jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" && parts[0] != "-" {
				name = parts[0]
			}
		}

		// Build full path
		var path string
		if prefix == "" {
			path = l.namespace + "." + name
		} else {
			path = prefix + "." + name
		}

		// Process based on field type
		fieldType := field.Type
		kind := fieldType.Kind()

		switch kind {
		case reflect.Bool:
			ts.RegisterFactType(path, types.NewBoolType())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			ts.RegisterFactType(path, types.NewIntType())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			ts.RegisterFactType(path, types.NewIntType())
		case reflect.Float32, reflect.Float64:
			ts.RegisterFactType(path, types.NewFloatType())
		case reflect.String:
			ts.RegisterFactType(path, types.NewStringType())
		case reflect.Array, reflect.Slice:
			// Register array type
			elemType := l.getTypeForKind(fieldType.Elem().Kind())
			if elemType != nil {
				ts.RegisterFactType(path, types.NewListType(elemType))
			}
		case reflect.Map:
			// Register map type if key is string
			if fieldType.Key().Kind() == reflect.String {
				elemType := l.getTypeForKind(fieldType.Elem().Kind())
				keyType := types.NewStringType()
				if elemType != nil {
					ts.RegisterFactType(path, types.NewMapType(keyType, elemType))
				}
			}
		case reflect.Struct:
			// Process nested struct
			if err := l.loadStructFields(fieldType, path, ts); err != nil {
				return err
			}
		case reflect.Ptr:
			// Handle pointer types
			elemKind := fieldType.Elem().Kind()
			if elemKind == reflect.Struct {
				if err := l.loadStructFields(fieldType.Elem(), path, ts); err != nil {
					return err
				}
			} else {
				elemType := l.getTypeForKind(elemKind)
				if elemType != nil {
					ts.RegisterFactType(path, elemType)
				}
			}
		}
	}

	return nil
}

// getTypeForKind maps reflect.Kind to schema Type
func (l *ReflectLoader) getTypeForKind(kind reflect.Kind) *types.Type {
	switch kind {
	case reflect.Bool:
		return types.NewBoolType()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return types.NewIntType()
	case reflect.Float32, reflect.Float64:
		return types.NewFloatType()
	case reflect.String:
		return types.NewStringType()
	default:
		return nil
	}
}

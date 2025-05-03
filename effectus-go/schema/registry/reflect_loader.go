// Package registry provides schema management for Effectus
package registry

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema/types"
)

// ReflectLoader loads schemas from Go structs
type ReflectLoader struct{}

// NewReflectLoader creates a new reflection-based loader
func NewReflectLoader() *ReflectLoader {
	return &ReflectLoader{}
}

// CanLoad checks if this loader can handle the given path
// For struct loaders, this is a no-op since structs are provided directly
func (l *ReflectLoader) CanLoad(path string) bool {
	return false
}

// Load loads type definitions from a file (not applicable for struct loader)
func (l *ReflectLoader) Load(path string, ts *types.TypeSystem) error {
	return fmt.Errorf("reflect loader doesn't support file loading, use RegisterStruct directly")
}

// RegisterStruct registers a Go struct as a schema
func (l *ReflectLoader) RegisterStruct(ts *types.TypeSystem, structValue interface{}, namespace string) error {
	t := reflect.TypeOf(structValue)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct, got %v", t.Kind())
	}

	l.registerStructFields(ts, t, namespace, "")
	return nil
}

// registerStructFields recursively registers struct fields
func (l *ReflectLoader) registerStructFields(ts *types.TypeSystem, t reflect.Type, namespace, prefix string) {
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
		typ := l.goTypeToEffectusType(field.Type)
		parsedPath, err := pathutil.FromString(path)
		if err != nil {
			// Cannot return error from this function, so we'll log it instead
			// and skip registering this field
			fmt.Printf("Error parsing fact path %s: %v", path, err)
			continue
		}
		ts.RegisterFactType(parsedPath, typ)

		// For struct fields, recursively register
		if field.Type.Kind() == reflect.Struct {
			// Skip certain standard library types
			pkgPath := field.Type.PkgPath()
			if pkgPath == "time" && field.Type.Name() == "Time" {
				// Special handling for time.Time
				continue
			}

			// Recurse for nested structs
			newPrefix := prefix
			if newPrefix != "" {
				newPrefix += "."
			}
			newPrefix += fieldName
			l.registerStructFields(ts, field.Type, namespace, newPrefix)
		}
	}
}

// goTypeToEffectusType converts Go types to Effectus types
func (l *ReflectLoader) goTypeToEffectusType(t reflect.Type) *types.Type {
	switch t.Kind() {
	case reflect.String:
		return &types.Type{PrimType: types.TypeString}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &types.Type{PrimType: types.TypeInt}
	case reflect.Float32, reflect.Float64:
		return &types.Type{PrimType: types.TypeFloat}
	case reflect.Bool:
		return &types.Type{PrimType: types.TypeBool}
	case reflect.Slice, reflect.Array:
		elemType := l.goTypeToEffectusType(t.Elem())
		return &types.Type{
			PrimType:    types.TypeList,
			ElementType: elemType,
		}
	case reflect.Map:
		keyType := l.goTypeToEffectusType(t.Key())
		valType := l.goTypeToEffectusType(t.Elem())
		mapType := &types.Type{
			PrimType:   types.TypeMap,
			Properties: make(map[string]*types.Type),
		}
		mapType.Properties["__key"] = keyType
		mapType.Properties["__value"] = valType
		return mapType
	case reflect.Struct:
		// Handle special types
		if t.PkgPath() == "time" && t.Name() == "Time" {
			// time.Time is represented as string
			return &types.Type{PrimType: types.TypeString}
		}
		return &types.Type{
			PrimType: types.TypeUnknown,
			Name:     t.Name(),
		}
	case reflect.Interface:
		return &types.Type{PrimType: types.TypeUnknown}
	default:
		return &types.Type{PrimType: types.TypeUnknown}
	}
}

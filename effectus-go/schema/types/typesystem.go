package types

import (
	"fmt"
	"strings"
	"sync"
)

// VerbInfo contains type information for a verb
type VerbInfo struct {
	ArgTypes   map[string]*Type
	ReturnType *Type
}

// TypeSystem manages types registered in the system
type TypeSystem struct {
	// Types is a map from type name to type
	types map[string]*Type

	// Facts is a map from fact path to type
	facts map[string]*Type

	// VerbTypes is a map from verb name to verb type information
	VerbTypes map[string]*VerbInfo

	// Mutex for thread safety
	mu sync.RWMutex
}

// NewTypeSystem creates a new type system
func NewTypeSystem() *TypeSystem {
	return &TypeSystem{
		types:     make(map[string]*Type),
		facts:     make(map[string]*Type),
		VerbTypes: make(map[string]*VerbInfo),
	}
}

// RegisterType registers a named type in the system
func (ts *TypeSystem) RegisterType(name string, typ *Type) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Clone the type to avoid external modification
	ts.types[name] = typ.Clone()
}

// GetType retrieves a type by name
func (ts *TypeSystem) GetType(name string) (*Type, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	typ, found := ts.types[name]
	if !found {
		return nil, false
	}

	// Return a clone to prevent external modification
	return typ.Clone(), true
}

// RegisterFactType registers a type for a fact path
func (ts *TypeSystem) RegisterFactType(path string, typ *Type) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Clone the type to avoid external modification
	ts.facts[path] = typ.Clone()
	return nil
}

// GetFactType retrieves the type for a fact path
func (ts *TypeSystem) GetFactType(path string) (*Type, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// Try direct match first
	if typ, found := ts.facts[path]; found {
		return typ.Clone(), nil
	}

	// Try parent paths
	parts := strings.Split(path, ".")
	for i := len(parts) - 1; i > 0; i-- {
		parentPath := strings.Join(parts[:i], ".")
		if parentType, found := ts.facts[parentPath]; found {
			// Check if parent is a container type that defines its element type
			if parentType.PrimType == TypeList && parentType.ListType != nil {
				return parentType.ListType.Clone(), nil
			}
			if parentType.PrimType == TypeMap && parentType.MapValType != nil {
				return parentType.MapValType.Clone(), nil
			}
		}
	}

	return nil, fmt.Errorf("type not found for path: %s", path)
}

// InferFactType infers a type from a value and registers it
func (ts *TypeSystem) InferFactType(path string, value interface{}) *Type {
	typ := inferTypeFromValue(value)
	ts.RegisterFactType(path, typ)
	return typ
}

// inferTypeFromValue infers a Type from a Go value
func inferTypeFromValue(value interface{}) *Type {
	if value == nil {
		return &Type{PrimType: TypeUnknown}
	}

	switch v := value.(type) {
	case bool:
		return &Type{PrimType: TypeBool}

	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return &Type{PrimType: TypeInt}

	case float32, float64:
		return &Type{PrimType: TypeFloat}

	case string:
		return &Type{PrimType: TypeString}

	case []interface{}:
		// For arrays/lists, infer element type from first element if available
		elemType := &Type{PrimType: TypeUnknown}
		if len(v) > 0 {
			elemType = inferTypeFromValue(v[0])
		}
		return &Type{
			PrimType: TypeList,
			ListType: elemType,
		}

	case map[string]interface{}:
		// For maps, we can only determine key type (string)
		return &Type{
			PrimType:   TypeMap,
			MapKeyType: &Type{PrimType: TypeString},
			MapValType: &Type{PrimType: TypeUnknown},
		}

	default:
		return &Type{PrimType: TypeUnknown}
	}
}

// ValidateType validates a type against the type system
func (ts *TypeSystem) ValidateType(typ *Type) error {
	if typ == nil {
		return fmt.Errorf("nil type")
	}

	// Named types must be registered
	if typ.IsNamed() {
		if _, found := ts.GetType(typ.Name); !found {
			return fmt.Errorf("unknown named type: %s", typ.Name)
		}
		return nil
	}

	// Reference types must be registered
	if typ.IsReference() {
		if _, found := ts.GetType(typ.ReferenceType); !found {
			return fmt.Errorf("unknown reference type: %s", typ.ReferenceType)
		}
		return nil
	}

	// Validate container element types
	if typ.PrimType == TypeList && typ.ListType != nil {
		return ts.ValidateType(typ.ListType)
	}

	if typ.PrimType == TypeMap {
		if typ.MapKeyType != nil {
			if err := ts.ValidateType(typ.MapKeyType); err != nil {
				return fmt.Errorf("invalid map key type: %w", err)
			}
		}

		if typ.MapValType != nil {
			if err := ts.ValidateType(typ.MapValType); err != nil {
				return fmt.Errorf("invalid map value type: %w", err)
			}
		}
	}

	return nil
}

// GetAllFactPaths returns all registered fact paths
func (ts *TypeSystem) GetAllFactPaths() []string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	paths := make([]string, 0, len(ts.facts))
	for path := range ts.facts {
		paths = append(paths, path)
	}
	return paths
}

// GetAllTypes returns all registered named types
func (ts *TypeSystem) GetAllTypes() map[string]*Type {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make(map[string]*Type, len(ts.types))
	for name, typ := range ts.types {
		result[name] = typ.Clone()
	}
	return result
}

// RegisterVerbType registers input and output types for a verb
func (ts *TypeSystem) RegisterVerbType(verbName string, inputType, outputType *Type) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Store verb type information
	ts.VerbTypes[verbName] = &VerbInfo{
		ArgTypes:   map[string]*Type{"input": inputType.Clone()},
		ReturnType: outputType.Clone(),
	}

	return nil
}

// TypeCheckPredicate evaluates if a predicate path has valid typing
func (ts *TypeSystem) TypeCheckPredicate(path string) error {
	_, err := ts.GetFactType(path)
	return err
}

// TypeCheckEffect evaluates if an effect is type-compatible
func (ts *TypeSystem) TypeCheckEffect(verbName string, payload interface{}) error {
	// Look up verb type information
	verbInfo, exists := ts.VerbTypes[verbName]
	if !exists {
		return fmt.Errorf("unknown verb: %s", verbName)
	}

	// Check that the payload is compatible with the verb's input type
	expectedType := verbInfo.ArgTypes["input"]
	if expectedType == nil {
		return nil // No type constraints
	}

	// Infer the actual type of the payload
	actualType := inferTypeFromValue(payload)

	// Check compatibility
	if !areTypesCompatible(actualType, expectedType) {
		return fmt.Errorf("incompatible payload type for verb %s: expected %s, got %s",
			verbName, expectedType.String(), actualType.String())
	}

	return nil
}

// Helper to check if two types are compatible (actual can be assigned to expected)
func areTypesCompatible(actual, expected *Type) bool {
	// Same types are always compatible
	if actual.Equals(expected) {
		return true
	}

	// Numeric types are compatible with each other
	if actual.IsNumeric() && expected.IsNumeric() {
		return true
	}

	// For containers, check element compatibility
	if actual.PrimType == TypeList && expected.PrimType == TypeList {
		if actual.ListType == nil || expected.ListType == nil {
			return true // One side has unknown element type
		}
		return areTypesCompatible(actual.ListType, expected.ListType)
	}

	if actual.PrimType == TypeMap && expected.PrimType == TypeMap {
		// Key types must match
		if !areTypesCompatible(actual.MapKeyType, expected.MapKeyType) {
			return false
		}

		// Value types must be compatible
		if actual.MapValType == nil || expected.MapValType == nil {
			return true // One side has unknown value type
		}
		return areTypesCompatible(actual.MapValType, expected.MapValType)
	}

	// Unknown types can be assigned to any type
	if actual.PrimType == TypeUnknown || expected.PrimType == TypeUnknown {
		return true
	}

	return false
}

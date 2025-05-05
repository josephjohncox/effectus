package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/effectus/effectus-go/pathutil"
)

// ExprSchema provides a unified approach to loading and working with schema information
// It completely replaces the old SchemaRegistry with a more powerful expr-based implementation
type ExprSchema struct {
	// The facts provider for reading data
	facts *pathutil.TypedExprFacts

	// Root directory for schema files
	rootDir string

	// Namespace for data organization
	namespace string
}

// NewExprSchema creates a new schema manager
func NewExprSchema(namespace string) *ExprSchema {
	// Create empty ExprFacts as a starting point
	emptyData := make(map[string]interface{})
	facts := pathutil.NewExprFacts(emptyData)
	typedFacts := pathutil.NewTypedExprFacts(facts)

	return &ExprSchema{
		facts:     typedFacts,
		namespace: namespace,
	}
}

// LoadFromDirectory loads all schema files from a directory
func (s *ExprSchema) LoadFromDirectory(dir string) error {
	s.rootDir = dir

	// Walk the directory and load all compatible files
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check file extension
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".json":
			return s.loadJSONFile(path)
		case ".yaml", ".yml":
			return s.loadYAMLFile(path)
		default:
			// Skip unsupported file types
			return nil
		}
	})
}

// loadJSONFile loads a JSON file into the schema
func (s *ExprSchema) loadJSONFile(path string) error {
	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading JSON file: %w", err)
	}

	// Create a loader
	loader := pathutil.NewJSONLoader(data)

	// Get the namespace from the file path if necessary
	namespace := s.deriveNamespaceFromPath(path)

	// Create a namespaced loader if we have a namespace
	var factLoader pathutil.FactLoader = loader
	if namespace != "" {
		namespacedLoader := pathutil.NewNamespacedLoader()
		namespacedLoader.AddSource(namespace, data)
		factLoader = namespacedLoader
	}

	// Load into typed facts
	typedFacts, err := factLoader.LoadIntoTypedFacts()
	if err != nil {
		return fmt.Errorf("loading JSON into typed facts: %w", err)
	}

	// Merge with our existing facts
	s.mergeFacts(typedFacts)

	return nil
}

// loadYAMLFile loads a YAML file into the schema
func (s *ExprSchema) loadYAMLFile(path string) error {
	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading YAML file: %w", err)
	}

	// Convert YAML to JSON
	jsonData, err := yamlToJSON(data)
	if err != nil {
		return fmt.Errorf("converting YAML to JSON: %w", err)
	}

	// Create a loader
	loader := pathutil.NewJSONLoader(jsonData)

	// Get the namespace from the file path if necessary
	namespace := s.deriveNamespaceFromPath(path)

	// Create a namespaced loader if we have a namespace
	var factLoader pathutil.FactLoader = loader
	if namespace != "" {
		namespacedLoader := pathutil.NewNamespacedLoader()
		namespacedLoader.AddSource(namespace, jsonData)
		factLoader = namespacedLoader
	}

	// Load into typed facts
	typedFacts, err := factLoader.LoadIntoTypedFacts()
	if err != nil {
		return fmt.Errorf("loading YAML into typed facts: %w", err)
	}

	// Merge with our existing facts
	s.mergeFacts(typedFacts)

	return nil
}

// yamlToJSON converts YAML data to JSON
func yamlToJSON(yamlData []byte) ([]byte, error) {
	// This is a simplified implementation
	// In a real implementation, you would use a YAML library like gopkg.in/yaml.v3
	return nil, fmt.Errorf("YAML conversion not implemented - add gopkg.in/yaml.v3 dependency")
}

// deriveNamespaceFromPath derives a namespace from a file path
func (s *ExprSchema) deriveNamespaceFromPath(path string) string {
	// If we already have a namespace, use it
	if s.namespace != "" {
		return s.namespace
	}

	// Otherwise, derive from the file name
	baseName := filepath.Base(path)
	// Remove extension
	namespace := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	// Sanitize for use as a namespace
	namespace = strings.ReplaceAll(namespace, "-", "_")
	namespace = strings.ReplaceAll(namespace, " ", "_")

	return namespace
}

// mergeFacts merges another TypedExprFacts into our facts
func (s *ExprSchema) mergeFacts(other *pathutil.TypedExprFacts) {
	// Get our facts instance
	if s.facts == nil {
		// If we don't have any facts yet, just use the other one
		s.facts = other
		return
	}

	// Merge the facts
	s.facts.MergeTypedFacts(other)
}

// Get retrieves a value from the facts
func (s *ExprSchema) Get(path string) (interface{}, bool) {
	return s.facts.Get(path)
}

// GetWithType retrieves a value and its type information
func (s *ExprSchema) GetWithType(path string) (interface{}, string, bool) {
	value, exists := s.facts.Get(path)
	if !exists {
		return nil, "", false
	}

	typeInfo := s.facts.GetTypeInfo(path)
	return value, typeInfo, true
}

// EvaluateExpr evaluates an expression using the facts
func (s *ExprSchema) EvaluateExpr(expr string) (interface{}, error) {
	return s.facts.GetUnderlyingFacts().EvaluateExpr(expr)
}

// EvaluateExprBool evaluates an expression that returns a boolean
func (s *ExprSchema) EvaluateExprBool(expr string) (bool, error) {
	return s.facts.GetUnderlyingFacts().EvaluateExprBool(expr)
}

// TypeCheckExpr type-checks an expression
func (s *ExprSchema) TypeCheckExpr(expr string) error {
	return s.facts.TypeCheck(expr)
}

// GetFacts returns the underlying TypedExprFacts
func (s *ExprSchema) GetFacts() *pathutil.TypedExprFacts {
	return s.facts
}

// AsFactProvider returns the facts as a FactProvider
func (s *ExprSchema) AsFactProvider() pathutil.FactProvider {
	return s.facts
}

// LoadFromFile loads a schema from a specific file
func (s *ExprSchema) LoadFromFile(path string) error {
	// Check file extension
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return s.loadJSONFile(path)
	case ".yaml", ".yml":
		return s.loadYAMLFile(path)
	default:
		return fmt.Errorf("unsupported file type: %s", ext)
	}
}

// RegisterType registers a type for a path
func (s *ExprSchema) RegisterType(path string, typeName string) {
	s.facts.GetUnderlyingFacts().EvaluateExpr(path)
	// This would typically update type information
	// The current implementation of TypedExprFacts doesn't expose
	// a direct way to register types by name, but we can add this
}

// RegisterFunction registers a function that can be used in expressions
func (s *ExprSchema) RegisterFunction(name string, fn interface{}) error {
	// This would require extending the ExprFacts to support custom functions
	// For now, we'll return an error
	return fmt.Errorf("function registration not implemented")
}

// MergeSchemas merges another ExprSchema into this one
func (s *ExprSchema) MergeSchemas(other *ExprSchema) {
	if other == nil || other.facts == nil {
		return
	}

	s.mergeFacts(other.facts)
}

// GetAllPaths returns all registered paths
func (s *ExprSchema) GetAllPaths() []string {
	if s.facts == nil {
		return nil
	}

	// Get all paths with any prefix
	return s.facts.GetPathsByPrefix("")
}

// GetTypedValue gets a value and converts it to the expected type
func (s *ExprSchema) GetTypedValue(path string, target interface{}) error {
	// Get the value
	value, exists := s.Get(path)
	if !exists {
		return fmt.Errorf("path not found: %s", path)
	}

	// Use reflection to set the target
	return setReflectValue(reflect.ValueOf(target), value)
}

// Helper to set a reflect value
func setReflectValue(target reflect.Value, value interface{}) error {
	// Ensure target is a pointer
	if target.Kind() != reflect.Ptr {
		return fmt.Errorf("target must be a pointer")
	}

	// Get the target element
	targetElem := target.Elem()

	// Handle nil value
	if value == nil {
		// Can only set nil for certain types
		switch targetElem.Kind() {
		case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice:
			targetElem.Set(reflect.Zero(targetElem.Type()))
			return nil
		default:
			return fmt.Errorf("cannot set nil for type %s", targetElem.Type())
		}
	}

	// Get value's type
	valueType := reflect.TypeOf(value)

	// If types match exactly, just set it
	if valueType.AssignableTo(targetElem.Type()) {
		targetElem.Set(reflect.ValueOf(value))
		return nil
	}

	// Try to convert numeric types
	if isNumericValue(value) && isNumericType(targetElem.Type()) {
		return convertNumeric(targetElem, value)
	}

	// Try to convert maps to structs
	if valueType.Kind() == reflect.Map && targetElem.Kind() == reflect.Struct {
		return convertMapToStruct(targetElem, value)
	}

	// Try to convert slices
	if valueType.Kind() == reflect.Slice && targetElem.Kind() == reflect.Slice {
		return convertSlice(targetElem, value)
	}

	return fmt.Errorf("cannot convert %s to %s", valueType, targetElem.Type())
}

// Helper functions for type conversion
func isNumericValue(value interface{}) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

func isNumericType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func convertNumeric(target reflect.Value, value interface{}) error {
	// Convert all to float64 first
	var floatVal float64
	switch v := value.(type) {
	case int:
		floatVal = float64(v)
	case int8:
		floatVal = float64(v)
	case int16:
		floatVal = float64(v)
	case int32:
		floatVal = float64(v)
	case int64:
		floatVal = float64(v)
	case uint:
		floatVal = float64(v)
	case uint8:
		floatVal = float64(v)
	case uint16:
		floatVal = float64(v)
	case uint32:
		floatVal = float64(v)
	case uint64:
		floatVal = float64(v)
	case float32:
		floatVal = float64(v)
	case float64:
		floatVal = v
	default:
		return fmt.Errorf("not a numeric type: %T", value)
	}

	// Set based on target type
	switch target.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		target.SetInt(int64(floatVal))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		target.SetUint(uint64(floatVal))
	case reflect.Float32, reflect.Float64:
		target.SetFloat(floatVal)
	default:
		return fmt.Errorf("target is not a numeric type: %s", target.Type())
	}

	return nil
}

func convertMapToStruct(target reflect.Value, value interface{}) error {
	// Get the map
	mapValue, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Errorf("expected map[string]interface{}, got %T", value)
	}

	// Set fields in the struct
	typ := target.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Skip unexported fields
		if field.PkgPath != "" {
			continue
		}

		// Get field name from json tag if available
		fieldName := field.Name
		if jsonTag := field.Tag.Get("json"); jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" && parts[0] != "-" {
				fieldName = parts[0]
			}
		}

		// Check if we have this field in the map
		if mapVal, ok := mapValue[fieldName]; ok {
			fieldValue := target.Field(i)
			if fieldValue.CanSet() {
				// Create a new pointer to use with setReflectValue
				ptr := reflect.New(fieldValue.Type())
				if err := setReflectValue(ptr, mapVal); err != nil {
					return fmt.Errorf("setting field %s: %w", field.Name, err)
				}
				fieldValue.Set(ptr.Elem())
			}
		}
	}

	return nil
}

func convertSlice(target reflect.Value, value interface{}) error {
	// Get the slice
	sliceValue, ok := value.([]interface{})
	if !ok {
		return fmt.Errorf("expected []interface{}, got %T", value)
	}

	// Create a new slice of the right type
	elemType := target.Type().Elem()
	newSlice := reflect.MakeSlice(target.Type(), len(sliceValue), len(sliceValue))

	// Set each element
	for i, val := range sliceValue {
		elemPtr := reflect.New(elemType)
		if err := setReflectValue(elemPtr, val); err != nil {
			return fmt.Errorf("setting slice element %d: %w", i, err)
		}
		newSlice.Index(i).Set(elemPtr.Elem())
	}

	// Set the target slice
	target.Set(newSlice)
	return nil
}

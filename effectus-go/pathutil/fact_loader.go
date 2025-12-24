package pathutil

import (
	"encoding/json"
	"fmt"
	"reflect"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// FactLoader is an interface for loading data from various sources into fact providers
type FactLoader interface {
	// LoadIntoFacts loads data into an ExprFacts provider
	LoadIntoFacts() (*ExprFacts, error)

	// LoadIntoTypedFacts loads data into a TypedExprFacts provider
	LoadIntoTypedFacts() (*TypedExprFacts, error)
}

// JSONLoader loads facts from JSON data
type JSONLoader struct {
	data []byte // Raw JSON data
}

// NewJSONLoader creates a new JSON loader
func NewJSONLoader(jsonData []byte) *JSONLoader {
	return &JSONLoader{
		data: jsonData,
	}
}

// LoadIntoFacts loads JSON data into an ExprFacts provider
func (l *JSONLoader) LoadIntoFacts() (*ExprFacts, error) {
	// Parse JSON into a map
	var data map[string]interface{}
	if err := json.Unmarshal(l.data, &data); err != nil {
		return nil, fmt.Errorf("parsing JSON data: %w", err)
	}

	// Create ExprFacts provider
	return NewExprFactsFromData(data), nil
}

// LoadIntoTypedFacts loads JSON data into a TypedExprFacts provider
func (l *JSONLoader) LoadIntoTypedFacts() (*TypedExprFacts, error) {
	// Parse JSON into a map
	var data map[string]interface{}
	if err := json.Unmarshal(l.data, &data); err != nil {
		return nil, fmt.Errorf("parsing JSON data: %w", err)
	}

	// Create provider and typed facts
	provider := NewExprFacts(data) // This returns *RegistryFactProvider
	return NewTypedExprFacts(provider), nil
}

// ProtoLoader loads facts from Protocol Buffer messages
type ProtoLoader struct {
	message proto.Message
}

// NewProtoLoader creates a new Protocol Buffer loader
func NewProtoLoader(message proto.Message) *ProtoLoader {
	return &ProtoLoader{
		message: message,
	}
}

// LoadIntoFacts loads Protocol Buffer data into an ExprFacts provider
func (l *ProtoLoader) LoadIntoFacts() (*ExprFacts, error) {
	// Marshal to JSON first
	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,  // Use original proto field names
		EmitUnpopulated: false, // Skip zero values
	}

	data, err := marshaler.Marshal(l.message)
	if err != nil {
		return nil, fmt.Errorf("marshaling proto message: %w", err)
	}

	// Use JSON loader to convert to ExprFacts
	return NewJSONLoader(data).LoadIntoFacts()
}

// LoadIntoTypedFacts loads Protocol Buffer data into a TypedExprFacts provider
func (l *ProtoLoader) LoadIntoTypedFacts() (*TypedExprFacts, error) {
	// Marshal to JSON first
	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,  // Use original proto field names
		EmitUnpopulated: false, // Skip zero values
	}

	data, err := marshaler.Marshal(l.message)
	if err != nil {
		return nil, fmt.Errorf("marshaling proto message: %w", err)
	}

	// Parse JSON and create provider
	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return nil, fmt.Errorf("parsing JSON data: %w", err)
	}

	provider := NewExprFacts(jsonData)
	return NewTypedExprFacts(provider), nil
}

// StructLoader loads facts from Go structs
type StructLoader struct {
	data interface{}
}

// NewStructLoader creates a new struct loader
func NewStructLoader(data interface{}) *StructLoader {
	return &StructLoader{
		data: data,
	}
}

// LoadIntoFacts loads struct data into an ExprFacts provider
func (l *StructLoader) LoadIntoFacts() (*ExprFacts, error) {
	// Convert struct to map using reflection
	data, err := structToMap(l.data)
	if err != nil {
		return nil, fmt.Errorf("converting struct to map: %w", err)
	}

	// Create ExprFacts provider
	return NewExprFactsFromData(data), nil
}

// LoadIntoTypedFacts loads struct data into a TypedExprFacts provider with type information
func (l *StructLoader) LoadIntoTypedFacts() (*TypedExprFacts, error) {
	// Convert struct to map using reflection
	data, err := structToMap(l.data)
	if err != nil {
		return nil, fmt.Errorf("converting struct to map: %w", err)
	}

	// Create provider and typed facts
	provider := NewExprFacts(data)
	return NewTypedExprFacts(provider), nil
}

// NamespacedLoader allows multiple sources to be loaded with different namespaces
type NamespacedLoader struct {
	sources map[string]interface{} // Map of namespace -> data source
}

// NewNamespacedLoader creates a new namespaced loader
func NewNamespacedLoader() *NamespacedLoader {
	return &NamespacedLoader{
		sources: make(map[string]interface{}),
	}
}

// AddSource adds a data source with a namespace
func (l *NamespacedLoader) AddSource(namespace string, source interface{}) *NamespacedLoader {
	l.sources[namespace] = source
	return l
}

// LoadIntoFacts loads all data sources into a unified ExprFacts provider
func (l *NamespacedLoader) LoadIntoFacts() (*ExprFacts, error) {
	// Create result map
	result := make(map[string]interface{})

	// Process each source
	for namespace, source := range l.sources {
		var data map[string]interface{}
		var err error

		switch s := source.(type) {
		case []byte:
			// Assume JSON
			loader := NewJSONLoader(s)
			facts, err := loader.LoadIntoFacts()
			if err != nil {
				return nil, fmt.Errorf("loading JSON for namespace %s: %w", namespace, err)
			}
			data = facts.data

		case proto.Message:
			// Protocol Buffer
			loader := NewProtoLoader(s)
			facts, err := loader.LoadIntoFacts()
			if err != nil {
				return nil, fmt.Errorf("loading proto for namespace %s: %w", namespace, err)
			}
			data = facts.data

		case map[string]interface{}:
			// Already a map
			data = s

		default:
			// Assume struct
			data, err = structToMap(s)
			if err != nil {
				return nil, fmt.Errorf("converting struct for namespace %s: %w", namespace, err)
			}
		}

		// Add to result with namespace
		result[namespace] = data
	}

	return NewExprFactsFromData(result), nil
}

// LoadIntoTypedFacts loads all data sources into a TypedExprFacts provider
func (l *NamespacedLoader) LoadIntoTypedFacts() (*TypedExprFacts, error) {
	// Create result map
	result := make(map[string]interface{})

	// Process each source
	for namespace, source := range l.sources {
		var data map[string]interface{}
		var err error

		switch s := source.(type) {
		case []byte:
			// Assume JSON
			loader := NewJSONLoader(s)
			facts, err := loader.LoadIntoFacts()
			if err != nil {
				return nil, fmt.Errorf("loading JSON for namespace %s: %w", namespace, err)
			}
			data = facts.data

		case proto.Message:
			// Protocol Buffer
			loader := NewProtoLoader(s)
			facts, err := loader.LoadIntoFacts()
			if err != nil {
				return nil, fmt.Errorf("loading proto for namespace %s: %w", namespace, err)
			}
			data = facts.data

		case map[string]interface{}:
			// Already a map
			data = s

		default:
			// Assume struct
			data, err = structToMap(s)
			if err != nil {
				return nil, fmt.Errorf("converting struct for namespace %s: %w", namespace, err)
			}
		}

		// Add to result with namespace
		result[namespace] = data
	}

	// Create provider and typed facts
	provider := NewExprFacts(result)
	return NewTypedExprFacts(provider), nil
}

// Helper functions for struct conversion

// structToMap converts a struct to a map[string]interface{} using reflection
func structToMap(data interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	// Handle nil
	if data == nil {
		return result, nil
	}

	val := reflect.ValueOf(data)
	typ := val.Type()

	// Handle pointer to struct
	if typ.Kind() == reflect.Ptr {
		if val.IsNil() {
			return result, nil
		}
		val = val.Elem()
		typ = val.Type()
	}

	// Ensure it's a struct
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %s", typ.Kind())
	}

	// Process each field
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Skip unexported fields
		if field.PkgPath != "" {
			continue
		}

		// Get field name (use json tag if available)
		fieldName := field.Name
		if jsonTag := field.Tag.Get("json"); jsonTag != "" {
			// Extract name part from json tag
			for i, c := range jsonTag {
				if c == ',' {
					jsonTag = jsonTag[:i]
					break
				}
			}
			if jsonTag != "-" {
				fieldName = jsonTag
			}
		}

		// Get field value
		fieldValue := val.Field(i).Interface()

		// Process nested struct
		if field.Type.Kind() == reflect.Struct {
			nestedMap, err := structToMap(fieldValue)
			if err != nil {
				return nil, err
			}
			result[fieldName] = nestedMap
		} else {
			// Store other types directly
			result[fieldName] = fieldValue
		}
	}

	return result, nil
}

// structToNestedMap does a deep conversion of structs to maps, including slices and nested structs
func structToNestedMap(data interface{}) (interface{}, error) {
	if data == nil {
		return nil, nil
	}

	val := reflect.ValueOf(data)
	typ := val.Type()

	// Handle pointer
	if typ.Kind() == reflect.Ptr {
		if val.IsNil() {
			return nil, nil
		}
		return structToNestedMap(val.Elem().Interface())
	}

	// Process based on type
	switch typ.Kind() {
	case reflect.Struct:
		// Convert struct to map
		result := make(map[string]interface{})

		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)

			// Skip unexported fields
			if field.PkgPath != "" {
				continue
			}

			// Get field name (use json tag if available)
			fieldName := field.Name
			if jsonTag := field.Tag.Get("json"); jsonTag != "" {
				// Extract name part from json tag
				for i, c := range jsonTag {
					if c == ',' {
						jsonTag = jsonTag[:i]
						break
					}
				}
				if jsonTag != "-" {
					fieldName = jsonTag
				}
			}

			// Get field value and process recursively
			fieldValue := val.Field(i).Interface()
			processed, err := structToNestedMap(fieldValue)
			if err != nil {
				return nil, err
			}

			result[fieldName] = processed
		}

		return result, nil

	case reflect.Slice, reflect.Array:
		// Convert slice to array
		result := make([]interface{}, val.Len())

		for i := 0; i < val.Len(); i++ {
			elemValue := val.Index(i).Interface()
			processed, err := structToNestedMap(elemValue)
			if err != nil {
				return nil, err
			}

			result[i] = processed
		}

		return result, nil

	case reflect.Map:
		// Convert map values
		result := make(map[string]interface{})

		iter := val.MapRange()
		for iter.Next() {
			key := iter.Key()
			value := iter.Value().Interface()

			// Skip non-string keys (expr requires string keys)
			if key.Kind() != reflect.String {
				continue
			}

			processed, err := structToNestedMap(value)
			if err != nil {
				return nil, err
			}

			result[key.String()] = processed
		}

		return result, nil

	default:
		// Leave primitive types as-is
		return data, nil
	}
}

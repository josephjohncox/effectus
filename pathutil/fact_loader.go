package pathutil

import (
	"encoding/json"
	"fmt"
	"reflect"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// FactLoader loads data into a registry-backed fact provider.
type FactLoader interface {
	Load() (*RegistryFactProvider, error)
}

// JSONLoader loads facts from JSON data.
type JSONLoader struct {
	data []byte
}

// NewJSONLoader creates a new JSON loader.
func NewJSONLoader(jsonData []byte) *JSONLoader {
	return &JSONLoader{data: jsonData}
}

// Load loads JSON data into a registry-backed provider.
func (l *JSONLoader) Load() (*RegistryFactProvider, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(l.data, &data); err != nil {
		return nil, fmt.Errorf("parsing JSON data: %w", err)
	}

	return NewRegistryFactProviderFromMap(data), nil
}

// ProtoLoader loads facts from Protocol Buffer messages.
type ProtoLoader struct {
	message proto.Message
}

// NewProtoLoader creates a new Protocol Buffer loader.
func NewProtoLoader(message proto.Message) *ProtoLoader {
	return &ProtoLoader{message: message}
}

// Load marshals proto data to JSON and loads it into a provider.
func (l *ProtoLoader) Load() (*RegistryFactProvider, error) {
	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}

	data, err := marshaler.Marshal(l.message)
	if err != nil {
		return nil, fmt.Errorf("marshaling proto message: %w", err)
	}

	return NewJSONLoader(data).Load()
}

// StructLoader loads facts from Go structs.
type StructLoader struct {
	data interface{}
}

// NewStructLoader creates a new struct loader.
func NewStructLoader(data interface{}) *StructLoader {
	return &StructLoader{data: data}
}

// Load converts struct data and loads it into a provider.
func (l *StructLoader) Load() (*RegistryFactProvider, error) {
	data, err := structToMap(l.data)
	if err != nil {
		return nil, fmt.Errorf("converting struct to map: %w", err)
	}

	return NewRegistryFactProviderFromMap(data), nil
}

// NamespacedLoader allows multiple sources to be loaded with different namespaces.
type NamespacedLoader struct {
	sources map[string]interface{}
}

// NewNamespacedLoader creates a new namespaced loader.
func NewNamespacedLoader() *NamespacedLoader {
	return &NamespacedLoader{sources: make(map[string]interface{})}
}

// AddSource adds a data source with a namespace.
func (l *NamespacedLoader) AddSource(namespace string, source interface{}) *NamespacedLoader {
	l.sources[namespace] = source
	return l
}

// Load loads all data sources into a unified provider.
func (l *NamespacedLoader) Load() (*RegistryFactProvider, error) {
	result := make(map[string]interface{})

	for namespace, source := range l.sources {
		data, err := sourceToMap(source)
		if err != nil {
			return nil, fmt.Errorf("converting source for namespace %s: %w", namespace, err)
		}
		result[namespace] = data
	}

	return NewRegistryFactProviderFromMap(result), nil
}

func sourceToMap(source interface{}) (map[string]interface{}, error) {
	switch s := source.(type) {
	case []byte:
		var data map[string]interface{}
		if err := json.Unmarshal(s, &data); err != nil {
			return nil, fmt.Errorf("parsing JSON: %w", err)
		}
		return data, nil
	case proto.Message:
		marshaler := protojson.MarshalOptions{
			UseProtoNames:   true,
			EmitUnpopulated: false,
		}
		data, err := marshaler.Marshal(s)
		if err != nil {
			return nil, fmt.Errorf("marshaling proto message: %w", err)
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return nil, fmt.Errorf("parsing JSON: %w", err)
		}
		return decoded, nil
	case map[string]interface{}:
		return s, nil
	default:
		return structToMap(s)
	}
}

// structToMap converts a struct to a map[string]interface{} using reflection.
func structToMap(data interface{}) (map[string]interface{}, error) {
	converted, err := structToNestedMap(data)
	if err != nil {
		return nil, err
	}
	if converted == nil {
		return map[string]interface{}{}, nil
	}
	result, ok := converted.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("expected struct, got %T", converted)
	}
	return result, nil
}

// structToNestedMap does a deep conversion of structs to maps, including slices and nested structs.
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

	switch typ.Kind() {
	case reflect.Struct:
		if typ.PkgPath() == "time" && typ.Name() == "Time" {
			return data, nil
		}

		result := make(map[string]interface{})

		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)

			if field.PkgPath != "" {
				continue
			}

			fieldName := field.Name
			if jsonTag := field.Tag.Get("json"); jsonTag != "" {
				for i, c := range jsonTag {
					if c == ',' {
						jsonTag = jsonTag[:i]
						break
					}
				}
				if jsonTag == "-" {
					continue
				}
				if jsonTag != "" {
					fieldName = jsonTag
				}
			}

			fieldValue := val.Field(i).Interface()
			processed, err := structToNestedMap(fieldValue)
			if err != nil {
				return nil, err
			}

			result[fieldName] = processed
		}

		return result, nil

	case reflect.Slice, reflect.Array:
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
		result := make(map[string]interface{})

		iter := val.MapRange()
		for iter.Next() {
			key := iter.Key()
			value := iter.Value().Interface()

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
		return data, nil
	}
}

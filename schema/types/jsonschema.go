package types

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// LoadJSONSchemaFile loads a JSON Schema (subset) into fact types.
func (ts *TypeSystem) LoadJSONSchemaFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("reading JSON schema file: %w", err)
	}
	return ts.LoadJSONSchemaBytes("", data)
}

// LoadJSONSchemaBytes loads a JSON Schema payload into fact types with a prefix.
func (ts *TypeSystem) LoadJSONSchemaBytes(prefix string, data []byte) error {
	var schema map[string]interface{}
	if err := json.Unmarshal(data, &schema); err != nil {
		return fmt.Errorf("parsing JSON schema: %w", err)
	}
	return ts.loadJSONSchemaWithRegister(prefix, schema, func(path string, typ *Type) {
		ts.RegisterFactType(path, typ)
	})
}

// LoadJSONSchemaBytesVersion loads a JSON Schema payload into versioned fact types.
func (ts *TypeSystem) LoadJSONSchemaBytesVersion(prefix, version string, data []byte, setDefault bool) error {
	if strings.TrimSpace(version) == "" {
		return ts.LoadJSONSchemaBytes(prefix, data)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(data, &schema); err != nil {
		return fmt.Errorf("parsing JSON schema: %w", err)
	}
	return ts.loadJSONSchemaWithRegister(prefix, schema, func(path string, typ *Type) {
		ts.RegisterFactTypeVersion(path, version, typ, setDefault)
	})
}

// LoadSchemaJSONBytes loads either JSON schema or Effectus schema entries.
func (ts *TypeSystem) LoadSchemaJSONBytes(prefix string, data []byte) error {
	return ts.loadSchemaJSONBytes(prefix, "", data, false)
}

// LoadSchemaJSONBytesVersion loads schema payloads into versioned fact types.
func (ts *TypeSystem) LoadSchemaJSONBytesVersion(prefix, version string, data []byte, setDefault bool) error {
	return ts.loadSchemaJSONBytes(prefix, version, data, setDefault)
}

func (ts *TypeSystem) loadSchemaJSONBytes(prefix, version string, data []byte, setDefault bool) error {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parsing schema json: %w", err)
	}
	switch payload := raw.(type) {
	case map[string]interface{}:
		if strings.TrimSpace(version) == "" {
			return ts.loadJSONSchemaWithRegister(prefix, payload, func(path string, typ *Type) {
				ts.RegisterFactType(path, typ)
			})
		}
		return ts.loadJSONSchemaWithRegister(prefix, payload, func(path string, typ *Type) {
			ts.RegisterFactTypeVersion(path, version, typ, setDefault)
		})
	case []interface{}:
		for _, entry := range payload {
			entryBytes, err := json.Marshal(entry)
			if err != nil {
				return fmt.Errorf("encoding schema entry: %w", err)
			}
			var parsed struct {
				Path string `json:"path"`
				Type Type   `json:"type"`
			}
			if err := json.Unmarshal(entryBytes, &parsed); err != nil {
				return fmt.Errorf("parsing schema entry: %w", err)
			}
			if strings.TrimSpace(parsed.Path) == "" {
				continue
			}
			fullPath := joinSchemaPrefix(prefix, parsed.Path)
			if strings.TrimSpace(version) == "" {
				ts.RegisterFactType(fullPath, &parsed.Type)
			} else {
				ts.RegisterFactTypeVersion(fullPath, version, &parsed.Type, setDefault)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported schema json format")
	}
}

func (ts *TypeSystem) loadJSONSchema(prefix string, schema map[string]interface{}) error {
	return ts.loadJSONSchemaWithRegister(prefix, schema, func(path string, typ *Type) {
		ts.RegisterFactType(path, typ)
	})
}

func (ts *TypeSystem) loadJSONSchemaWithRegister(prefix string, schema map[string]interface{}, register func(path string, typ *Type)) error {
	schemaType := normalizeJSONSchemaType(schema["type"])

	switch schemaType {
	case "object":
		if prefix != "" {
			register(prefix, NewObjectType())
		}
		properties, _ := schema["properties"].(map[string]interface{})
		for name, raw := range properties {
			child, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			childPrefix := name
			if prefix != "" {
				childPrefix = prefix + "." + name
			}
			if err := ts.loadJSONSchemaWithRegister(childPrefix, child, register); err != nil {
				return err
			}
		}
		return nil
	case "array":
		items, _ := schema["items"].(map[string]interface{})
		elemType := NewAnyType()
		if items != nil {
			if parsed, err := jsonSchemaTypeForPath(items); err == nil {
				elemType = parsed
			}
		}
		if prefix != "" {
			register(prefix, NewListType(elemType))
			childPrefix := prefix + "[]"
			if items != nil {
				_ = ts.loadJSONSchemaWithRegister(childPrefix, items, register)
			}
		}
		return nil
	case "string", "number", "integer", "boolean":
		if prefix != "" {
			if parsed, err := jsonSchemaTypeForPath(schema); err == nil {
				register(prefix, parsed)
			}
		}
		return nil
	default:
		if prefix != "" {
			if parsed, err := jsonSchemaTypeForPath(schema); err == nil {
				register(prefix, parsed)
			}
		}
		return nil
	}
}

func joinSchemaPrefix(prefix, path string) string {
	prefix = strings.Trim(prefix, ".")
	path = strings.Trim(path, ".")
	if prefix == "" {
		return path
	}
	if path == "" {
		return prefix
	}
	return prefix + "." + path
}

func normalizeJSONSchemaType(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return strings.ToLower(v)
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				if lower := strings.ToLower(str); lower != "null" {
					return lower
				}
			}
		}
	}
	return ""
}

func jsonSchemaTypeForPath(schema map[string]interface{}) (*Type, error) {
	kind := normalizeJSONSchemaType(schema["type"])
	switch kind {
	case "string":
		return NewStringType(), nil
	case "integer":
		return NewIntType(), nil
	case "number":
		return NewFloatType(), nil
	case "boolean":
		return NewBoolType(), nil
	case "object":
		return NewObjectType(), nil
	case "array":
		items, _ := schema["items"].(map[string]interface{})
		elemType := NewAnyType()
		if items != nil {
			if parsed, err := jsonSchemaTypeForPath(items); err == nil {
				elemType = parsed
			}
		}
		return NewListType(elemType), nil
	default:
		return NewAnyType(), fmt.Errorf("unsupported JSON schema type: %v", schema["type"])
	}
}

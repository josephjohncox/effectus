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

	var schema map[string]interface{}
	if err := json.Unmarshal(data, &schema); err != nil {
		return fmt.Errorf("parsing JSON schema file: %w", err)
	}

	return ts.loadJSONSchema("", schema)
}

func (ts *TypeSystem) loadJSONSchema(prefix string, schema map[string]interface{}) error {
	schemaType := normalizeJSONSchemaType(schema["type"])

	switch schemaType {
	case "object":
		if prefix != "" {
			ts.RegisterFactType(prefix, NewObjectType())
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
			if err := ts.loadJSONSchema(childPrefix, child); err != nil {
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
			ts.RegisterFactType(prefix, NewListType(elemType))
			childPrefix := prefix + "[]"
			if items != nil {
				_ = ts.loadJSONSchema(childPrefix, items)
			}
		}
		return nil
	case "string", "number", "integer", "boolean":
		if prefix != "" {
			if parsed, err := jsonSchemaTypeForPath(schema); err == nil {
				ts.RegisterFactType(prefix, parsed)
			}
		}
		return nil
	default:
		if prefix != "" {
			if parsed, err := jsonSchemaTypeForPath(schema); err == nil {
				ts.RegisterFactType(prefix, parsed)
			}
		}
		return nil
	}
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

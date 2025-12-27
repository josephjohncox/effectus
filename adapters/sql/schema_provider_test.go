package sql

import "testing"

func TestBuildJSONSchemaMapping(t *testing.T) {
	config := &SchemaConfig{}
	columns := []columnInfo{
		{Name: "id", DataType: "uuid", Nullable: false},
		{Name: "amount", DataType: "numeric", Nullable: false},
		{Name: "active", DataType: "bool", Nullable: true},
		{Name: "tags", DataType: "text[]", Nullable: true},
		{Name: "meta", DataType: "jsonb", Nullable: true},
	}

	schema := buildJSONSchema(config, columns)
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties in schema")
	}

	assertTypeContains(t, properties, "id", "string")
	assertTypeContains(t, properties, "amount", "number")
	assertTypeContains(t, properties, "active", "boolean")
	assertTypeContains(t, properties, "meta", "object")

	tags, ok := properties["tags"].(map[string]interface{})
	if !ok {
		t.Fatalf("tags property missing")
	}
	types := schemaTypes(tags["type"])
	hasArray := false
	for _, typ := range types {
		if typ == "array" {
			hasArray = true
			break
		}
	}
	if !hasArray {
		t.Fatalf("expected tags to include array type, got %v", tags["type"])
	}
	items, ok := tags["items"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected tags.items to be object")
	}
	if items["type"] != "string" {
		t.Fatalf("expected tags.items.type string, got %v", items["type"])
	}

	required := stringSlice(schema["required"])
	reqMap := make(map[string]struct{}, len(required))
	for _, item := range required {
		reqMap[item] = struct{}{}
	}
	if _, ok := reqMap["id"]; !ok {
		t.Fatalf("expected id to be required")
	}
	if _, ok := reqMap["amount"]; !ok {
		t.Fatalf("expected amount to be required")
	}
	if _, ok := reqMap["active"]; ok {
		t.Fatalf("expected active to be nullable")
	}
}

func assertTypeContains(t *testing.T, props map[string]interface{}, key, expected string) {
	t.Helper()
	property, ok := props[key].(map[string]interface{})
	if !ok {
		t.Fatalf("property %s missing", key)
	}
	types := schemaTypes(property["type"])
	for _, typ := range types {
		if typ == expected {
			return
		}
	}
	t.Fatalf("property %s expected type %s, got %v", key, expected, types)
}

func schemaTypes(value interface{}) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	case []string:
		return v
	default:
		return nil
	}
}

func stringSlice(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

package schema

import "fmt"

func validBufIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, c := range value {
		if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

func supportedBufFieldType(value interface{}) bool {
	switch typed := value.(type) {
	case string:
		return typed == "string" || typed == "integer" || typed == "number" || typed == "boolean"
	case map[string]interface{}:
		kind, ok := typed["type"].(string)
		return ok && supportedBufFieldType(kind)
	default:
		return false
	}
}

func checkBufSchemaNames(name string, fieldSets ...map[string]interface{}) error {
	if !validBufIdentifier(name) || !validBufIdentifier(toCamelCase(name)) {
		return fmt.Errorf("invalid protobuf schema name %q", name)
	}
	for _, fields := range fieldSets {
		for field, value := range fields {
			if !validBufIdentifier(field) {
				return fmt.Errorf("invalid protobuf field name %q", field)
			}
			if !supportedBufFieldType(value) {
				return fmt.Errorf("field %q requires a supported scalar type: string, integer, number, or boolean", field)
			}
		}
	}
	return nil
}

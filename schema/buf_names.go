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

func checkBufSchemaNames(name string, fieldSets ...map[string]interface{}) error {
	if !validBufIdentifier(name) || !validBufIdentifier(toCamelCase(name)) {
		return fmt.Errorf("invalid protobuf schema name %q", name)
	}
	for _, fields := range fieldSets {
		for field := range fields {
			if !validBufIdentifier(field) {
				return fmt.Errorf("invalid protobuf field name %q", field)
			}
		}
	}
	return nil
}

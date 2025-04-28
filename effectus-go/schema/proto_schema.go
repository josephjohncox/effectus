package schema

import (
	"strings"

	"github.com/effectus/effectus-go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ProtoSchema is an implementation of SchemaInfo backed by Protocol Buffer descriptors
type ProtoSchema struct {
	message proto.Message
}

// NewProtoSchema creates a new ProtoSchema from a proto.Message
func NewProtoSchema(message proto.Message) *ProtoSchema {
	return &ProtoSchema{
		message: message,
	}
}

// ValidatePath checks if a path is valid according to the Protocol Buffer schema
func (s *ProtoSchema) ValidatePath(path string) bool {
	if path == "" {
		return false
	}

	// Basic sanity checks for path format
	if strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") {
		return false
	}

	if strings.Contains(path, "..") {
		return false
	}

	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return false
	}

	// Start with the top-level message
	msg := s.message.ProtoReflect()

	// Navigate through the message fields according to the path
	var current interface{} = msg
	for i, part := range parts {
		if i == 0 {
			// Skip the first part, which is the namespace
			continue
		}

		// Handle nested messages by field name
		reflectMsg, ok := current.(protoreflect.Message)
		if !ok {
			return false
		}

		field := reflectMsg.Descriptor().Fields().ByName(protoreflect.Name(part))
		if field == nil {
			// Try with camelCase to snake_case conversion
			field = reflectMsg.Descriptor().Fields().ByName(protoreflect.Name(camelToSnake(part)))
			if field == nil {
				return false
			}
		}

		// For leaf fields, we're done
		if i == len(parts)-1 {
			return true
		}

		// For nested fields, continue validation
		if field.Kind() == protoreflect.MessageKind && !field.IsList() && !field.IsMap() {
			value := reflectMsg.Get(field)
			current = value.Message()
		} else {
			// Not a message type, cannot traverse further
			return false
		}
	}

	return true
}

// GetProtoFieldType returns the type of a field at the given path
func (s *ProtoSchema) GetProtoFieldType(path string) (*Type, bool) {
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return nil, false
	}

	// Start with the top-level message
	msg := s.message.ProtoReflect()

	// Navigate through the message fields according to the path
	var current interface{} = msg
	for i, part := range parts {
		if i == 0 {
			// Skip the first part, which is the namespace
			continue
		}

		// Handle nested messages by field name
		reflectMsg, ok := current.(protoreflect.Message)
		if !ok {
			return nil, false
		}

		field := reflectMsg.Descriptor().Fields().ByName(protoreflect.Name(part))
		if field == nil {
			// Try with camelCase to snake_case conversion
			field = reflectMsg.Descriptor().Fields().ByName(protoreflect.Name(camelToSnake(part)))
			if field == nil {
				return nil, false
			}
		}

		// For leaf fields, return the type
		if i == len(parts)-1 {
			return protoFieldToEffectusType(field), true
		}

		// For nested fields, continue traversal
		if field.Kind() == protoreflect.MessageKind && !field.IsList() && !field.IsMap() {
			value := reflectMsg.Get(field)
			current = value.Message()
		} else {
			// Not a message type, cannot traverse further
			return nil, false
		}
	}

	return nil, false
}

// ProtoSchemaLoader creates a SchemaInfo from Protocol Buffer files
type ProtoSchemaLoader struct {
	// Would contain logic to load and parse .proto files
}

// NewProtoSchemaLoader creates a new ProtoSchemaLoader
func NewProtoSchemaLoader() *ProtoSchemaLoader {
	return &ProtoSchemaLoader{}
}

// LoadProtoSchema loads a schema from a .proto file
func (l *ProtoSchemaLoader) LoadProtoSchema(protoFile string) (effectus.SchemaInfo, error) {
	// This would be implemented to load Protocol Buffer definitions and create a ProtoSchema
	// For now, just return a placeholder implementation
	return &SimpleSchema{}, nil
}

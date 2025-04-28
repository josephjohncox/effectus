package schema

import (
	"fmt"
	"strings"

	"github.com/effectus/effectus-go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ProtoFacts implements the Facts interface for Protocol Buffer messages
type ProtoFacts struct {
	message proto.Message
	schema  effectus.SchemaInfo
}

// NewProtoFacts creates a new ProtoFacts instance from a proto.Message
func NewProtoFacts(message proto.Message) *ProtoFacts {
	return &ProtoFacts{
		message: message,
		schema:  NewProtoSchema(message),
	}
}

// Get returns the value at the given path
func (f *ProtoFacts) Get(path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return nil, false
	}

	// Start with the top-level message
	msg := f.message.ProtoReflect()

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

		value := reflectMsg.Get(field)

		// Convert protoreflect values to Go types
		if i == len(parts)-1 {
			// This is the leaf value we want to return
			return convertProtoValueToGo(value, field), true
		}

		// For nested messages, continue traversing
		if field.Kind() == protoreflect.MessageKind && !field.IsList() && !field.IsMap() {
			current = value.Message()
		} else if field.Kind() == protoreflect.MessageKind && field.IsList() {
			// Handle repeated message fields
			list := value.List()
			result := make([]interface{}, list.Len())
			for j := 0; j < list.Len(); j++ {
				item := list.Get(j)
				result[j] = convertProtoValueToGo(item, field)
			}
			return result, true
		} else if field.Kind() == protoreflect.MessageKind && field.IsMap() {
			// Handle map fields
			mapValue := value.Map()
			result := make(map[string]interface{})
			mapValue.Range(func(key protoreflect.MapKey, value protoreflect.Value) bool {
				result[key.String()] = convertProtoValueToGo(value, field)
				return true
			})
			return result, true
		} else {
			// Not a message, can't traverse further
			return nil, false
		}
	}

	return nil, false
}

// Schema returns the schema information
func (f *ProtoFacts) Schema() effectus.SchemaInfo {
	return f.schema
}

// convertProtoValueToGo converts protoreflect.Value to appropriate Go types
func convertProtoValueToGo(value protoreflect.Value, field protoreflect.FieldDescriptor) interface{} {
	switch field.Kind() {
	case protoreflect.BoolKind:
		return value.Bool()
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return int(value.Int())
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return value.Int()
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return uint(value.Uint())
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return value.Uint()
	case protoreflect.FloatKind:
		return float32(value.Float())
	case protoreflect.DoubleKind:
		return value.Float()
	case protoreflect.StringKind:
		return value.String()
	case protoreflect.BytesKind:
		return value.Bytes()
	case protoreflect.EnumKind:
		return int(value.Enum())
	case protoreflect.MessageKind:
		if field.IsList() {
			list := value.List()
			result := make([]interface{}, list.Len())
			for i := 0; i < list.Len(); i++ {
				result[i] = convertMessageToMap(list.Get(i).Message())
			}
			return result
		} else if field.IsMap() {
			mapValue := value.Map()
			result := make(map[string]interface{})
			mapValue.Range(func(key protoreflect.MapKey, val protoreflect.Value) bool {
				if field.MapValue().Kind() == protoreflect.MessageKind {
					result[key.String()] = convertMessageToMap(val.Message())
				} else {
					result[key.String()] = convertProtoValueToGo(val, field.MapValue())
				}
				return true
			})
			return result
		} else {
			return convertMessageToMap(value.Message())
		}
	default:
		return nil
	}
}

// convertMessageToMap converts a protoreflect.Message to a map[string]interface{}
func convertMessageToMap(msg protoreflect.Message) map[string]interface{} {
	result := make(map[string]interface{})
	msg.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		name := string(field.Name())
		result[name] = convertProtoValueToGo(value, field)
		return true
	})
	return result
}

// camelToSnake converts camelCase to snake_case
func camelToSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && 'A' <= r && r <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// snakeToCamel converts snake_case to camelCase
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// ProtoFactsFromReflection creates a Facts implementation from a struct with reflection
// This is useful for users who have generated Go structs from protobuf
func ProtoFactsFromReflection(obj interface{}) (*ProtoFacts, error) {
	if obj == nil {
		return nil, fmt.Errorf("cannot create ProtoFacts from nil")
	}

	// Check if obj implements proto.Message
	msg, ok := obj.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("object does not implement proto.Message")
	}

	return NewProtoFacts(msg), nil
}

// registerProtoMessageTypes registers all message types from a proto message
func RegisterProtoMessageTypes(msg proto.Message, ts *TypeSystem, prefix string) {
	desc := msg.ProtoReflect().Descriptor()
	fields := desc.Fields()

	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		fieldName := string(field.Name())
		path := prefix + "." + fieldName

		// Register the field type
		typ := protoFieldToEffectusType(field)
		ts.RegisterFactType(path, typ)

		// Recursively register nested message types
		if field.Kind() == protoreflect.MessageKind && !field.IsList() && !field.IsMap() {
			// Get a default instance of the message to inspect its structure
			fieldMsg := msg.ProtoReflect().Get(field).Message().Interface()
			RegisterProtoMessageTypes(fieldMsg, ts, path)
		}
	}
}

// protoFieldToEffectusType converts a protobuf field to an Effectus type
func protoFieldToEffectusType(field protoreflect.FieldDescriptor) *Type {
	switch field.Kind() {
	case protoreflect.BoolKind:
		return &Type{PrimType: TypeBool}
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return &Type{PrimType: TypeInt}
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return &Type{PrimType: TypeFloat}
	case protoreflect.StringKind:
		return &Type{PrimType: TypeString}
	case protoreflect.BytesKind:
		return &Type{PrimType: TypeUnknown, Name: "bytes"}
	case protoreflect.EnumKind:
		return &Type{PrimType: TypeInt}
	case protoreflect.MessageKind:
		if field.IsList() {
			var elemType *Type
			if field.Message().FullName() != "" {
				elemType = &Type{
					PrimType: TypeUnknown,
					Name:     string(field.Message().FullName()),
				}
			} else {
				elemType = &Type{PrimType: TypeUnknown}
			}
			return &Type{
				PrimType: TypeList,
				ListType: elemType,
			}
		} else if field.IsMap() {
			keyType := &Type{PrimType: TypeString} // Maps in proto3 always have string keys
			valueType := protoFieldToEffectusType(field.MapValue())
			return &Type{
				PrimType:   TypeMap,
				MapKeyType: keyType,
				MapValType: valueType,
			}
		} else {
			return &Type{
				PrimType: TypeUnknown,
				Name:     string(field.Message().FullName()),
			}
		}
	default:
		return &Type{PrimType: TypeUnknown}
	}
}

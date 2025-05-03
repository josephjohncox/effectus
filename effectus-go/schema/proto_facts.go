package schema

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/effectus/effectus-go"
	"github.com/iancoleman/strcase"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ProtoFacts implements Facts for Protocol Buffer messages
type ProtoFacts struct {
	message  proto.Message
	schema   effectus.SchemaInfo
	resolver FactPathResolver
}

// NewProtoFacts creates a new ProtoFacts from a proto.Message
func NewProtoFacts(message proto.Message) *ProtoFacts {
	// Create a unified resolver
	typeSystem := NewTypeSystem()
	resolver := NewUnifiedPathResolver(typeSystem, false)

	return &ProtoFacts{
		message:  message,
		schema:   NewProtoSchema(message),
		resolver: resolver,
	}
}

// Get retrieves a value at a specified path
// The path is a string representing a traversal through message fields
// separated by dots (e.g., "user.address.street")
func (f *ProtoFacts) Get(path string) (interface{}, bool) {
	log.Printf("ProtoFacts: Get(%s)", path)

	// Parse the path into a FactPath
	factPath, err := ParseFactPath(path)
	if err != nil {
		log.Printf("ProtoFacts: error parsing path: %v", err)
		return nil, false
	}

	// Use the resolver to resolve the path
	return f.resolver.Resolve(f, factPath)
}

// Schema returns the schema information
func (f *ProtoFacts) Schema() effectus.SchemaInfo {
	return f.schema
}

// convertProtoValueToGo converts a protoreflect.Value to a Go type
func convertProtoValueToGo(value protoreflect.Value, field protoreflect.FieldDescriptor) interface{} {
	switch field.Kind() {
	case protoreflect.BoolKind:
		return value.Bool()
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return int32(value.Int())
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return value.Int()
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return uint32(value.Uint())
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
		return int32(value.Enum())
	case protoreflect.MessageKind:
		if field.IsList() {
			return convertListToSlice(value.List(), field)
		} else if field.IsMap() {
			return convertProtoMapToGoMap(value.Map(), field)
		} else {
			// Use protojson for message conversion
			return protoMessageToMap(value.Message())
		}
	default:
		return nil
	}
}

// protoMessageToMap converts a protoreflect.Message to a Go map using protojson
func protoMessageToMap(msg protoreflect.Message) map[string]interface{} {
	// First attempt to use the more efficient direct conversion
	result := make(map[string]interface{})
	msg.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		result[string(fd.Name())] = convertProtoValueToGo(v, fd)
		return true
	})

	// If there are no fields, use the protojson approach as a fallback
	if len(result) == 0 && msg.IsValid() {
		// Use protojson as a fallback for more complex messages
		// Get the underlying proto.Message
		if protoMsg, ok := msg.Interface().(proto.Message); ok {
			marshaler := protojson.MarshalOptions{
				UseProtoNames:   true,
				EmitUnpopulated: false,
			}

			messageData, err := marshaler.Marshal(protoMsg)
			if err == nil {
				json.Unmarshal(messageData, &result)
			}
		}
	}

	return result
}

// convertProtoMapToGoMap converts a protoreflect.Map to a Go map
func convertProtoMapToGoMap(protoMap protoreflect.Map, field protoreflect.FieldDescriptor) interface{} {
	result := make(map[interface{}]interface{})
	protoMap.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
		key := convertProtoKeyToGo(k, field.MapKey())
		value := convertProtoValueToGo(v, field.MapValue())
		result[key] = value
		return true
	})
	return result
}

// convertProtoKeyToGo converts a protoreflect.MapKey to a Go type
func convertProtoKeyToGo(key protoreflect.MapKey, fieldDesc protoreflect.FieldDescriptor) interface{} {
	switch fieldDesc.Kind() {
	case protoreflect.BoolKind:
		return key.Bool()
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return int32(key.Int())
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return key.Int()
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return uint32(key.Uint())
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return key.Uint()
	case protoreflect.StringKind:
		return key.String()
	default:
		return nil
	}
}

// convertListToSlice converts a protoreflect.List to a Go slice
func convertListToSlice(list protoreflect.List, field protoreflect.FieldDescriptor) interface{} {
	result := make([]interface{}, list.Len())
	for i := 0; i < list.Len(); i++ {
		result[i] = convertProtoValueToGo(list.Get(i), field)
	}
	return result
}

// camelToSnake converts camelCase to snake_case using a library
func camelToSnake(s string) string {
	return strcase.ToSnake(s)
}

// snakeToCamel converts snake_case to camelCase using a library
func snakeToCamel(s string) string {
	return strcase.ToCamel(s)
}

// ProtoFactsFromReflection creates a ProtoFacts from a Go interface{}
func ProtoFactsFromReflection(obj interface{}) (*ProtoFacts, error) {
	// Try to convert to a proto.Message
	msg, ok := obj.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("object is not a proto.Message")
	}
	return NewProtoFacts(msg), nil
}

// RegisterProtoMessageTypes registers all message types from a proto message
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
			// For nested message types, we need to create an example message to inspect
			msgType := field.Message()
			// We can't use New() directly, so register based on field descriptors
			RegisterProtoMessageFieldTypes(msgType, ts, path)
		}
	}
}

// RegisterProtoMessageFieldTypes registers types for all fields in a message descriptor
func RegisterProtoMessageFieldTypes(msgDesc protoreflect.MessageDescriptor, ts *TypeSystem, prefix string) {
	fields := msgDesc.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		fieldName := string(field.Name())
		path := prefix + "." + fieldName

		// Register the field type
		typ := protoFieldToEffectusType(field)
		ts.RegisterFactType(path, typ)

		// Recursively register nested message types
		if field.Kind() == protoreflect.MessageKind && !field.IsList() && !field.IsMap() {
			RegisterProtoMessageFieldTypes(field.Message(), ts, path)
		}
	}
}

// protoFieldToEffectusType converts a protoreflect.FieldDescriptor to an Effectus Type
func protoFieldToEffectusType(field protoreflect.FieldDescriptor) *Type {
	if field.IsList() {
		// Handle list type
		elemType := &Type{PrimType: TypeUnknown}

		// Get the element type
		switch field.Kind() {
		case protoreflect.BoolKind:
			elemType.PrimType = TypeBool
		case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
			protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
			protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
			protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
			elemType.PrimType = TypeInt
		case protoreflect.FloatKind, protoreflect.DoubleKind:
			elemType.PrimType = TypeFloat
		case protoreflect.StringKind:
			elemType.PrimType = TypeString
		case protoreflect.MessageKind:
			// For message types, use the message name as the type
			elemType.Name = string(field.Message().Name())
			elemType.PrimType = TypeUnknown
		default:
			elemType.PrimType = TypeUnknown
		}

		return &Type{
			PrimType: TypeList,
			ListType: elemType,
		}
	} else if field.IsMap() {
		// Handle map type
		keyType := &Type{PrimType: TypeUnknown}
		valType := &Type{PrimType: TypeUnknown}

		// Get the key type
		switch field.MapKey().Kind() {
		case protoreflect.BoolKind:
			keyType.PrimType = TypeBool
		case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
			protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
			protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
			protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
			keyType.PrimType = TypeInt
		case protoreflect.StringKind:
			keyType.PrimType = TypeString
		default:
			keyType.PrimType = TypeUnknown
		}

		// Get the value type
		switch field.MapValue().Kind() {
		case protoreflect.BoolKind:
			valType.PrimType = TypeBool
		case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
			protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
			protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
			protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
			valType.PrimType = TypeInt
		case protoreflect.FloatKind, protoreflect.DoubleKind:
			valType.PrimType = TypeFloat
		case protoreflect.StringKind:
			valType.PrimType = TypeString
		case protoreflect.MessageKind:
			// For message types, use the message name as the type
			valType.Name = string(field.MapValue().Message().Name())
			valType.PrimType = TypeUnknown
		default:
			valType.PrimType = TypeUnknown
		}

		return &Type{
			PrimType:   TypeMap,
			MapKeyType: keyType,
			MapValType: valType,
		}
	} else {
		// Handle scalar type
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
		case protoreflect.MessageKind:
			// For message types, use the message name as the type
			return &Type{
				Name:     string(field.Message().Name()),
				PrimType: TypeUnknown,
			}
		default:
			return &Type{PrimType: TypeUnknown}
		}
	}
}

// Get a type name string from a protoreflect.Kind
func fieldKindToTypeName(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "int32"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "int64"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "uint32"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "uint64"
	case protoreflect.FloatKind:
		return "float"
	case protoreflect.DoubleKind:
		return "double"
	case protoreflect.StringKind:
		return "string"
	case protoreflect.BytesKind:
		return "bytes"
	case protoreflect.EnumKind:
		return "enum"
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return "message"
	default:
		return "unknown"
	}
}

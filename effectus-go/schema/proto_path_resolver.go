package schema

import (
	"log"

	"github.com/iancoleman/strcase"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ProtoPathResolver is a PathResolver implementation for Protocol Buffer messages
type ProtoPathResolver struct{}

// CanResolve returns true if this resolver can handle Protocol Buffer data
func (r *ProtoPathResolver) CanResolve(data interface{}) bool {
	_, isProto := data.(proto.Message)
	_, isReflect := data.(protoreflect.Message)
	return isProto || isReflect
}

// ResolveSegment resolves a single path segment in a Protocol Buffer message
func (r *ProtoPathResolver) ResolveSegment(data interface{}, segment PathSegment) (interface{}, bool) {
	log.Printf("ProtoPathResolver: resolving segment %s in %T", segment.String(), data)

	// Get the protoreflect.Message representation
	var msg protoreflect.Message
	switch v := data.(type) {
	case proto.Message:
		msg = v.ProtoReflect()
	case protoreflect.Message:
		msg = v
	default:
		log.Printf("ProtoPathResolver: data is not a proto.Message: %T", data)
		return nil, false
	}

	// Try to find the field by name
	fieldName := segment.Name
	field := msg.Descriptor().Fields().ByName(protoreflect.Name(fieldName))
	if field == nil {
		// Try with camelCase to snake_case conversion
		snakeFieldName := strcase.ToSnake(fieldName)
		field = msg.Descriptor().Fields().ByName(protoreflect.Name(snakeFieldName))
		if field == nil {
			log.Printf("ProtoPathResolver: field %s not found", fieldName)
			return nil, false
		}
	}

	// Get the field value
	fieldValue := msg.Get(field)

	// Handle array indexing if present
	if segment.IndexExpr != nil {
		// Must be a list field
		if !field.IsList() {
			log.Printf("ProtoPathResolver: field %s is not a list", fieldName)
			return nil, false
		}

		// Get the list
		list := fieldValue.List()
		index := segment.IndexExpr.Value
		if index < 0 || index >= list.Len() {
			log.Printf("ProtoPathResolver: index %d out of bounds (0-%d)", index, list.Len()-1)
			return nil, false
		}

		// Get the value at the index
		indexValue := list.Get(index)
		return convertProtoValueToGo(indexValue, field), true
	}

	// Return the field value
	return convertProtoValueToGo(fieldValue, field), true
}

// GetType returns the type of a value at a path segment
func (r *ProtoPathResolver) GetType(data interface{}, segment PathSegment) *Type {
	// Get the protoreflect.Message representation
	var msg protoreflect.Message
	switch v := data.(type) {
	case proto.Message:
		msg = v.ProtoReflect()
	case protoreflect.Message:
		msg = v
	default:
		return &Type{PrimType: TypeUnknown}
	}

	// Try to find the field by name
	fieldName := segment.Name
	field := msg.Descriptor().Fields().ByName(protoreflect.Name(fieldName))
	if field == nil {
		// Try with camelCase to snake_case conversion
		snakeFieldName := strcase.ToSnake(fieldName)
		field = msg.Descriptor().Fields().ByName(protoreflect.Name(snakeFieldName))
		if field == nil {
			return &Type{PrimType: TypeUnknown}
		}
	}

	// Convert the field type to an Effectus type
	return protoFieldToEffectusType(field)
}

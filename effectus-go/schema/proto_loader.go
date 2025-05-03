// schema/proto_loader.go
package schema

import (
	"fmt"
	"os"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// LoadProtoSchema loads type information from a protobuf file
func (sr *SchemaRegistry) LoadProtoSchema(filepath string) error {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("reading proto file: %w", err)
	}

	// Parse file descriptor set
	fileDesc := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(content, fileDesc); err != nil {
		return fmt.Errorf("unmarshal proto descriptor: %w", err)
	}

	// Process each file descriptor
	for _, fd := range fileDesc.GetFile() {
		// Extract package name for namespace
		namespace := string(fd.GetPackage())

		// Process message types
		for _, msgType := range fd.GetMessageType() {
			registerMessageType(sr.typeSystem, msgType, namespace)
		}
	}

	return nil
}

// registerMessageType recursively registers message fields as fact types
func registerMessageType(ts *TypeSystem, msgDesc *descriptorpb.DescriptorProto, prefix string) {
	msgName := msgDesc.GetName()
	pathPrefix := prefix
	if pathPrefix != "" {
		pathPrefix += "."
	}
	pathPrefix += msgName

	// Register each field
	for _, field := range msgDesc.GetField() {
		fieldName := field.GetName()
		path := pathPrefix + "." + fieldName

		// Instead of using protoFieldToEffectusType which expects a protoreflect.FieldDescriptor,
		// create a Type directly based on the field type
		var typ *Type
		switch field.GetType() {
		case descriptorpb.FieldDescriptorProto_TYPE_STRING:
			typ = &Type{PrimType: TypeString}
		case descriptorpb.FieldDescriptorProto_TYPE_INT32, descriptorpb.FieldDescriptorProto_TYPE_INT64,
			descriptorpb.FieldDescriptorProto_TYPE_UINT32, descriptorpb.FieldDescriptorProto_TYPE_UINT64,
			descriptorpb.FieldDescriptorProto_TYPE_SINT32, descriptorpb.FieldDescriptorProto_TYPE_SINT64,
			descriptorpb.FieldDescriptorProto_TYPE_FIXED32, descriptorpb.FieldDescriptorProto_TYPE_FIXED64,
			descriptorpb.FieldDescriptorProto_TYPE_SFIXED32, descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
			typ = &Type{PrimType: TypeInt}
		case descriptorpb.FieldDescriptorProto_TYPE_FLOAT, descriptorpb.FieldDescriptorProto_TYPE_DOUBLE:
			typ = &Type{PrimType: TypeFloat}
		case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
			typ = &Type{PrimType: TypeBool}
		case descriptorpb.FieldDescriptorProto_TYPE_MESSAGE:
			if field.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
				// Repeated message = list
				typ = &Type{
					PrimType: TypeList,
					ListType: &Type{
						PrimType: TypeUnknown,
						Name:     strings.TrimPrefix(field.GetTypeName(), "."),
					},
				}
			} else {
				typ = &Type{
					PrimType: TypeUnknown,
					Name:     strings.TrimPrefix(field.GetTypeName(), "."),
				}
			}
		case descriptorpb.FieldDescriptorProto_TYPE_ENUM:
			// Enums are represented as string in Effectus
			typ = &Type{PrimType: TypeString}
		default:
			if field.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
				// Handle repeated fields (arrays/lists)
				elemType := &Type{PrimType: TypeUnknown}
				typ = &Type{
					PrimType: TypeList,
					ListType: elemType,
				}
			} else {
				typ = &Type{PrimType: TypeUnknown}
			}
		}

		ts.RegisterFactType(path, typ)

		// For nested messages, recursively register
		if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
			nestedTypeName := field.GetTypeName()
			// Extract nested message from the containing message or imports
			// This would need the full protobuf registry to resolve properly
			// For now, just use a placeholder for nested types
			ts.RegisterFactType(path, &Type{
				PrimType: TypeUnknown,
				Name:     strings.TrimPrefix(nestedTypeName, "."),
			})
		}
	}

	// Process nested types
	for _, nestedType := range msgDesc.GetNestedType() {
		registerMessageType(ts, nestedType, pathPrefix)
	}
}

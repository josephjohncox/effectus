package registry

import (
	"fmt"
	"os"
	"strings"

	"github.com/effectus/effectus-go/schema/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// ProtoSchemaLoader loads schemas from protobuf descriptor files
type ProtoSchemaLoader struct {
	// Optional prefix for fact paths
	pathPrefix string
}

// NewProtoSchemaLoader creates a new protobuf schema loader
func NewProtoSchemaLoader(prefix string) *ProtoSchemaLoader {
	return &ProtoSchemaLoader{
		pathPrefix: prefix,
	}
}

// CanLoad checks if this loader can handle the given file
func (l *ProtoSchemaLoader) CanLoad(path string) bool {
	return strings.HasSuffix(path, ".pb") || strings.HasSuffix(path, ".proto.bin")
}

// Load loads type definitions from a protobuf descriptor file
func (l *ProtoSchemaLoader) Load(path string, ts *types.TypeSystem) error {
	// Read the file
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading proto file: %w", err)
	}

	// Parse the FileDescriptorSet
	fileDescSet := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(content, fileDescSet); err != nil {
		return fmt.Errorf("parsing proto descriptor: %w", err)
	}

	// Process each file descriptor
	for _, fileDesc := range fileDescSet.GetFile() {
		if err := l.registerMessagesFromFile(fileDesc, ts); err != nil {
			return err
		}
	}

	return nil
}

// registerMessagesFromFile registers all messages from a file descriptor
func (l *ProtoSchemaLoader) registerMessagesFromFile(fileDesc *descriptorpb.FileDescriptorProto, ts *types.TypeSystem) error {
	// Get package name
	pkgName := fileDesc.GetPackage()

	// Process each message
	for _, msgDesc := range fileDesc.GetMessageType() {
		msgPath := pkgName
		if msgPath != "" {
			msgPath += "."
		}
		msgPath += msgDesc.GetName()

		if err := l.registerMessageType(msgDesc, msgPath, ts); err != nil {
			return err
		}
	}

	return nil
}

// registerMessageType registers a message type and its fields
func (l *ProtoSchemaLoader) registerMessageType(msgDesc *descriptorpb.DescriptorProto, path string, ts *types.TypeSystem) error {
	// Register the message type itself
	msgName := msgDesc.GetName()

	// Create a named type for the message
	msgType := &types.Type{
		Name:     msgName,
		PrimType: types.TypeMap,
		// Maps with string keys
		MapKeyType: &types.Type{PrimType: types.TypeString},
		// Map values depend on field types
		MapValType: &types.Type{PrimType: types.TypeUnknown},
	}

	// Register the message type
	ts.RegisterType(msgName, msgType)

	// Full package path for this message
	fullPath := path
	if l.pathPrefix != "" {
		fullPath = l.pathPrefix + "." + path
	}

	// Register each field
	for _, field := range msgDesc.GetField() {
		fieldName := field.GetName()
		fieldPath := fullPath + "." + fieldName

		// Convert field type
		fieldType, err := l.protoFieldToType(field, ts)
		if err != nil {
			return fmt.Errorf("converting field %s: %w", fieldName, err)
		}

		// Register the field type
		ts.RegisterFactType(fieldPath, fieldType)
	}

	// Process nested messages
	for _, nestedMsg := range msgDesc.GetNestedType() {
		nestedPath := path + "." + nestedMsg.GetName()
		if err := l.registerMessageType(nestedMsg, nestedPath, ts); err != nil {
			return err
		}
	}

	return nil
}

// protoFieldToType converts a protobuf field to a Type
func (l *ProtoSchemaLoader) protoFieldToType(field *descriptorpb.FieldDescriptorProto, ts *types.TypeSystem) (*types.Type, error) {
	// Check if field is repeated
	repeated := field.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED

	// Get the base type
	var baseType *types.Type

	switch field.GetType() {
	case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE,
		descriptorpb.FieldDescriptorProto_TYPE_FLOAT:
		baseType = &types.Type{PrimType: types.TypeFloat}

	case descriptorpb.FieldDescriptorProto_TYPE_INT64,
		descriptorpb.FieldDescriptorProto_TYPE_UINT64,
		descriptorpb.FieldDescriptorProto_TYPE_INT32,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED64,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED32,
		descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED32,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED64,
		descriptorpb.FieldDescriptorProto_TYPE_SINT32,
		descriptorpb.FieldDescriptorProto_TYPE_SINT64:
		baseType = &types.Type{PrimType: types.TypeInt}

	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		baseType = &types.Type{PrimType: types.TypeBool}

	case descriptorpb.FieldDescriptorProto_TYPE_STRING:
		baseType = &types.Type{PrimType: types.TypeString}

	case descriptorpb.FieldDescriptorProto_TYPE_MESSAGE:
		// Reference to another message
		typeName := field.GetTypeName()
		// Remove leading dot if present
		if typeName != "" && typeName[0] == '.' {
			typeName = typeName[1:]
		}
		// Get the short name (last component)
		parts := strings.Split(typeName, ".")
		shortName := parts[len(parts)-1]

		baseType = &types.Type{Name: shortName}

	case descriptorpb.FieldDescriptorProto_TYPE_ENUM:
		// Enums are represented as strings
		baseType = &types.Type{PrimType: types.TypeString}

	default:
		return nil, fmt.Errorf("unsupported proto field type: %v", field.GetType())
	}

	// If field is repeated, wrap in a list type
	if repeated {
		return &types.Type{
			PrimType: types.TypeList,
			ListType: baseType,
		}, nil
	}

	return baseType, nil
}

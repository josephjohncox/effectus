package registry

import (
	"fmt"
	"path/filepath"
	"os"
	"strings"

	"github.com/effectus/effectus-go/schema/types"
)

// ProtoLoader loads schema definitions from Protocol Buffers files
type ProtoLoader struct{}

// NewProtoLoader creates a new Protocol Buffers loader
func NewProtoLoader() *ProtoLoader {
	return &ProtoLoader{}
}

// CanLoad checks if the loader can handle this file
func (l *ProtoLoader) CanLoad(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".proto"
}

// Load loads schema definitions from a proto file
func (l *ProtoLoader) Load(path string, ts *types.TypeSystem) error {
	// Read the proto file
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading proto file: %w", err)
	}

	// Parse the file contents
	protoContent := string(data)

	// Extract message definitions
	messages, err := l.extractMessages(protoContent)
	if err != nil {
		return fmt.Errorf("extracting messages: %w", err)
	}

	// Process each message
	namespace := l.getNamespaceFromProto(protoContent)
	for _, msg := range messages {
		if err := l.processMessage(ts, msg, namespace); err != nil {
			return fmt.Errorf("processing message %s: %w", msg.Name, err)
		}
	}

	return nil
}

// protoMessage represents a parsed Protocol Buffers message
type protoMessage struct {
	Name   string
	Fields []protoField
}

// protoField represents a field in a Protocol Buffers message
type protoField struct {
	Name     string
	Type     string
	Repeated bool
	Optional bool
}

// extractMessages parses a proto file and extracts message definitions
func (l *ProtoLoader) extractMessages(content string) ([]protoMessage, error) {
	// Simplified parser for demo purposes - in a real implementation, use a proper parser
	messages := []protoMessage{}

	// Split into lines
	lines := strings.Split(content, "\n")

	var currentMessage *protoMessage
	inMessage := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		// Detect message start
		if strings.HasPrefix(line, "message ") {
			inMessage = true
			msgName := strings.TrimPrefix(line, "message ")
			msgName = strings.TrimSuffix(strings.TrimSpace(msgName), "{")
			msgName = strings.TrimSpace(msgName)

			currentMessage = &protoMessage{
				Name:   msgName,
				Fields: []protoField{},
			}
		} else if inMessage && line == "}" {
			// Message end
			inMessage = false
			messages = append(messages, *currentMessage)
			currentMessage = nil
		} else if inMessage && currentMessage != nil {
			// Process field
			l.parseField(line, currentMessage)
		}
	}

	return messages, nil
}

// parseField parses a field definition
func (l *ProtoLoader) parseField(line string, msg *protoMessage) {
	// Skip if not a field definition
	if !strings.Contains(line, ";") {
		return
	}

	// Remove trailing semicolon and any comments
	line = strings.Split(line, ";")[0]
	if idx := strings.Index(line, "//"); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(line)

	// Check for repeated/optional
	repeated := strings.HasPrefix(line, "repeated ")
	if repeated {
		line = strings.TrimPrefix(line, "repeated ")
	}

	optional := strings.HasPrefix(line, "optional ")
	if optional {
		line = strings.TrimPrefix(line, "optional ")
	}

	// Split type and name
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return
	}

	fieldType := parts[0]
	fieldName := parts[1]

	// Handle equal sign (for field numbers)
	if idx := strings.Index(fieldName, "="); idx >= 0 {
		fieldName = strings.TrimSpace(fieldName[:idx])
	}

	field := protoField{
		Name:     fieldName,
		Type:     fieldType,
		Repeated: repeated,
		Optional: optional,
	}

	msg.Fields = append(msg.Fields, field)
}

// getNamespaceFromProto extracts the package name from a proto file
func (l *ProtoLoader) getNamespaceFromProto(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			pkg := strings.TrimPrefix(line, "package ")
			pkg = strings.TrimSuffix(pkg, ";")
			return strings.TrimSpace(pkg)
		}
	}
	return "proto"
}

// processMessage converts a proto message to schema types
func (l *ProtoLoader) processMessage(ts *types.TypeSystem, msg protoMessage, namespace string) error {
	// Create object type for message
	msgType := types.NewObjectType()
	msgType.Name = msg.Name

	// Register message type
	msgPath := namespace + "." + msg.Name
	ts.RegisterFactType(msgPath, msgType)

	// Process all fields
	for _, field := range msg.Fields {
		fieldPath := msgPath + "." + field.Name
		fieldType := l.protoTypeToEffectusType(field.Type)

		// Handle repeated fields (arrays)
		if field.Repeated {
			fieldType = types.NewListType(fieldType)
		}

		// Register field type
		ts.RegisterFactType(fieldPath, fieldType)

		// Add property to message type
		if err := msgType.AddProperty(field.Name, fieldType); err != nil {
			return fmt.Errorf("adding property %s: %w", field.Name, err)
		}
	}

	return nil
}

// protoTypeToEffectusType converts Protocol Buffers types to Effectus types
func (l *ProtoLoader) protoTypeToEffectusType(protoType string) *types.Type {
	switch protoType {
	case "bool":
		return types.NewBoolType()
	case "int32", "int64", "uint32", "uint64", "sint32", "sint64", "fixed32", "fixed64", "sfixed32", "sfixed64":
		return types.NewIntType()
	case "float", "double":
		return types.NewFloatType()
	case "string", "bytes":
		return types.NewStringType()
	default:
		// Assume it's a message reference
		return &types.Type{
			ReferenceType: protoType,
		}
	}
}

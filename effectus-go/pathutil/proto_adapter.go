package pathutil

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ProtoAdapter is an adapter for protocol buffer messages
type ProtoAdapter struct {
	provider FactProvider
}

// NewProtoAdapter creates a new adapter for a protocol buffer message
func NewProtoAdapter(message proto.Message) (*ProtoAdapter, error) {
	// Marshal proto message to JSON
	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,  // Use original proto field names instead of lowerCamelCase
		EmitUnpopulated: false, // Omit unpopulated fields
	}

	jsonBytes, err := marshaler.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal protobuf message: %w", err)
	}

	// Parse JSON into a map
	var data map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON to map: %w", err)
	}

	// Create facts provider
	provider := NewExprFacts(data)

	return &ProtoAdapter{provider: provider}, nil
}

// Get implements FactProvider interface
func (a *ProtoAdapter) Get(path string) (interface{}, bool) {
	return a.provider.Get(path)
}

// GetWithContext implements FactProvider interface
func (a *ProtoAdapter) GetWithContext(path string) (interface{}, *ResolutionResult) {
	return a.provider.GetWithContext(path)
}

// WithMessage creates a new adapter with a different message
func (a *ProtoAdapter) WithMessage(message proto.Message) (*ProtoAdapter, error) {
	return NewProtoAdapter(message)
}

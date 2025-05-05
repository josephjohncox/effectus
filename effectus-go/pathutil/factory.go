package pathutil

import (
	"encoding/json"

	"google.golang.org/protobuf/proto"
)

// NewFactProviderFromJSON creates a FactProvider from a JSON string
func NewFactProviderFromJSON(jsonData string) FactProvider {
	// Parse JSON into a map
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		// If we can't parse JSON, return an empty provider
		return NewExprFacts(make(map[string]interface{}))
	}
	return NewExprFacts(data)
}

// NewFactProviderFromMap creates a FactProvider from a map
func NewFactProviderFromMap(data map[string]interface{}) FactProvider {
	return NewExprFacts(data)
}

// NewFactProviderFromProto creates a FactProvider from a protobuf message
func NewFactProviderFromProto(message proto.Message) (FactProvider, error) {
	adapter, err := NewProtoAdapter(message)
	if err != nil {
		return nil, err
	}
	return adapter, nil
}

// NewFactProviderFromStruct creates a FactProvider from a struct
func NewFactProviderFromStruct(data interface{}) (FactProvider, error) {
	// Create a registry with one namespace
	registry := NewStructFactRegistry()
	registry.Register("data", data)

	// Return the registry as the fact provider
	return registry, nil
}

// CreateNamespacedRegistry creates a registry with multiple providers
// Takes a map of namespace -> data sources (can be JSON strings, maps, proto messages, or structs)
func CreateNamespacedRegistry(sources map[string]interface{}) (*Registry, error) {
	registry := NewRegistry()

	// Create a struct registry for all struct-based data
	structRegistry := NewStructFactRegistry()
	hasStructs := false

	for namespace, source := range sources {
		var provider FactProvider
		var err error

		switch src := source.(type) {
		case string:
			// Assume JSON string
			provider = NewFactProviderFromJSON(src)
			registry.Register(namespace, provider)
		case map[string]interface{}:
			// Map
			provider = NewFactProviderFromMap(src)
			registry.Register(namespace, provider)
		case proto.Message:
			// Protobuf message
			provider, err = NewFactProviderFromProto(src)
			if err != nil {
				return nil, err
			}
			registry.Register(namespace, provider)
		default:
			// It's a struct, add it to our struct registry
			structRegistry.Register(namespace, src)
			hasStructs = true
		}
	}

	// If we have any structs, register the struct registry as a provider
	if hasStructs {
		registry.Register("", structRegistry)
	}

	return registry, nil
}

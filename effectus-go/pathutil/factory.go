package pathutil

import (
	"google.golang.org/protobuf/proto"
)

// NewFactProviderFromJSON creates a FactProvider from a JSON string
func NewFactProviderFromJSON(jsonData string) FactProvider {
	return NewGjsonProviderFromJSON(jsonData)
}

// NewFactProviderFromMap creates a FactProvider from a map
func NewFactProviderFromMap(data map[string]interface{}) FactProvider {
	return NewGjsonProvider(data)
}

// NewFactProviderFromProto creates a FactProvider from a protobuf message
func NewFactProviderFromProto(message proto.Message) (FactProvider, error) {
	return NewProtoAdapter(message)
}

// NewFactProviderFromStruct creates a FactProvider from a struct
func NewFactProviderFromStruct(data interface{}) (FactProvider, error) {
	return NewGjsonProvider(data), nil
}

// CreateNamespacedRegistry creates a registry with multiple providers
// Takes a map of namespace -> data sources (can be JSON strings, maps, proto messages, or structs)
func CreateNamespacedRegistry(sources map[string]interface{}) (*Registry, error) {
	registry := NewRegistry()

	for namespace, source := range sources {
		var provider FactProvider
		var err error

		switch src := source.(type) {
		case string:
			// Assume JSON string
			provider = NewFactProviderFromJSON(src)
		case map[string]interface{}:
			// Map
			provider = NewFactProviderFromMap(src)
		case proto.Message:
			// Protobuf message
			provider, err = NewFactProviderFromProto(src)
			if err != nil {
				return nil, err
			}
		default:
			// Try as struct
			provider = NewGjsonProvider(src)
		}

		registry.Register(namespace, provider)
	}

	return registry, nil
}

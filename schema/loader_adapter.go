package schema

import (
	"context"
	"fmt"

	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/schema/verb"
)

// LoaderAdapter adapts the ExtensionManager to work with existing registries
type LoaderAdapter struct {
	registry     *Registry
	verbRegistry *verb.Registry
}

// NewLoaderAdapter creates a new adapter for the extension system
func NewLoaderAdapter(registry *Registry, verbRegistry *verb.Registry) *LoaderAdapter {
	return &LoaderAdapter{
		registry:     registry,
		verbRegistry: verbRegistry,
	}
}

// RegisterVerb implements loader.LoadTarget for verb registration
func (la *LoaderAdapter) RegisterVerb(spec loader.VerbSpec, executor loader.VerbExecutor) error {
	// Convert loader interfaces to our concrete verb.Spec type
	verbSpec := &verb.Spec{
		Name:         spec.GetName(),
		Description:  spec.GetDescription(),
		Capability:   convertCapabilities(spec.GetCapabilities()),
		ArgTypes:     spec.GetArgTypes(),
		ReturnType:   spec.GetReturnType(),
		RequiredArgs: spec.GetRequiredArgs(),
		Resources:    convertResources(spec.GetResources()),
		Inverse:      spec.GetInverseVerb(),
		Executor:     executor,
	}

	return la.verbRegistry.RegisterVerb(verbSpec)
}

// RegisterFunction implements loader.LoadTarget for function registration
func (la *LoaderAdapter) RegisterFunction(name string, fn interface{}) error {
	la.registry.RegisterFunction(name, fn)
	return nil
}

// LoadData implements loader.LoadTarget for data loading
func (la *LoaderAdapter) LoadData(path string, value interface{}) error {
	la.registry.Set(path, value)
	return nil
}

// RegisterType implements loader.LoadTarget for type registration
func (la *LoaderAdapter) RegisterType(name string, typeDef loader.TypeDefinition) error {
	// Store type definitions in registry metadata (for future use)
	typeKey := fmt.Sprintf("__type:%s", name)
	la.registry.Set(typeKey, typeDef)
	return nil
}

// Helper functions to convert capabilities and types

func convertCapabilities(caps []string) verb.Capability {
	result := verb.CapNone
	for _, cap := range caps {
		switch cap {
		case "read":
			result |= verb.CapRead
		case "write":
			result |= verb.CapWrite
		case "create":
			result |= verb.CapCreate
		case "delete":
			result |= verb.CapDelete
		case "idempotent":
			result |= verb.CapIdempotent
		case "exclusive":
			result |= verb.CapExclusive
		case "commutative":
			result |= verb.CapCommutative
		}
	}
	return result
}

func convertResources(resources []loader.ResourceSpec) verb.ResourceSet {
	result := make(verb.ResourceSet, 0, len(resources))
	for _, res := range resources {
		result = append(result, verb.ResourceCapability{
			Resource: res.GetResource(),
			Cap:      convertCapabilities(res.GetCapabilities()),
		})
	}
	return result
}

// === Convenience Functions ===

// LoadExtensionsIntoRegistries loads extensions from an ExtensionManager into registries
func LoadExtensionsIntoRegistries(em *loader.ExtensionManager, registry *Registry, verbRegistry *verb.Registry) error {
	adapter := NewLoaderAdapter(registry, verbRegistry)
	return em.LoadExtensions(context.Background(), adapter)
}

// CreateExtensionManagerWithDefaults creates an ExtensionManager with directory scanning
func CreateExtensionManagerWithDefaults(extensionDirs ...string) (*loader.ExtensionManager, error) {
	em := loader.NewExtensionManager()

	// Scan provided directories for extensions
	for _, dir := range extensionDirs {
		loaders, err := loader.LoadFromDirectory(dir)
		if err != nil {
			return nil, fmt.Errorf("loading from directory %s: %w", dir, err)
		}

		for _, l := range loaders {
			em.AddLoader(l)
		}
	}

	return em, nil
}

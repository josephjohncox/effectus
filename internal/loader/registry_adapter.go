package loader

import (
	"fmt"

	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/verb"
)

// registryAdapter adapts extension loading to the schema registries.
type registryAdapter struct {
	registry     *schema.Registry
	verbRegistry *verb.Registry
}

// NewRegistryAdapter creates a registry adapter for internal extension loading.
func NewRegistryAdapter(registry *schema.Registry, verbRegistry *verb.Registry) *registryAdapter {
	return &registryAdapter{registry: registry, verbRegistry: verbRegistry}
}

func (adapter *registryAdapter) RegisterVerb(spec VerbSpec, executor VerbExecutor) error {
	verbSpec := &verb.Spec{
		Name:         spec.GetName(),
		Description:  spec.GetDescription(),
		Capability:   registryCapabilities(spec.GetCapabilities()),
		ArgTypes:     cloneRegistryStringMap(spec.GetArgTypes()),
		ReturnType:   spec.GetReturnType(),
		RequiredArgs: append([]string(nil), spec.GetRequiredArgs()...),
		Resources:    registryResources(spec.GetResources()),
		Inverse:      spec.GetInverseVerb(),
		Executor:     executor,
	}
	return adapter.verbRegistry.RegisterVerb(verbSpec)
}

func (adapter *registryAdapter) RegisterVerbDescriptor(spec VerbSpec, descriptor ExecutorDescriptor) error {
	if err := adapter.RegisterVerb(spec, nil); err != nil {
		return err
	}
	source := verb.SourceInfo{Type: descriptor.Type}
	switch descriptor.Type {
	case "http":
		source.Ref, _ = descriptor.Config["url"].(string)
	case "grpc":
		source.Ref, _ = descriptor.Config["address"].(string)
	case "oci":
		source.Ref, _ = descriptor.Config["ref"].(string)
	}
	adapter.verbRegistry.SetVerbSource(spec.GetName(), source)
	return nil
}

func (adapter *registryAdapter) RegisterFunction(name string, fn interface{}) error {
	adapter.registry.RegisterFunction(name, fn)
	return nil
}

func (adapter *registryAdapter) LoadData(path string, value interface{}) error {
	adapter.registry.Set(path, value)
	return nil
}

func (adapter *registryAdapter) RegisterType(name string, typeDef TypeDefinition) error {
	adapter.registry.Set(fmt.Sprintf("__type:%s", name), typeDef)
	return nil
}

func cloneRegistryStringMap(values map[string]string) map[string]string {
	clone := make(map[string]string, len(values))
	for name, value := range values {
		clone[name] = value
	}
	return clone
}

func registryCapabilities(caps []string) verb.Capability {
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

func registryResources(resources []ResourceSpec) verb.ResourceSet {
	result := make(verb.ResourceSet, 0, len(resources))
	for _, resource := range resources {
		result = append(result, verb.ResourceCapability{
			Resource: resource.GetResource(),
			Cap:      registryCapabilities(resource.GetCapabilities()),
		})
	}
	return result
}

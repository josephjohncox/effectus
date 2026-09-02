package loader

import (
	"context"
	"testing"

	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/verb"
	"github.com/stretchr/testify/require"
)

func TestRegistryAdapterClonesMutableVerbContract(t *testing.T) {
	registry := schema.NewRegistry()
	verbs := verb.NewRegistry(registry)
	adapter := NewRegistryAdapter(registry, verbs)
	spec := &mutableRegistryVerbSpec{
		name: "write", args: map[string]string{"value": "string"}, required: []string{"value"}, capabilities: []string{"write"},
	}
	require.NoError(t, adapter.RegisterVerb(spec, registryTestExecutor{}))
	spec.args["value"] = "int"
	spec.required[0] = "changed"

	registered, ok := verbs.GetVerb("write")
	require.True(t, ok)
	require.Equal(t, "string", registered.ArgTypes["value"])
	require.Equal(t, []string{"value"}, registered.RequiredArgs)
}

type mutableRegistryVerbSpec struct {
	name         string
	args         map[string]string
	required     []string
	capabilities []string
}

func (spec *mutableRegistryVerbSpec) GetName() string           { return spec.name }
func (*mutableRegistryVerbSpec) GetDescription() string         { return "" }
func (spec *mutableRegistryVerbSpec) GetCapabilities() []string { return spec.capabilities }
func (*mutableRegistryVerbSpec) GetResources() []ResourceSpec   { return nil }
func (spec *mutableRegistryVerbSpec) GetArgTypes() map[string]string {
	return spec.args
}
func (spec *mutableRegistryVerbSpec) GetRequiredArgs() []string { return spec.required }
func (*mutableRegistryVerbSpec) GetReturnType() string          { return "void" }
func (*mutableRegistryVerbSpec) GetInverseVerb() string         { return "" }

type registryTestExecutor struct{}

func (registryTestExecutor) Execute(context.Context, map[string]interface{}) (interface{}, error) {
	return nil, nil
}

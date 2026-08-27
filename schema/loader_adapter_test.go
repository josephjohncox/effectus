package schema

import (
	"context"
	"testing"

	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/stretchr/testify/require"
)

func TestLoaderAdapterClonesMutableVerbContract(t *testing.T) {
	registry := NewRegistry()
	verbs := verb.NewRegistry(registry)
	adapter := NewLoaderAdapter(registry, verbs)
	spec := &mutableLoaderVerbSpec{
		name: "write", args: map[string]string{"value": "string"}, required: []string{"value"}, capabilities: []string{"write"},
	}
	require.NoError(t, adapter.RegisterVerb(spec, loaderTestExecutor{}))
	spec.args["value"] = "int"
	spec.required[0] = "changed"

	registered, ok := verbs.GetVerb("write")
	require.True(t, ok)
	require.Equal(t, "string", registered.ArgTypes["value"])
	require.Equal(t, []string{"value"}, registered.RequiredArgs)
}

type mutableLoaderVerbSpec struct {
	name         string
	args         map[string]string
	required     []string
	capabilities []string
}

func (spec *mutableLoaderVerbSpec) GetName() string                { return spec.name }
func (*mutableLoaderVerbSpec) GetDescription() string              { return "" }
func (spec *mutableLoaderVerbSpec) GetCapabilities() []string      { return spec.capabilities }
func (*mutableLoaderVerbSpec) GetResources() []loader.ResourceSpec { return nil }
func (spec *mutableLoaderVerbSpec) GetArgTypes() map[string]string { return spec.args }
func (spec *mutableLoaderVerbSpec) GetRequiredArgs() []string      { return spec.required }
func (*mutableLoaderVerbSpec) GetReturnType() string               { return "void" }
func (*mutableLoaderVerbSpec) GetInverseVerb() string              { return "" }

type loaderTestExecutor struct{}

func (loaderTestExecutor) Execute(context.Context, map[string]interface{}) (interface{}, error) {
	return nil, nil
}

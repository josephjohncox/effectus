package verb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryCopiesSpecs(t *testing.T) {
	strict := true
	input := &Spec{Name: "charge", ArgTypes: map[string]string{"amount": "int"}, RequiredArgs: []string{"amount"}, Resources: ResourceSet{{Resource: "account", Cap: CapWrite}}, StrictArgs: &strict}
	registry := NewRegistry(nil)
	require.NoError(t, registry.RegisterVerb(input))
	hash := registry.GetVerbHash()
	input.ArgTypes["amount"] = "string"
	input.RequiredArgs[0] = "other"
	*input.StrictArgs = false
	got, ok := registry.GetVerb("charge")
	require.True(t, ok)
	got.ArgTypes["amount"] = "number"
	got.RequiredArgs[0] = "changed"
	all := registry.GetAllVerbs()
	all[0].Resources[0].Resource = "changed"

	stable, ok := registry.GetVerb("charge")
	require.True(t, ok)
	require.Equal(t, "int", stable.ArgTypes["amount"])
	require.Equal(t, []string{"amount"}, stable.RequiredArgs)
	require.Equal(t, "account", stable.Resources[0].Resource)
	require.True(t, *stable.StrictArgs)
	require.Equal(t, hash, registry.GetVerbHash())
}

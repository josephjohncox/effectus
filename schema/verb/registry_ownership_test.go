package verb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryOwnsContractState(t *testing.T) {
	executor := NewFunctionExecutor(func(context.Context, map[string]interface{}) (interface{}, error) { return nil, nil })
	strict := true
	original := &Spec{
		Name: "write", ArgTypes: map[string]string{"id": "string"}, RequiredArgs: []string{"id"},
		Resources: ResourceSet{{Resource: "orders", Cap: CapWrite}}, StrictArgs: &strict, Executor: executor,
	}
	registry := NewRegistry(nil)
	require.NoError(t, registry.RegisterVerb(original))

	original.ArgTypes["id"] = "int"
	original.RequiredArgs[0] = "changed"
	original.Resources[0].Resource = "changed"
	*original.StrictArgs = false

	stored, ok := registry.GetVerb("write")
	require.True(t, ok)
	require.Equal(t, "string", stored.ArgTypes["id"])
	require.Equal(t, "id", stored.RequiredArgs[0])
	require.Equal(t, "orders", stored.Resources[0].Resource)
	require.True(t, *stored.StrictArgs)
	require.Same(t, executor, stored.Executor)

	stored.ArgTypes["id"] = "float"
	stored.RequiredArgs[0] = "again"
	stored.Resources[0].Resource = "again"
	*stored.StrictArgs = false
	again, _ := registry.GetVerb("write")
	require.Equal(t, "string", again.ArgTypes["id"])
	require.Equal(t, "id", again.RequiredArgs[0])
	require.Equal(t, "orders", again.Resources[0].Resource)
	require.True(t, *again.StrictArgs)

	setting := true
	registry.SetStrictArgs(&setting)
	setting = false
	returned := registry.StrictArgs()
	require.True(t, *returned)
	*returned = false
	require.True(t, *registry.StrictArgs())
}

func TestRegistryHashTracksContractState(t *testing.T) {
	strict := true
	original := &Spec{Name: "read", ArgTypes: map[string]string{"id": "string"}, Description: "v1", StrictReturn: &strict}
	registry := NewRegistry(nil)
	require.NoError(t, registry.RegisterVerb(original))
	first := registry.GetVerbHash()

	original.ArgTypes["id"] = "int"
	original.Description = "mutated"
	*original.StrictReturn = false
	require.Equal(t, first, registry.GetVerbHash())

	replacement := &Spec{Name: "read", ArgTypes: map[string]string{"id": "string"}, Description: "v1"}
	strict = false
	replacement.StrictReturn = &strict
	replacementRegistry := NewRegistry(nil)
	require.NoError(t, replacementRegistry.RegisterVerb(replacement))
	require.NotEqual(t, first, replacementRegistry.GetVerbHash())
}

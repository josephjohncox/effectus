package tests

import (
	"context"
	"testing"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/stretchr/testify/assert"
)

type typeSystemSchema struct {
	ts *types.TypeSystem
}

func (s *typeSystemSchema) ValidatePath(path string) bool {
	if path == "" {
		return false
	}
	_, err := s.ts.GetFactType(path)
	return err == nil
}

type simpleFacts struct {
	data     map[string]interface{}
	provider *pathutil.RegistryFactProvider
	schema   effectus.SchemaInfo
}

func (f *simpleFacts) Get(path string) (interface{}, bool) {
	if path == "" {
		return f.data, true
	}
	return f.provider.Get(path)
}

func (f *simpleFacts) Schema() effectus.SchemaInfo {
	return f.schema
}

type captureExecutor struct {
	called bool
	args   map[string]interface{}
}

func (c *captureExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	c.called = true
	c.args = args
	return true, nil
}

func TestListRuleEndToEnd(t *testing.T) {
	ruleContent := `
rule "FlagLargeOrder" priority 10 {
	when {
		order.total > 500 && customer.vip == false
	}
	then {
		FlagReview(orderId: order.id, reason: "large order")
	}
}
`

	// Build facts and schema
	factsData := map[string]interface{}{
		"order": map[string]interface{}{
			"id":    "ORDER-1",
			"total": 750.0,
		},
		"customer": map[string]interface{}{
			"vip": false,
		},
	}

	provider := pathutil.NewRegistryFactProviderFromMap(factsData)
	typeSystem := types.NewTypeSystem()
	typeSystem.RegisterFactType("order.id", types.NewStringType())
	typeSystem.RegisterFactType("order.total", types.NewFloatType())
	typeSystem.RegisterFactType("customer.vip", types.NewBoolType())

	// Register verb types for type checking
	typeSystem.RegisterVerbType(
		"FlagReview",
		map[string]*types.Type{
			"orderId": types.NewStringType(),
			"reason":  types.NewStringType(),
		},
		types.NewBoolType(),
	)

	schemaInfo := &typeSystemSchema{ts: typeSystem}
	facts := &simpleFacts{data: factsData, provider: provider, schema: schemaInfo}

	// Parse + type check
	comp := compiler.NewCompiler()
	compTS := comp.GetTypeSystem()
	for _, path := range typeSystem.GetAllFactPaths() {
		factType, _ := typeSystem.GetFactType(path)
		compTS.RegisterFactType(path, factType)
	}
	compTS.RegisterVerbType(
		"FlagReview",
		map[string]*types.Type{
			"orderId": types.NewStringType(),
			"reason":  types.NewStringType(),
		},
		types.NewBoolType(),
	)

	tmpFile := createTempRuleFile(t, ruleContent)
	defer cleanupTempFile(tmpFile)

	parsed, err := comp.ParseAndTypeCheck(tmpFile, facts)
	assert.NoError(t, err)

	// Compile into list spec
	listCompiler := &list.Compiler{}
	specAny, err := listCompiler.CompileParsedFile(parsed, tmpFile, facts.Schema())
	assert.NoError(t, err)

	spec := specAny.(*list.Spec)

	// Register verb executor
	verbRegistry := verb.NewRegistry(nil)
	executor := &captureExecutor{}
	err = verbRegistry.RegisterVerb(&verb.Spec{
		Name:         "FlagReview",
		Description:  "Flags an order for review",
		Capability:   verb.CapWrite,
		ArgTypes:     map[string]string{"orderId": "string", "reason": "string"},
		RequiredArgs: []string{"orderId", "reason"},
		ReturnType:   "bool",
		Executor:     executor,
	})
	assert.NoError(t, err)

	spec.VerbRegistry = verbRegistry

	// Execute
	err = spec.Execute(context.Background(), facts, nil)
	assert.NoError(t, err)
	assert.True(t, executor.called)
	assert.Equal(t, "ORDER-1", executor.args["orderId"])
	assert.Equal(t, "large order", executor.args["reason"])
}

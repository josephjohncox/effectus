package compiler

import (
	"context"
	"fmt"
	"testing"

	"github.com/josephjohncox/effectus/loader"
	"github.com/stretchr/testify/require"
)

func TestExtensionCompilerLowersEmptyCandidateToCheckedIR(t *testing.T) {
	result, err := NewExtensionCompiler().Compile(t.Context(), loader.NewExtensionManager())
	require.NoError(t, err)
	require.True(t, result.Success)
	require.NotNil(t, result.CompiledUnit)
	require.NotNil(t, result.CompiledUnit.CheckedIR)
	require.Zero(t, result.CompiledUnit.CheckedIR.PlanCount())
}

func TestExtensionCompilerUsesCanonicalSourceCompiler(t *testing.T) {
	manager := loader.NewExtensionManager()
	manager.AddLoader(extensionSourceTestLoader{source: []byte(`
flow "charge-order" priority 10 {
  when {}
  steps {
    receipt = charge(order_id: "order-1", amount: 42)
    record(receipt: $receipt)
  }
}`)})
	result, err := NewExtensionCompiler().Compile(t.Context(), manager)
	require.NoError(t, err)
	require.True(t, result.Success, "%v", result.Errors)
	unit := result.CompiledUnit
	require.Equal(t, 1, unit.CheckedIR.PlanCount())
	require.Equal(t, 2, unit.CheckedIR.StepCount())
	require.Equal(t, "effectusc", unit.CheckedIR.CloneArtifact().Compiler.Name)
	for name, config := range unit.ExecutionPlan.Executors {
		require.Equal(t, ExecutorLocal, config.GetType(), "verb %s", name)
	}

	second, err := NewExtensionCompiler().Compile(t.Context(), manager)
	require.NoError(t, err)
	require.True(t, second.Success)
	require.Equal(t, unit.CheckedIR.Digest(), second.CompiledUnit.CheckedIR.Digest())
}

func TestExtensionCompilerRejectsCapabilitiesAndTypeConfusion(t *testing.T) {
	tests := []struct {
		name                 string
		capabilities         []string
		resourceCapabilities []string
		source               string
		contains             string
	}{
		{name: "unknown capability", capabilities: []string{"admin"}, source: validExtensionFlow(), contains: "unknown capability"},
		{name: "resource escalation", capabilities: []string{"read"}, resourceCapabilities: []string{"write"}, source: validExtensionFlow(), contains: "not declared by the verb"},
		{name: "literal mismatch", source: `flow "bad" priority 1 { when {} steps { charge(order_id: "id", amount: "bad") } }`, contains: "incompatible"},
		{name: "forward result", source: `flow "bad" priority 1 { when {} steps { record(receipt: $future) } }`, contains: "not available"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := loader.NewExtensionManager()
			manager.AddLoader(extensionSourceTestLoader{
				capabilities: test.capabilities, resourceCapabilities: test.resourceCapabilities, source: []byte(test.source),
			})
			result, err := NewExtensionCompiler().Compile(t.Context(), manager)
			require.NoError(t, err)
			require.False(t, result.Success)
			require.Contains(t, fmt.Sprint(result.Errors), test.contains)
		})
	}
}

func TestExtensionCompilerRejectsIncompatibleInverseContract(t *testing.T) {
	manager := loader.NewExtensionManager()
	manager.AddLoader(inverseContractLoader{})
	result, err := NewExtensionCompiler().Compile(t.Context(), manager)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Contains(t, fmt.Sprint(result.Errors), "incompatible argument")
}

type inverseContractLoader struct{}

func (inverseContractLoader) Name() string { return "inverse-contract" }
func (inverseContractLoader) Load(_ context.Context, target loader.LoadTarget) error {
	if err := target.RegisterVerb(&testVerbSpec{name: "forward", capabilities: []string{"write"}, args: map[string]string{"id": "string"}, required: []string{"id"}, result: "void", inverse: "inverse"}, testVerbExecutor{}); err != nil {
		return err
	}
	return target.RegisterVerb(&testVerbSpec{name: "inverse", capabilities: []string{"write"}, args: map[string]string{"other": "string"}, required: []string{"other"}, result: "void"}, testVerbExecutor{})
}

func TestExtensionCompilerRetainsInitialDataAndFunctions(t *testing.T) {
	manager := loader.NewExtensionManager()
	function := func(value int64) int64 { return value + 1 }
	manager.AddLoader(scalarGenerationLoader{function: function})
	result, err := NewExtensionCompiler().Compile(t.Context(), manager)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, int64(7), result.CompiledUnit.InitialData["config.value"])
	require.NotNil(t, result.CompiledUnit.Functions["increment"].Implementation)
}

type scalarGenerationLoader struct{ function interface{} }

func (loaderValue scalarGenerationLoader) Name() string { return "scalar-generation" }
func (loaderValue scalarGenerationLoader) Load(_ context.Context, target loader.LoadTarget) error {
	if err := target.RegisterFunction("increment", loaderValue.function); err != nil {
		return err
	}
	return target.LoadData("config.value", int64(7))
}

type extensionSourceTestLoader struct {
	source               []byte
	capabilities         []string
	resourceCapabilities []string
}

func (source extensionSourceTestLoader) Name() string { return "source-test" }
func (source extensionSourceTestLoader) Load(_ context.Context, target loader.LoadTarget) error {
	capabilities := source.capabilities
	if capabilities == nil {
		capabilities = []string{"write", "idempotent"}
	}
	resourceCapabilities := source.resourceCapabilities
	if resourceCapabilities == nil {
		resourceCapabilities = []string{"write"}
	}
	if err := target.RegisterVerb(&testVerbSpec{
		name: "charge", capabilities: capabilities,
		resources: []loader.ResourceSpec{testResource{resource: "payment", capabilities: resourceCapabilities}},
		args:      map[string]string{"amount": "int", "order_id": "string"}, required: []string{"amount", "order_id"}, result: "string",
	}, testVerbExecutor{}); err != nil {
		return err
	}
	if err := target.RegisterVerb(&testVerbSpec{
		name: "record", capabilities: []string{"write"}, args: map[string]string{"receipt": "string"}, required: []string{"receipt"}, result: "void",
	}, testVerbExecutor{}); err != nil {
		return err
	}
	sourceTarget, ok := target.(loader.SourceLoadTarget)
	if !ok {
		return fmt.Errorf("source target unavailable")
	}
	return sourceTarget.RegisterSource(loader.SourceFile{Path: "rules/workflow.effx", Data: source.source})
}

type testVerbSpec struct {
	name         string
	capabilities []string
	resources    []loader.ResourceSpec
	args         map[string]string
	required     []string
	result       string
	inverse      string
}

func (spec *testVerbSpec) GetName() string                     { return spec.name }
func (spec *testVerbSpec) GetDescription() string              { return "test" }
func (spec *testVerbSpec) GetCapabilities() []string           { return spec.capabilities }
func (spec *testVerbSpec) GetResources() []loader.ResourceSpec { return spec.resources }
func (spec *testVerbSpec) GetArgTypes() map[string]string      { return spec.args }
func (spec *testVerbSpec) GetRequiredArgs() []string           { return spec.required }
func (spec *testVerbSpec) GetReturnType() string               { return spec.result }
func (spec *testVerbSpec) GetInverseVerb() string              { return spec.inverse }

type testResource struct {
	resource     string
	capabilities []string
}

func (resource testResource) GetResource() string       { return resource.resource }
func (resource testResource) GetCapabilities() []string { return resource.capabilities }

type testVerbExecutor struct{}

func (testVerbExecutor) Execute(context.Context, map[string]interface{}) (interface{}, error) {
	return "ok", nil
}

func validExtensionFlow() string {
	return `flow "charge" priority 1 { when {} steps { charge(order_id: "id", amount: 1) } }`
}

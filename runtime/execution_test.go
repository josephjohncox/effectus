package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/schema"
	"github.com/stretchr/testify/require"
)

func TestInitialHotReloadPublishesCheckedEmptyGeneration(t *testing.T) {
	runtime := NewExecutionRuntime()
	require.NoError(t, runtime.HotReload(t.Context()))
	require.Equal(t, StateReady, runtime.GetRuntimeInfo().State)
	require.NotNil(t, runtime.compiledUnit)
	require.NotNil(t, runtime.compiledUnit.CheckedIR)
}

func TestExecuteWorkflowRequiresCheckedPlan(t *testing.T) {
	runtime := NewExecutionRuntime()
	runtime.state = StateReady
	require.NoError(t, runtime.ConfigureDurableWorkflowExecution(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "test"}))
	err := runtime.ExecuteWorkflowWithIdentity(t.Context(), "test", "execution", nil)
	require.ErrorContains(t, err, "no checked extension workflow")
	require.Equal(t, StateReady, runtime.state)
}

func TestExtensionHotReloadRollsBackRejectedCandidate(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "extension.verbs.json")
	sourcePath := filepath.Join(directory, "workflow.effx")
	require.NoError(t, os.WriteFile(path, []byte(validWorkflowManifest()), 0o600))
	require.NoError(t, os.WriteFile(sourcePath, []byte(validWorkflowSource("1")), 0o600))

	runtime := NewExecutionRuntime()
	runtime.EnableLegacyExecutionForCompatibility()
	runtime.RegisterExtensionLoader(loader.NewJSONVerbLoader("test", path))
	runtime.RegisterExtensionLoader(fileSourceLoader{path: sourcePath})
	require.NoError(t, runtime.CompileAndValidate(t.Context()))
	require.NoError(t, runtime.ConfigureDurableWorkflowExecution(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "test"}))
	first := runtime.compiledUnit
	firstDigest := first.CheckedIR.Digest()
	require.NoError(t, runtime.ExecuteWorkflowWithIdentity(t.Context(), "test", "execution-1", nil))

	require.NoError(t, os.WriteFile(path, []byte(invalidCapabilityManifest()), 0o600))
	require.Error(t, runtime.HotReload(t.Context()))
	require.Equal(t, StateReady, runtime.GetRuntimeInfo().State)
	require.Same(t, first, runtime.compiledUnit)
	require.Equal(t, firstDigest, runtime.compiledUnit.CheckedIR.Digest())
	require.Error(t, runtime.CompileAndValidate(t.Context()))
	require.Equal(t, StateReady, runtime.GetRuntimeInfo().State)
	require.Same(t, first, runtime.compiledUnit)

	require.NoError(t, os.WriteFile(path, []byte(validWorkflowManifest()), 0o600))
	require.NoError(t, os.WriteFile(sourcePath, []byte(validWorkflowSource("2")), 0o600))
	require.NoError(t, runtime.HotReload(t.Context()))
	require.NotSame(t, first, runtime.compiledUnit)
	require.NotEqual(t, firstDigest, runtime.compiledUnit.CheckedIR.Digest())
}

func TestCheckedWorkflowResolvesPublishedInitialData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extension.verbs.json")
	manifest := `{
  "name":"initial-data",
  "version":"1",
  "verbs":[{
    "name":"charge",
    "capabilities":["write"],
    "resources":[{"resource":"payment","capabilities":["write"]}],
    "argTypes":{"amount":"int"},
    "requiredArgs":["amount"],
    "returnType":"void",
    "target":{"type":"noop"}
  }]
}`
	require.NoError(t, os.WriteFile(path, []byte(manifest), 0o600))
	schemaPath := filepath.Join(t.TempDir(), "extension.schema.json")
	require.NoError(t, os.WriteFile(schemaPath, []byte(`{
  "name":"initial-data",
  "version":"1",
  "types":{},
  "functions":{},
  "initialData":{"config.value":7}
}`), 0o600))
	runtime := NewExecutionRuntime()
	runtime.EnableLegacyExecutionForCompatibility()
	runtime.RegisterExtensionLoader(loader.NewJSONSchemaLoader("data", schemaPath))
	runtime.RegisterExtensionLoader(loader.NewJSONVerbLoader("test", path))
	runtime.RegisterExtensionLoader(loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(`flow "charge-from-config" priority 1 { when {} steps { charge(amount: config.value) } }`)))
	require.NoError(t, runtime.CompileAndValidate(t.Context()))
	require.Equal(t, json.Number("7"), runtime.compiledUnit.InitialData["config.value"])
	require.NoError(t, runtime.ConfigureDurableWorkflowExecution(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "test"}))
	require.NoError(t, runtime.ExecuteWorkflowWithIdentity(t.Context(), "test", "initial-data-execution", nil))
}

func validWorkflowManifest() string {
	return `{
  "name":"test",
  "version":"1",
  "verbs":[{
    "name":"charge",
    "capabilities":["write"],
    "resources":[{"resource":"payment","capabilities":["write"]}],
    "argTypes":{"amount":"int"},
    "requiredArgs":["amount"],
    "returnType":"void",
    "target":{"type":"noop"}
  }]
}`
}

func validWorkflowSource(amount string) string {
	return `flow "charge" priority 1 { when {} steps { charge(amount: ` + amount + `) } }`
}

func invalidCapabilityManifest() string {
	return `{
  "name":"test",
  "version":"2",
  "verbs":[{
    "name":"charge",
    "capabilities":["root"],
    "argTypes":{"amount":"int"},
    "requiredArgs":["amount"],
    "returnType":"void",
    "target":{"type":"noop"}
  }]
}`
}

type fileSourceLoader struct{ path string }

func (source fileSourceLoader) Name() string { return "file-source" }
func (source fileSourceLoader) Load(_ context.Context, target loader.LoadTarget) error {
	data, err := os.ReadFile(source.path)
	if err != nil {
		return err
	}
	sourceTarget, ok := target.(loader.SourceLoadTarget)
	if !ok {
		return os.ErrInvalid
	}
	return sourceTarget.RegisterSource(loader.SourceFile{Path: filepath.Base(source.path), Data: data})
}

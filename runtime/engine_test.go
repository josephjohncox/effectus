package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/schema"
	"github.com/stretchr/testify/require"
)

func TestEngineExecuteAcceptedResumeAndIdentityConflict(t *testing.T) {
	runtime := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	request := ExecuteRequest{Admission: &Admission{
		ExecutionID: "execution-1", AdmissionID: "delivery-1", TenantNamespace: "tenant", Ruleset: "orders", Version: "1",
		Facts: map[string]any{"order": map[string]any{"id": "42"}},
	}, WaitMode: WaitAccepted}
	accepted, err := runtime.Engine().Execute(t.Context(), request)
	require.NoError(t, err)
	require.True(t, accepted.DurablyAccepted)
	require.False(t, accepted.Completed)

	completed, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "execution-1", WaitMode: WaitTerminal})
	require.NoError(t, err)
	require.True(t, completed.Completed)
	require.Equal(t, accepted.GenerationDigest, completed.GenerationDigest)

	replay := request
	replay.Admission = &Admission{
		ExecutionID: "execution-1", AdmissionID: "delivery-1", TenantNamespace: "tenant", Ruleset: "orders", Version: "1",
		Facts: map[string]any{"order": map[string]any{"id": "42"}},
	}
	_, err = runtime.Engine().Execute(t.Context(), replay)
	require.NoError(t, err)

	conflict := replay
	conflict.Admission = &Admission{
		ExecutionID: "execution-1", TenantNamespace: "tenant", Ruleset: "orders", Version: "1",
		Facts: map[string]any{"order": map[string]any{"id": "different"}},
	}
	_, err = runtime.Engine().Execute(t.Context(), conflict)
	require.ErrorIs(t, err, ErrIdentityConflict)
}

func TestEngineCompletesEmptySelectedPlanAtAdmission(t *testing.T) {
	runtime := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(`flow "empty" priority 1 { when {} steps {} }`)))
	result, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{
		ExecutionID: "empty", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "one"},
	}, WaitMode: WaitAccepted})
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.Equal(t, string(schema.ExecutionCompleted), result.State)
}

func TestEngineRejectsLegacyContinuationInProductionMode(t *testing.T) {
	runtime := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	runtime.mu.Lock()
	runtime.allowLegacyExecution = false
	runtime.mu.Unlock()
	_, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{
		ExecutionID: "production", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "one"},
	}, WaitMode: WaitAccepted})
	require.ErrorContains(t, err, "unrestricted Go continuation")
}

func TestEnginePinsGenerationAcrossHotReload(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "workflow.effx")
	require.NoError(t, os.WriteFile(sourcePath, []byte(validWorkflowSource("1")), 0o600))
	runtime := newEngineTestRuntime(t, fileSourceLoader{path: sourcePath})
	first, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{
		ExecutionID: "old", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "one"},
	}, WaitMode: WaitAccepted})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(sourcePath, []byte(validWorkflowSource("2")), 0o600))
	require.NoError(t, runtime.HotReload(t.Context()))
	second, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{
		ExecutionID: "new", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "two"},
	}, WaitMode: WaitAccepted})
	require.NoError(t, err)
	require.NotEqual(t, first.GenerationDigest, second.GenerationDigest)
	replayed, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{
		ExecutionID: "old", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "one"},
	}, WaitMode: WaitAccepted})
	require.NoError(t, err)
	require.Equal(t, first.GenerationDigest, replayed.GenerationDigest, "delivery replay must retain the admitted generation")

	resumed, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "old", WaitMode: WaitTerminal})
	require.NoError(t, err)
	require.Equal(t, first.GenerationDigest, resumed.GenerationDigest)
}

func newEngineTestRuntime(t *testing.T, source loader.Loader) *ExecutionRuntime {
	t.Helper()
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "extension.verbs.json")
	require.NoError(t, os.WriteFile(manifestPath, []byte(validWorkflowManifest()), 0o600))
	runtime := NewExecutionRuntime()
	runtime.EnableLegacyExecutionForCompatibility()
	runtime.RegisterExtensionLoader(loader.NewJSONVerbLoader("test", manifestPath))
	runtime.RegisterExtensionLoader(source)
	require.NoError(t, runtime.CompileAndValidate(t.Context()))
	require.NoError(t, runtime.ConfigureDurableWorkflowExecution(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "engine-test"}))
	return runtime
}

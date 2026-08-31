package runtime

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/josephjohncox/effectus/loader"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

type recordingRuntimeObserver struct{ executions atomic.Int32 }

func (observer *recordingRuntimeObserver) ObserveExecution(ExecuteResult, error) {
	observer.executions.Add(1)
}
func (*recordingRuntimeObserver) ObserveRecovery(RecoveryObservation) {}

func TestLoadExecutionRecordLocksCachedGenerationIdentity(t *testing.T) {
	engine := &Engine{executions: map[string]*engineExecution{}}
	execution := &engineExecution{record: schema.ExecutionRecord{ExecutionID: "same", GenerationDigest: "generation"}}
	engine.executions["same"] = execution
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for index := 0; index < 1000; index++ {
			execution.mu.Lock()
			execution.record.GenerationDigest = "generation"
			execution.mu.Unlock()
		}
	}()
	go func() {
		defer workers.Done()
		for index := 0; index < 1000; index++ {
			loaded, err := engine.loadExecutionRecord(t.Context(), schema.ExecutionRecord{ExecutionID: "same", GenerationDigest: "generation"}, nil)
			require.NoError(t, err)
			require.Same(t, execution, loaded)
		}
	}()
	workers.Wait()
}

func TestEngineObserverReceivesCheckedExecution(t *testing.T) {
	runtime := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	observer := new(recordingRuntimeObserver)
	runtime.Engine().SetObserver(observer)
	_, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: "observed", AdmissionID: "observed-delivery", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "one"}}, WaitMode: WaitAccepted})
	require.NoError(t, err)
	require.Equal(t, int32(1), observer.executions.Load())
}

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

package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/josephjohncox/effectus/internal/loader"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

type reloadSnapshotLoader struct {
	mu        sync.Mutex
	source    []byte
	executors []*reloadSnapshotExecutor
}
type reloadSnapshotExecutor struct {
	closed   atomic.Int32
	closeErr error
}

func (executor *reloadSnapshotExecutor) Execute(context.Context, map[string]any) (any, error) {
	return nil, nil
}
func (executor *reloadSnapshotExecutor) Close() error {
	executor.closed.Add(1)
	return executor.closeErr
}
func (extension *reloadSnapshotLoader) Name() string { return "reload-snapshot" }
func (extension *reloadSnapshotLoader) Load(_ context.Context, target loader.LoadTarget) error {
	extension.mu.Lock()
	defer extension.mu.Unlock()
	executor := new(reloadSnapshotExecutor)
	extension.executors = append(extension.executors, executor)
	if err := target.RegisterVerb(reloadSnapshotVerbSpec{}, executor); err != nil {
		return err
	}
	return target.(loader.SourceLoadTarget).RegisterSource(loader.SourceFile{Path: "workflow.effx", Data: append([]byte(nil), extension.source...)})
}

type reloadSnapshotVerbSpec struct{}

func (reloadSnapshotVerbSpec) GetName() string                     { return "charge" }
func (reloadSnapshotVerbSpec) GetDescription() string              { return "" }
func (reloadSnapshotVerbSpec) GetCapabilities() []string           { return []string{"write"} }
func (reloadSnapshotVerbSpec) GetResources() []loader.ResourceSpec { return nil }
func (reloadSnapshotVerbSpec) GetArgTypes() map[string]string {
	return map[string]string{"amount": "int"}
}
func (reloadSnapshotVerbSpec) GetRequiredArgs() []string { return []string{"amount"} }
func (reloadSnapshotVerbSpec) GetReturnType() string     { return "void" }
func (reloadSnapshotVerbSpec) GetInverseVerb() string    { return "" }

func TestRetiredExtensionSnapshotWaitsForAcceptedExecution(t *testing.T) {
	extension := &reloadSnapshotLoader{source: []byte(validWorkflowSource("1"))}
	runtime := NewExecutionRuntime()
	runtime.EnableLegacyExecutionForCompatibility()
	runtime.RegisterExtensionLoader(extension)
	require.NoError(t, runtime.CompileAndValidate(t.Context()))
	require.NoError(t, runtime.ConfigureDurableWorkflowExecution(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "snapshot-test"}))
	_, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: "old", AdmissionID: "delivery", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "42"}}, WaitMode: WaitAccepted})
	require.NoError(t, err)
	extension.mu.Lock()
	extension.source = []byte(validWorkflowSource("2"))
	extension.mu.Unlock()
	require.NoError(t, runtime.HotReload(t.Context()))
	extension.mu.Lock()
	first := extension.executors[0]
	extension.mu.Unlock()
	require.Zero(t, first.closed.Load())
	_, err = runtime.Engine().Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "old", WaitMode: WaitTerminal})
	require.NoError(t, err)
	require.Equal(t, int32(1), first.closed.Load())
}

func TestHotReloadPublicationSurvivesPreviousCloserFailure(t *testing.T) {
	extension := &reloadSnapshotLoader{source: []byte(validWorkflowSource("1"))}
	runtime := NewExecutionRuntime()
	runtime.EnableLegacyExecutionForCompatibility()
	runtime.RegisterExtensionLoader(extension)
	require.NoError(t, runtime.CompileAndValidate(t.Context()))
	extension.mu.Lock()
	first := extension.executors[0]
	first.closeErr = context.DeadlineExceeded
	extension.source = []byte(validWorkflowSource("2"))
	extension.mu.Unlock()

	require.NoError(t, runtime.HotReload(t.Context()), "cleanup failure cannot reject a committed publication")
	require.Equal(t, StateReady, runtime.GetRuntimeInfo().State)
	snapshot, err := extensionSnapshot(runtime.activeGeneration.unit)
	require.NoError(t, err)
	require.False(t, snapshot.Closed())
	handle, err := snapshot.Acquire()
	require.NoError(t, err, "installed candidate must remain acquirable")
	require.NoError(t, handle.Release())
	require.Equal(t, int32(1), first.closed.Load())
}

func TestCloseReleasesAcceptedExecutionSnapshot(t *testing.T) {
	extension := &reloadSnapshotLoader{source: []byte(validWorkflowSource("1"))}
	runtime := NewExecutionRuntime()
	runtime.EnableLegacyExecutionForCompatibility()
	runtime.RegisterExtensionLoader(extension)
	require.NoError(t, runtime.CompileAndValidate(t.Context()))
	require.NoError(t, runtime.ConfigureDurableWorkflowExecution(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "close-test"}))
	_, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: "accepted", AdmissionID: "accepted-delivery", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "42"}}, WaitMode: WaitAccepted})
	require.NoError(t, err)
	extension.mu.Lock()
	executor := extension.executors[0]
	extension.mu.Unlock()
	require.Zero(t, executor.closed.Load())
	require.NoError(t, runtime.Close())
	require.NoError(t, runtime.Close())
	require.Equal(t, int32(1), executor.closed.Load())
	_, err = runtime.Engine().Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "accepted", WaitMode: WaitTerminal})
	require.ErrorContains(t, err, "closed")
}

func TestFailedExtensionReloadCleansCandidateAndKeepsActiveSnapshot(t *testing.T) {
	extension := &reloadSnapshotLoader{source: []byte(validWorkflowSource("1"))}
	runtime := NewExecutionRuntime()
	runtime.EnableLegacyExecutionForCompatibility()
	runtime.RegisterExtensionLoader(extension)
	require.NoError(t, runtime.CompileAndValidate(t.Context()))
	active := runtime.activeGeneration
	extension.mu.Lock()
	extension.source = []byte(`flow "broken" priority 1 { when {`)
	extension.mu.Unlock()
	require.Error(t, runtime.HotReload(t.Context()))
	require.Same(t, active, runtime.activeGeneration)
	extension.mu.Lock()
	require.Len(t, extension.executors, 2)
	first, second := extension.executors[0], extension.executors[1]
	extension.mu.Unlock()
	require.Zero(t, first.closed.Load())
	require.Equal(t, int32(1), second.closed.Load())
	require.NoError(t, runtime.Close())
	require.Equal(t, int32(1), first.closed.Load())
}

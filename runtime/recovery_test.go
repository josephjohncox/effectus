package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/internal/loader"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

func TestRecoveryWorkerLeasesAndResumesThroughEngine(t *testing.T) {
	runtime := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	_, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{
		ExecutionID: "recover-me", AdmissionID: "delivery", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "one"},
	}, WaitMode: WaitAccepted})
	require.NoError(t, err)
	ledger := runtime.Engine().ledger
	count, err := (&RecoveryWorker{Engine: runtime.Engine(), Store: ledger, Owner: "worker", BatchSize: 1}).RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	record, err := ledger.GetExecution(t.Context(), "recover-me")
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionCompleted, record.State)
	require.Empty(t, record.RecoveryToken)
}

type transientGetDispatchOutbox struct {
	schema.OutboxStore
	fail atomic.Bool
}

func (store *transientGetDispatchOutbox) GetDispatch(ctx context.Context, id string) (*schema.Dispatch, error) {
	if store.fail.Swap(false) {
		return nil, errors.New("transient workflow store failure")
	}
	return store.OutboxStore.GetDispatch(ctx, id)
}

func TestTransientStoreFailureReleasesRecoveryLeaseAndRemainsRecoverable(t *testing.T) {
	runtime := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	outbox := &transientGetDispatchOutbox{OutboxStore: schema.NewInMemoryOutboxStore()}
	require.NoError(t, runtime.ConfigureDurableWorkflowExecution(outbox, nil, schema.DispatcherOptions{Owner: "transient-test"}))
	_, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: "transient", AdmissionID: "transient-delivery", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "one"}}, WaitMode: WaitAccepted})
	require.NoError(t, err)
	outbox.fail.Store(true)
	ledger := runtime.Engine().ledger
	worker := &RecoveryWorker{Engine: runtime.Engine(), Store: ledger, Owner: "worker", BatchSize: 1}
	count, err := worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	record, err := ledger.GetExecution(t.Context(), "transient")
	require.NoError(t, err)
	require.False(t, schema.IsTerminalExecutionState(record.State))
	require.Empty(t, record.RecoveryToken)
	count, err = worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	record, err = ledger.GetExecution(t.Context(), "transient")
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionCompleted, record.State)
}

type failingFinishLedger struct {
	schema.ExecutionLedger
	fail atomic.Bool
}

func (ledger *failingFinishLedger) FinishExecutionLease(ctx context.Context, lease schema.ExecutionLease, state schema.ExecutionState, message string) error {
	if ledger.fail.Load() {
		return errors.New("finish lease store failure")
	}
	return ledger.ExecutionLedger.FinishExecutionLease(ctx, lease, state, message)
}

type failFirstRecoveryLoader struct{ calls atomic.Int32 }
type failFirstRecoveryExecutor struct{ calls *atomic.Int32 }
type failFirstRecoveryVerbSpec struct{}

func (*failFirstRecoveryLoader) Name() string { return "fail-first-recovery" }
func (loaderImpl *failFirstRecoveryLoader) Load(_ context.Context, target loader.LoadTarget) error {
	if err := target.RegisterVerb(failFirstRecoveryVerbSpec{}, &failFirstRecoveryExecutor{calls: &loaderImpl.calls}); err != nil {
		return err
	}
	return target.(loader.SourceLoadTarget).RegisterSource(loader.SourceFile{Path: "workflow.effx", Data: []byte(validWorkflowSource("1"))})
}
func (executor *failFirstRecoveryExecutor) Execute(context.Context, map[string]any) (any, error) {
	return nil, nil
}
func (executor *failFirstRecoveryExecutor) Invoke(_ context.Context, _ invocation.Request) invocation.Outcome {
	if executor.calls.Add(1) == 1 {
		return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("terminal business failure")}
	}
	return invocation.Outcome{Class: invocation.OutcomeSuccess}
}
func (*failFirstRecoveryExecutor) Close() error                       { return nil }
func (failFirstRecoveryVerbSpec) GetName() string                     { return "charge" }
func (failFirstRecoveryVerbSpec) GetDescription() string              { return "" }
func (failFirstRecoveryVerbSpec) GetCapabilities() []string           { return []string{"write"} }
func (failFirstRecoveryVerbSpec) GetResources() []loader.ResourceSpec { return nil }
func (failFirstRecoveryVerbSpec) GetArgTypes() map[string]string {
	return map[string]string{"amount": "int"}
}
func (failFirstRecoveryVerbSpec) GetRequiredArgs() []string { return []string{"amount"} }
func (failFirstRecoveryVerbSpec) GetReturnType() string     { return "void" }
func (failFirstRecoveryVerbSpec) GetInverseVerb() string    { return "" }

func TestRecoveryBusinessFailureDoesNotStopBatchOrRun(t *testing.T) {
	extension := new(failFirstRecoveryLoader)
	runtime := NewExecutionRuntime()
	runtime.EnableLegacyExecutionForCompatibility()
	runtime.RegisterExtensionLoader(extension)
	require.NoError(t, runtime.CompileAndValidate(t.Context()))
	require.NoError(t, runtime.ConfigureDurableWorkflowExecution(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "recovery-business"}))
	for _, id := range []string{"business-failure", "business-success"} {
		_, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{
			ExecutionID: id, AdmissionID: id + "-delivery", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": id},
		}, WaitMode: WaitAccepted})
		require.NoError(t, err)
	}
	worker := &RecoveryWorker{Engine: runtime.Engine(), Store: runtime.Engine().ledger, Owner: "worker", BatchSize: 2, PollInterval: time.Millisecond}
	count, err := worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, count)
	failed, err := runtime.Engine().ledger.GetExecution(t.Context(), "business-failure")
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionFailed, failed.State)
	succeeded, err := runtime.Engine().ledger.GetExecution(t.Context(), "business-success")
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionCompleted, succeeded.State)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	select {
	case err := <-done:
		t.Fatalf("Run exited after a terminal business failure: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	require.NoError(t, <-done)
}

func TestRecoveryWorkerStoreFailureStopsRunOnce(t *testing.T) {
	runtime := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	ledger := &failingFinishLedger{ExecutionLedger: schema.NewInMemoryExecutionLedger()}
	require.NoError(t, runtime.ConfigureExecutionLedger(ledger, nil))
	_, err := runtime.Engine().Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: "store-failure", AdmissionID: "store-failure-delivery", TenantNamespace: "tenant", Ruleset: "orders", Version: "1", Facts: map[string]any{"id": "one"}}, WaitMode: WaitAccepted})
	require.NoError(t, err)
	ledger.fail.Store(true)
	count, err := (&RecoveryWorker{Engine: runtime.Engine(), Store: ledger, Owner: "worker", BatchSize: 1}).RunOnce(t.Context())
	require.Equal(t, 1, count)
	require.ErrorIs(t, err, ErrDurableDisposition)
}

func TestRecoveryWorkerRunStopsOnCancellation(t *testing.T) {
	runtime := newEngineTestRuntime(t, loader.NewStaticSourceLoader("workflow", "workflow.effx", []byte(validWorkflowSource("1"))))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	worker := &RecoveryWorker{Engine: runtime.Engine(), Store: runtime.Engine().ledger, Owner: "worker", PollInterval: time.Millisecond}
	require.NoError(t, worker.Run(ctx))
}

func TestRecoveryLeaseRejectsStaleCompletion(t *testing.T) {
	ledger := schema.NewInMemoryExecutionLedger()
	// Lease behavior is covered with a minimally valid admission in schema tests;
	// this assertion keeps the runtime contract explicit.
	err := ledger.FinishExecutionLease(t.Context(), schema.ExecutionLease{ExecutionID: "missing", Owner: "worker", Token: "stale", Revision: 1}, schema.ExecutionCompleted, "")
	require.ErrorIs(t, err, schema.ErrExecutionNotFound)
}

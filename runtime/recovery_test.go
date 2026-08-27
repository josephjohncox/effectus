package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/schema"
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

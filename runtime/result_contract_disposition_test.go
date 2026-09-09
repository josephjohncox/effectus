package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/ledger"
	"github.com/stretchr/testify/require"
)

type failingResultDispositionLedger struct {
	ledger.ExecutionLedger
	failure error
}

func (store failingResultDispositionLedger) SetExecutionState(ctx context.Context, id string, revision uint64, state ledger.ExecutionState, message string) (ledger.ExecutionRecord, error) {
	if state == ledger.ExecutionBlockedDependency {
		return ledger.ExecutionRecord{}, store.failure
	}
	return store.ExecutionLedger.SetExecutionState(ctx, id, revision, state, message)
}

func TestInvalidCheckedResultRecoveryAfterDispositionWriteFailure(t *testing.T) {
	environment := ir.Environment{Verbs: map[string]ir.VerbContract{
		"Produce": {ResultType: "list<int>"}, "Consume": {Arguments: map[string]string{"values": "list<int>"}, ResultType: "void"},
	}}
	checked := compileLanguage(t, environment, `flow "result" priority 1 { when {} steps { values = Produce() Consume(values: $values) } }`, "effx")
	var calls atomic.Int64
	executor := languageExecutor(func(_ context.Context, request invocation.Request) invocation.Outcome {
		calls.Add(1)
		require.Equal(t, "Produce", request.Verb)
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: []any{1, "bad"}}
	})
	executors := map[string]invocation.Executor{"Produce": executor, "Consume": executor}
	durable, outbox := schema.NewInMemoryExecutionLedger(), schema.NewInMemoryOutboxStore()
	failure := errors.New("injected blocked-result disposition write failure")
	failing := failingResultDispositionLedger{ExecutionLedger: durable, failure: failure}
	engine := languageEngine(t, languageGeneration(t, environment, checked, executors), outbox, failing)
	_, err := engine.Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: "result-write-failure", TenantNamespace: "test", Ruleset: "language", Version: "1"}, WaitMode: WaitTerminal})
	require.ErrorIs(t, err, failure)
	require.ErrorIs(t, err, ErrDurableDisposition)
	require.NotErrorIs(t, err, ErrTerminalExecution, "a failed write must not claim a durable terminal disposition")
	record, err := durable.GetExecution(t.Context(), "result-write-failure")
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionRunning, record.State)
	require.Len(t, record.Plans, 1)
	dispatches, err := outbox.ListDispatches(t.Context(), record.Plans[0].SagaID)
	require.NoError(t, err)
	require.Len(t, dispatches, 1)
	require.Equal(t, schema.DispatchSucceeded, dispatches[0].State)
	require.Equal(t, `[1,"bad"]`, string(dispatches[0].Result))
	require.NoError(t, engine.Close())

	restarted := languageEngine(t, languageGeneration(t, environment, checked, executors), outbox, durable)
	worker := &RecoveryWorker{Engine: restarted, Store: durable, Owner: "result-contract-recovery", BatchSize: 2}
	processed, err := worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, processed, "recovery must block once, not repeatedly lease the same invalid result")
	record, err = durable.GetExecution(t.Context(), record.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionBlockedDependency, record.State)
	require.Empty(t, record.RecoveryToken)
	require.Empty(t, record.RecoveryOwner)
	require.True(t, record.RecoveryDeadline.IsZero())
	processed, err = worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Zero(t, processed)
	_, err = restarted.Execute(t.Context(), ExecuteRequest{ResumeExecutionID: record.ExecutionID, WaitMode: WaitTerminal})
	var terminal *TerminalExecutionError
	require.ErrorAs(t, err, &terminal)
	require.Equal(t, schema.ExecutionBlockedDependency, terminal.State)
	require.Equal(t, int64(1), calls.Load())
	unchanged, err := outbox.GetDispatch(t.Context(), dispatches[0].ID)
	require.NoError(t, err)
	require.Equal(t, dispatches[0], unchanged)
	attempts, err := outbox.ListAttempts(t.Context(), unchanged.ID)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, invocation.OutcomeSuccess, attempts[0].Outcome)
}

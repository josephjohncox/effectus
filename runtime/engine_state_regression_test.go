package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/compiler"
	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

func TestEngineCompensationFailureFirstExecutionAndReplay(t *testing.T) {
	env := ir.Environment{Verbs: map[string]ir.VerbContract{
		"Charge":     {ResultType: "bool", InverseVerb: "UndoCharge"},
		"Fail":       {ResultType: "bool", InverseVerb: "UndoFail"},
		"UndoCharge": {ResultType: "bool"}, "UndoFail": {ResultType: "bool"},
	}}
	source, err := bundle.New(bundle.Spec{Name: "language", Version: "1", Environment: env, Sources: []bundle.Source{{Path: "compensate.eff", Content: "rule \"compensate\" priority 1 { when { true } then {\nCharge()\nFail()\n} }"}}})
	require.NoError(t, err)
	checked, err := compiler.CompileChecked(t.Context(), source, compiler.CompileOptions{ExecutionPolicy: effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_COMPENSATING})
	require.NoError(t, err)
	var calls []string
	executor := recoveryExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
		calls = append(calls, request.Verb)
		if request.Verb == "Charge" {
			return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
		}
		return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("cannot apply or compensate")}
	})
	executors := map[string]invocation.Executor{}
	for verb := range env.Verbs {
		executors[verb] = executor
	}
	engine := languageEngine(t, languageGeneration(t, env, checked, executors), schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger())
	admission := &Admission{ExecutionID: "compensated", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
	for range 2 {
		result, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitTerminal})
		var terminal *TerminalExecutionError
		require.ErrorAs(t, err, &terminal)
		require.Equal(t, schema.ExecutionBlockedCompensation, terminal.State)
		require.Equal(t, string(schema.ExecutionBlockedCompensation), result.State)
	}
	_, err = engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitAccepted})
	require.NoError(t, err)
	require.Equal(t, []string{"Charge", "Fail", "UndoCharge"}, calls)
}

type contendedExecutionLedger struct {
	*schema.InMemoryExecutionLedger
	calls int
	final string
}

func (store *contendedExecutionLedger) SetExecutionState(ctx context.Context, id string, revision uint64, state schema.ExecutionState, message string) (schema.ExecutionRecord, error) {
	store.calls++
	if store.calls == 3 {
		switch store.final {
		case "owner":
			if _, err := store.InMemoryExecutionLedger.LeaseExecutions(ctx, "competitor", 1, time.Minute); err != nil {
				return schema.ExecutionRecord{}, err
			}
		case "terminal":
			if _, err := store.InMemoryExecutionLedger.SetExecutionState(ctx, id, revision, schema.ExecutionCompleted, ""); err != nil {
				return schema.ExecutionRecord{}, err
			}
		}
	}
	return schema.ExecutionRecord{}, schema.ErrOptimisticConflict
}

func TestEngineOptimisticConflictBoundAndFinalRefresh(t *testing.T) {
	for _, final := range []string{"contended", "owner", "terminal"} {
		t.Run(final, func(t *testing.T) {
			store := &contendedExecutionLedger{InMemoryExecutionLedger: schema.NewInMemoryExecutionLedger(), final: final}
			calls := 0
			engine := recoveryFixture(t, store, 1, recoveryExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
				calls++
				return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
			}))
			result, err := engine.Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "item-0", WaitMode: WaitTerminal})
			switch final {
			case "contended":
				require.ErrorIs(t, err, schema.ErrOptimisticConflict)
			case "owner":
				require.ErrorIs(t, err, ErrExecutionBusy)
			case "terminal":
				require.NoError(t, err)
				require.True(t, result.Completed)
			}
			require.Equal(t, 3, store.calls)
			require.Zero(t, calls)
		})
	}
}

func TestEngineRejectsUnsupportedRecoveryLeaseRequests(t *testing.T) {
	store := &countedAdmissionLedger{InMemoryExecutionLedger: schema.NewInMemoryExecutionLedger()}
	engine := recoveryFixture(t, store, 0, recoveryTestExecutor{})
	for _, request := range []ExecuteRequest{
		{Admission: &Admission{ExecutionID: "id", TenantNamespace: "tenant", Ruleset: "language", Version: "1"}, RecoveryLease: &schema.ExecutionLease{ExecutionID: "id"}},
		{ResumeExecutionID: "id", WaitMode: WaitAccepted, RecoveryLease: &schema.ExecutionLease{ExecutionID: "id"}},
		{ResumeExecutionID: "id", WaitMode: WaitTerminal, RecoveryLease: &schema.ExecutionLease{ExecutionID: "other"}},
	} {
		_, err := engine.Execute(t.Context(), request)
		require.ErrorIs(t, err, ErrInvalidExecuteRequest)
	}
	require.Zero(t, store.writes.Load())
	require.NoError(t, engine.ConfigureLedger(store, nil), "invalid request shapes must not start execution")
}

func TestEngineCloseErrorRetentionIsBounded(t *testing.T) {
	engine, err := NewEngine(lifetimeGeneration(t, "1", recoveryTestExecutor{}))
	require.NoError(t, err)
	first, second := errors.New("first close failure"), errors.New("later close failure")
	engine.recordCloseError(first)
	for range 100 {
		engine.recordCloseError(second)
	}
	require.ErrorIs(t, engine.Close(), first)
	require.NotErrorIs(t, engine.Close(), second)
	require.Equal(t, first.Error(), engine.Close().Error())
}

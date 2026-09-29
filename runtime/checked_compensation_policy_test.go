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

func compileCompensatingPolicyTest(t *testing.T, env ir.Environment, source string) *ir.Checked {
	t.Helper()
	b, err := bundle.New(bundle.Spec{Name: "language", Version: "1", Environment: env, Sources: []bundle.Source{{Path: "compensation.eff", Content: source}}})
	require.NoError(t, err)
	checked, err := compiler.CompileChecked(t.Context(), b, compiler.CompileOptions{ExecutionPolicy: effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_COMPENSATING})
	require.NoError(t, err)
	return checked
}

func TestCheckedCompensationUsesEachInverseRetryContractOnRecovery(t *testing.T) {
	env := ir.Environment{Verbs: map[string]ir.VerbContract{
		"Charge":      {ResultType: "bool", InverseVerb: "UndoCharge"},
		"Reserve":     {ResultType: "bool", InverseVerb: "UndoReserve"},
		"Fail":        {ResultType: "bool", InverseVerb: "UndoFail", RetryPolicy: ir.RetryPolicy{MaxAttempts: 1}},
		"UndoCharge":  {ResultType: "bool", RetryPolicy: ir.RetryPolicy{MaxAttempts: 3, InitialBackoffMillis: 1, MaxBackoffMillis: 1}},
		"UndoReserve": {ResultType: "bool", RetryPolicy: ir.RetryPolicy{MaxAttempts: 2, InitialBackoffMillis: 1, MaxBackoffMillis: 1}},
		"UndoFail":    {ResultType: "bool"},
	}}
	checked := compileCompensatingPolicyTest(t, env, "rule \"compensate\" priority 1 { when { true } then {\nCharge()\nReserve()\nFail()\n} }")
	var calls []string
	attempts := map[string]int{}
	executor := recoveryExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
		calls = append(calls, request.Verb)
		attempts[request.Verb]++
		switch request.Verb {
		case "Fail":
			return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("forward step failed")}
		case "UndoReserve", "UndoCharge":
			if attempts[request.Verb] < int(env.Verbs[request.Verb].RetryPolicy.MaxAttempts) {
				return invocation.Outcome{Class: invocation.OutcomeRetryableKnownNotCommitted, Err: errors.New("inverse was not committed")}
			}
		}
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	})
	executors := map[string]invocation.Executor{}
	for verb := range env.Verbs {
		executors[verb] = executor
	}
	store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, languageGeneration(t, env, checked, executors), store, ledger)
	admission := &Admission{ExecutionID: "inverse-retry-contracts", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
	_, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitAccepted})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	worker := &RecoveryWorker{Engine: engine, Store: ledger, Owner: "inverse-retry-recovery", BatchSize: 1, LeaseDuration: time.Second}
	processed, err := worker.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.NoError(t, ctx.Err())
	require.Equal(t, []string{"Charge", "Reserve", "Fail", "UndoReserve", "UndoReserve", "UndoCharge", "UndoCharge", "UndoCharge"}, calls)
	sagaID := schema.StableSagaID(admission.ExecutionID, "compensate")
	saga, err := store.GetSaga(ctx, sagaID)
	require.NoError(t, err)
	require.Equal(t, schema.SagaCompensated, saga.State)
	record, err := ledger.GetExecution(ctx, admission.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionFailed, record.State)
	dispatches, err := store.ListDispatches(ctx, sagaID)
	require.NoError(t, err)
	for verb, wantAttempts := range map[string]uint64{"UndoReserve": 2, "UndoCharge": 3} {
		contractHash, err := ir.ContractHash(env.Verbs[verb])
		require.NoError(t, err)
		found := false
		for _, dispatch := range dispatches {
			if dispatch.Verb != verb || dispatch.Direction != invocation.DirectionCompensation {
				continue
			}
			found = true
			require.Equal(t, schema.DispatchSucceeded, dispatch.State)
			require.Equal(t, wantAttempts, dispatch.Attempt)
			require.Equal(t, contractHash, dispatch.ContractHash)
		}
		require.True(t, found, "missing inverse dispatch %s", verb)
	}
	_, replayErr := engine.Execute(ctx, ExecuteRequest{Admission: admission, WaitMode: WaitTerminal})
	var terminal *TerminalExecutionError
	require.ErrorAs(t, replayErr, &terminal)
	require.Equal(t, schema.ExecutionFailed, terminal.State)
	require.Len(t, calls, 8, "terminal replay must not invoke an inverse again")
}

func TestCheckedCompensationDoesNotExceedInverseAttemptCap(t *testing.T) {
	env := ir.Environment{Verbs: map[string]ir.VerbContract{
		"Charge":     {ResultType: "bool", InverseVerb: "UndoCharge"},
		"Fail":       {ResultType: "bool", InverseVerb: "UndoFail", RetryPolicy: ir.RetryPolicy{MaxAttempts: 4}},
		"UndoCharge": {ResultType: "bool", RetryPolicy: ir.RetryPolicy{MaxAttempts: 1}},
		"UndoFail":   {ResultType: "bool"},
	}}
	checked := compileCompensatingPolicyTest(t, env, "rule \"compensate\" priority 1 { when { true } then {\nCharge()\nFail()\n} }")
	var calls []string
	executor := recoveryExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
		calls = append(calls, request.Verb)
		switch request.Verb {
		case "Fail":
			return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("forward step failed")}
		case "UndoCharge":
			return invocation.Outcome{Class: invocation.OutcomeRetryableKnownNotCommitted, Err: errors.New("inverse was not committed")}
		default:
			return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
		}
	})
	store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, languageGeneration(t, env, checked, map[string]invocation.Executor{
		"Charge": executor, "Fail": executor, "UndoCharge": executor, "UndoFail": executor,
	}), store, ledger)
	admission := &Admission{ExecutionID: "inverse-attempt-cap", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
	_, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitAccepted})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	worker := &RecoveryWorker{Engine: engine, Store: ledger, Owner: "inverse-attempt-cap-recovery", BatchSize: 1, LeaseDuration: time.Second}
	processed, err := worker.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, []string{"Charge", "Fail", "UndoCharge"}, calls)
	sagaID := schema.StableSagaID(admission.ExecutionID, "compensate")
	saga, err := store.GetSaga(ctx, sagaID)
	require.NoError(t, err)
	require.Equal(t, schema.SagaBlockedCompensation, saga.State)
	record, err := ledger.GetExecution(ctx, admission.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionBlockedCompensation, record.State)
	dispatches, err := store.ListDispatches(ctx, sagaID)
	require.NoError(t, err)
	for _, dispatch := range dispatches {
		if dispatch.Direction == invocation.DirectionCompensation {
			require.Equal(t, schema.DispatchFailedPermanent, dispatch.State)
			require.Equal(t, uint64(1), dispatch.Attempt)
		}
	}
	_, replayErr := engine.Execute(ctx, ExecuteRequest{Admission: admission, WaitMode: WaitTerminal})
	var terminal *TerminalExecutionError
	require.ErrorAs(t, replayErr, &terminal)
	require.Equal(t, schema.ExecutionBlockedCompensation, terminal.State)
	require.Len(t, calls, 3, "terminal replay must not exceed the inverse attempt cap")
}

func TestCheckedCompensationUnknownOutcomeUsesInverseIdempotencyOnRecovery(t *testing.T) {
	for _, test := range []struct {
		name          string
		forwardPolicy ir.IdempotencyPolicy
		inversePolicy ir.IdempotencyPolicy
		wantCalls     int
		wantExecution schema.ExecutionState
		wantSaga      schema.SagaState
		wantDispatch  schema.DispatchState
	}{
		{"inverse sink guarantee retries", ir.IdempotencyKeyRequired, ir.IdempotencySinkGuaranteed, 2, schema.ExecutionFailed, schema.SagaCompensated, schema.DispatchSucceeded},
		{"forward sink guarantee does not authorize inverse", ir.IdempotencySinkGuaranteed, ir.IdempotencyKeyRequired, 1, schema.ExecutionBlockedUnknown, schema.SagaBlockedUnknown, schema.DispatchBlockedUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := ir.Environment{Verbs: map[string]ir.VerbContract{
				"Charge":     {ResultType: "bool", InverseVerb: "UndoCharge", IdempotencyPolicy: test.forwardPolicy},
				"Fail":       {ResultType: "bool", InverseVerb: "UndoFail", RetryPolicy: ir.RetryPolicy{MaxAttempts: 1}},
				"UndoCharge": {ResultType: "bool", IdempotencyPolicy: test.inversePolicy, RetryPolicy: ir.RetryPolicy{MaxAttempts: 2, InitialBackoffMillis: 1, MaxBackoffMillis: 1}},
				"UndoFail":   {ResultType: "bool"},
			}}
			checked := compileCompensatingPolicyTest(t, env, "rule \"compensate\" priority 1 { when { true } then {\nCharge()\nFail()\n} }")
			var inverseRequests []invocation.Request
			executor := recoveryExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
				switch request.Verb {
				case "Fail":
					return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("forward step failed")}
				case "UndoCharge":
					inverseRequests = append(inverseRequests, request)
					if len(inverseRequests) == 1 {
						return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: errors.New("inverse response was lost")}
					}
				}
				return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
			})
			store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
			engine := languageEngine(t, languageGeneration(t, env, checked, map[string]invocation.Executor{
				"Charge": executor, "Fail": executor, "UndoCharge": executor, "UndoFail": executor,
			}), store, ledger)
			admission := &Admission{ExecutionID: "inverse-unknown", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
			_, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitAccepted})
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			worker := &RecoveryWorker{Engine: engine, Store: ledger, Owner: "inverse-unknown-recovery", BatchSize: 1, LeaseDuration: time.Second}
			processed, err := worker.RunOnce(ctx)
			require.NoError(t, err)
			require.Equal(t, 1, processed)
			require.NoError(t, ctx.Err())
			require.Len(t, inverseRequests, test.wantCalls)
			contractHash, err := ir.ContractHash(env.Verbs["UndoCharge"])
			require.NoError(t, err)
			for index, request := range inverseRequests {
				require.Equal(t, invocation.DirectionCompensation, request.Metadata.Saga.Direction)
				require.Equal(t, uint64(index+1), request.Metadata.Saga.Attempt)
				require.Equal(t, contractHash, request.ContractHash)
				require.Equal(t, inverseRequests[0].Metadata.Saga.IdempotencyKey, request.Metadata.Saga.IdempotencyKey)
			}
			sagaID := schema.StableSagaID(admission.ExecutionID, "compensate")
			saga, err := store.GetSaga(ctx, sagaID)
			require.NoError(t, err)
			require.Equal(t, test.wantSaga, saga.State)
			record, err := ledger.GetExecution(ctx, admission.ExecutionID)
			require.NoError(t, err)
			require.Equal(t, test.wantExecution, record.State)
			dispatches, err := store.ListDispatches(ctx, sagaID)
			require.NoError(t, err)
			found := false
			for _, dispatch := range dispatches {
				if dispatch.Direction == invocation.DirectionCompensation {
					found = true
					require.Equal(t, test.wantDispatch, dispatch.State)
					require.Equal(t, uint64(test.wantCalls), dispatch.Attempt)
					require.Equal(t, contractHash, dispatch.ContractHash)
				}
			}
			require.True(t, found)
			_, replayErr := engine.Execute(ctx, ExecuteRequest{Admission: admission, WaitMode: WaitTerminal})
			var terminal *TerminalExecutionError
			require.ErrorAs(t, replayErr, &terminal)
			require.Equal(t, test.wantExecution, terminal.State)
			require.Len(t, inverseRequests, test.wantCalls, "terminal replay must not invoke an inverse again")
		})
	}
}

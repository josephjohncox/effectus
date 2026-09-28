package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

// Embedding only the stable OutboxStore contract models a custom backend
// compiled before execution finalization became an optional capability.
type outboxWithoutExecutionFinalizer struct{ schema.OutboxStore }

func TestRecoveryFailsClosedWhenOutboxLacksExecutionFinalizer(t *testing.T) {
	env := ir.Environment{Verbs: map[string]ir.VerbContract{"Fail": {ResultType: "bool"}}}
	checked := compileLanguage(t, env, `rule "first" priority 1 { when { true } then { Fail() } }`, "eff")
	calls := 0
	executor := recoveryExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		calls++
		return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("permanent failure")}
	})
	store := outboxWithoutExecutionFinalizer{OutboxStore: schema.NewInMemoryOutboxStore()}
	ledger := schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, languageGeneration(t, env, checked, map[string]invocation.Executor{"Fail": executor}), store, ledger)
	admission := &Admission{ExecutionID: "custom-store", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
	_, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitAccepted})
	require.NoError(t, err)
	worker := &RecoveryWorker{Engine: engine, Store: ledger, Owner: "custom-store-recovery", BatchSize: 2, LeaseDuration: time.Second}
	processed, err := worker.RunOnce(t.Context())
	require.Equal(t, 1, processed)
	require.ErrorIs(t, err, ErrDurableDisposition)
	require.ErrorContains(t, err, "workflow store does not support atomic execution finalization")
	record, err := ledger.GetExecution(t.Context(), admission.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionAccepted, record.State)
	require.Empty(t, record.RecoveryToken)
	require.Equal(t, 1, calls)
}

func TestFailFastDoesNotCompensatePriorSuccessOnRecovery(t *testing.T) {
	env := ir.Environment{Verbs: map[string]ir.VerbContract{
		"Charge":     {ResultType: "bool", InverseVerb: "UndoCharge"},
		"Fail":       {ResultType: "bool"},
		"UndoCharge": {ResultType: "bool"},
	}}
	checked := compileLanguage(t, env, "rule \"first\" priority 1 { when { true } then {\nCharge()\nFail()\n} }", "eff")
	require.Equal(t, effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_FAIL_FAST, checked.CloneArtifact().Plans[0].ExecutionPolicy)
	require.Nil(t, checked.CloneArtifact().Plans[0].Steps[0].Compensation)
	var calls []string
	executor := recoveryExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
		calls = append(calls, request.Verb)
		if request.Verb == "Fail" {
			return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("permanent failure")}
		}
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	})
	store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, languageGeneration(t, env, checked, map[string]invocation.Executor{
		"Charge": executor, "Fail": executor, "UndoCharge": executor,
	}), store, ledger)
	admission := &Admission{ExecutionID: "fail-fast", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
	_, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitAccepted})
	require.NoError(t, err)
	worker := &RecoveryWorker{Engine: engine, Store: ledger, Owner: "fail-fast-recovery", BatchSize: 1, LeaseDuration: time.Second}
	processed, err := worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	record, err := ledger.GetExecution(t.Context(), admission.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionFailed, record.State)
	saga, err := store.GetSaga(t.Context(), schema.StableSagaID(admission.ExecutionID, "first"))
	require.NoError(t, err)
	require.Equal(t, schema.SagaFailed, saga.State)
	require.Equal(t, []string{"Charge", "Fail"}, calls)
	processed, err = worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Zero(t, processed)
}

func TestLegacyFailFastArtifactDoesNotCreateCompensationIntent(t *testing.T) {
	env := ir.Environment{Verbs: map[string]ir.VerbContract{
		"Charge":     {ResultType: "bool", InverseVerb: "UndoCharge"},
		"Fail":       {ResultType: "bool"},
		"UndoCharge": {ResultType: "bool"},
	}}
	compiled := compileLanguage(t, env, "rule \"first\" priority 1 { when { true } then {\nCharge()\nFail()\n} }", "eff")
	artifact := compiled.CloneArtifact()
	hash, err := ir.ContractHash(env.Verbs["UndoCharge"])
	require.NoError(t, err)
	artifact.Plans[0].Steps[0].Compensation = &effectusv1.CompensationContract{InverseVerb: "UndoCharge", InverseContractHash: hash}
	legacy, err := ir.Check(artifact, env, ir.Limits{})
	require.NoError(t, err)
	legacy, err = ir.Parse(legacy.Marshal(), env, ir.Limits{})
	require.NoError(t, err)
	require.Equal(t, "UndoCharge", legacy.CloneArtifact().Plans[0].Steps[0].Compensation.InverseVerb)
	initial, err := durableInitialStep(legacy.CloneArtifact().Plans[0], nil, "legacy-saga")
	require.NoError(t, err)
	require.Empty(t, initial.CompensationVerb)
	require.Empty(t, initial.CompensationContract)
	var calls []string
	executor := recoveryExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
		calls = append(calls, request.Verb)
		if request.Verb == "Fail" {
			return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("permanent failure")}
		}
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	})
	store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, languageGeneration(t, env, legacy, map[string]invocation.Executor{
		"Charge": executor, "Fail": executor, "UndoCharge": executor,
	}), store, ledger)
	admission := &Admission{ExecutionID: "legacy-fail-fast", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
	_, err = engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitAccepted})
	require.NoError(t, err)
	require.NoError(t, engine.Close())
	legacy, err = ir.Parse(legacy.Marshal(), env, ir.Limits{})
	require.NoError(t, err)
	restartedGeneration := languageGeneration(t, env, legacy, map[string]invocation.Executor{
		"Charge": executor, "Fail": executor, "UndoCharge": executor,
	})
	restarted := languageEngine(t, restartedGeneration, store, ledger)
	worker := &RecoveryWorker{Engine: restarted, Store: ledger, Owner: "legacy-recovery", BatchSize: 1, LeaseDuration: time.Second}
	processed, err := worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	record, err := ledger.GetExecution(t.Context(), admission.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionFailed, record.State)
	sagaID := schema.StableSagaID(admission.ExecutionID, "first")
	saga, err := store.GetSaga(t.Context(), sagaID)
	require.NoError(t, err)
	require.Equal(t, schema.SagaFailed, saga.State)
	dispatches, err := store.ListDispatches(t.Context(), sagaID)
	require.NoError(t, err)
	for _, dispatch := range dispatches {
		require.NotEqual(t, invocation.DirectionCompensation, dispatch.Direction)
	}
	blocked := checkedWorkflowInvocationExecutor{generation: restartedGeneration, store: store}.Invoke(t.Context(), invocation.Request{
		Metadata: invocation.Context{Saga: invocation.Saga{SagaID: sagaID, Direction: invocation.DirectionCompensation}},
		Verb:     "UndoCharge",
	})
	require.Equal(t, invocation.OutcomePermanentFailure, blocked.Class)
	require.ErrorIs(t, blocked.Err, schema.ErrInvalidTransition)
	require.Equal(t, []string{"Charge", "Fail"}, calls)
}

func TestRecoveryBlocksPreviouslyPersistedFailFastInverse(t *testing.T) {
	env := ir.Environment{Verbs: map[string]ir.VerbContract{
		"Charge": {ResultType: "bool", InverseVerb: "UndoCharge"},
		"Fail":   {ResultType: "bool"}, "UndoCharge": {ResultType: "bool"},
	}}
	compiled := compileLanguage(t, env, "rule \"first\" priority 1 { when { true } then {\nCharge()\nFail()\n} }", "eff")
	artifact := compiled.CloneArtifact()
	inverseHash, err := ir.ContractHash(env.Verbs["UndoCharge"])
	require.NoError(t, err)
	artifact.Plans[0].Steps[0].Compensation = &effectusv1.CompensationContract{InverseVerb: "UndoCharge", InverseContractHash: inverseHash}
	legacy, err := ir.Check(artifact, env, ir.Limits{})
	require.NoError(t, err)
	oldGeneration := languageGeneration(t, env, legacy, map[string]invocation.Executor{
		"Charge": recoveryTestExecutor{}, "Fail": recoveryTestExecutor{}, "UndoCharge": recoveryTestExecutor{},
	})
	admission := &Admission{ExecutionID: "persisted-legacy-inverse", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
	prepared, err := prepareAdmission(admission)
	require.NoError(t, err)
	hash, err := semanticAdmissionHash(prepared, env)
	require.NoError(t, err)
	durable, _, _, err := buildDurableAdmission(t.Context(), oldGeneration, prepared, hash)
	require.NoError(t, err)
	require.NoError(t, oldGeneration.Close())
	require.Len(t, durable.InitialSteps, 1)
	// Reconstruct the persisted row written by the previous compiler/runtime.
	durable.InitialSteps[0].CompensationVerb = "UndoCharge"
	durable.InitialSteps[0].CompensationContract = inverseHash
	durable.InitialSteps[0].CompensationArguments = durable.InitialSteps[0].Arguments
	store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
	require.NoError(t, ledger.PutArtifact(t.Context(), durable.Artifact))
	_, created, err := ledger.AdmitExecution(t.Context(), durable)
	require.NoError(t, err)
	require.True(t, created)
	for _, saga := range durable.Sagas {
		_, err := store.CreateSaga(t.Context(), saga)
		require.NoError(t, err)
	}
	for _, step := range durable.InitialSteps {
		_, err := store.EnqueueStep(t.Context(), step)
		require.NoError(t, err)
	}
	var calls []string
	executor := recoveryExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
		calls = append(calls, request.Verb)
		if request.Verb == "Fail" {
			return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("permanent failure")}
		}
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	})
	reloaded, err := ir.Parse(legacy.Marshal(), env, ir.Limits{})
	require.NoError(t, err)
	engine := languageEngine(t, languageGeneration(t, env, reloaded, map[string]invocation.Executor{
		"Charge": executor, "Fail": executor, "UndoCharge": executor,
	}), store, ledger)
	worker := &RecoveryWorker{Engine: engine, Store: ledger, Owner: "persisted-legacy-recovery", BatchSize: 1, LeaseDuration: time.Second}
	processed, err := worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	record, err := ledger.GetExecution(t.Context(), admission.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionBlockedCompensation, record.State)
	sagaID := schema.StableSagaID(admission.ExecutionID, "first")
	saga, err := store.GetSaga(t.Context(), sagaID)
	require.NoError(t, err)
	require.Equal(t, schema.SagaBlockedCompensation, saga.State)
	dispatches, err := store.ListDispatches(t.Context(), sagaID)
	require.NoError(t, err)
	compensationCount := 0
	for _, dispatch := range dispatches {
		if dispatch.Direction == invocation.DirectionCompensation {
			compensationCount++
			require.Equal(t, schema.DispatchCanceled, dispatch.State)
		}
	}
	require.Equal(t, 1, compensationCount)
	require.Equal(t, []string{"Charge", "Fail"}, calls)
}

func TestFailedExecutionCancelsLaterSelectedPlanOnRecovery(t *testing.T) {
	env := ir.Environment{Verbs: map[string]ir.VerbContract{
		"Fail":  {ResultType: "bool"},
		"Later": {ResultType: "bool"},
	}}
	checked := compileLanguage(t, env, `rule "first" priority 2 { when { true } then { Fail() } } rule "later" priority 1 { when { true } then { Later() } }`, "eff")
	var calls []string
	executor := recoveryExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
		calls = append(calls, request.Verb)
		if request.Verb == "Fail" {
			return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("permanent failure")}
		}
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	})
	store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, languageGeneration(t, env, checked, map[string]invocation.Executor{
		"Fail": executor, "Later": executor,
	}), store, ledger)
	admission := &Admission{ExecutionID: "two-plans", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
	_, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitAccepted})
	require.NoError(t, err)
	laterID := schema.StableSagaID(admission.ExecutionID, "later")
	pending, err := store.ListDispatches(t.Context(), laterID)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, schema.DispatchQueued, pending[0].State)
	worker := &RecoveryWorker{Engine: engine, Store: ledger, Owner: "two-plans-recovery", BatchSize: 2, LeaseDuration: time.Second}
	processed, err := worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	record, err := ledger.GetExecution(t.Context(), admission.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionFailed, record.State)
	later, err := store.GetSaga(t.Context(), laterID)
	require.NoError(t, err)
	require.Equal(t, schema.SagaFailed, later.State)
	canceled, err := store.ListDispatches(t.Context(), laterID)
	require.NoError(t, err)
	require.Len(t, canceled, 1)
	require.Equal(t, schema.DispatchCanceled, canceled[0].State)
	require.Equal(t, []string{"Fail"}, calls)
	processed, err = worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Zero(t, processed)
}

func TestFailedExecutionCancelsLaterSelectedPlanWithTerminalWait(t *testing.T) {
	env := ir.Environment{Verbs: map[string]ir.VerbContract{
		"Fail": {ResultType: "bool"}, "Later": {ResultType: "bool"},
	}}
	checked := compileLanguage(t, env, `rule "first" priority 2 { when { true } then { Fail() } } rule "later" priority 1 { when { true } then { Later() } }`, "eff")
	var calls []string
	executor := recoveryExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
		calls = append(calls, request.Verb)
		return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("permanent failure")}
	})
	store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, languageGeneration(t, env, checked, map[string]invocation.Executor{
		"Fail": executor, "Later": executor,
	}), store, ledger)
	admission := &Admission{ExecutionID: "terminal-two-plans", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
	result, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitTerminal})
	var terminal *TerminalExecutionError
	require.ErrorAs(t, err, &terminal)
	require.Equal(t, schema.ExecutionFailed, terminal.State)
	require.Equal(t, string(schema.ExecutionFailed), result.State)
	laterID := schema.StableSagaID(admission.ExecutionID, "later")
	later, err := store.GetSaga(t.Context(), laterID)
	require.NoError(t, err)
	require.Equal(t, schema.SagaFailed, later.State)
	dispatches, err := store.ListDispatches(t.Context(), laterID)
	require.NoError(t, err)
	require.Len(t, dispatches, 1)
	require.Equal(t, schema.DispatchCanceled, dispatches[0].State)
	require.Equal(t, []string{"Fail"}, calls)
}

func TestBlockedExecutionCancelsLaterSelectedPlanOnRecovery(t *testing.T) {
	env := ir.Environment{Verbs: map[string]ir.VerbContract{
		"Uncertain": {ResultType: "bool"}, "Later": {ResultType: "bool"},
	}}
	checked := compileLanguage(t, env, `rule "first" priority 2 { when { true } then { Uncertain() } } rule "later" priority 1 { when { true } then { Later() } }`, "eff")
	var calls []string
	executor := recoveryExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
		calls = append(calls, request.Verb)
		if request.Verb == "Uncertain" {
			return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: errors.New("destination response was lost")}
		}
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	})
	store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, languageGeneration(t, env, checked, map[string]invocation.Executor{
		"Uncertain": executor, "Later": executor,
	}), store, ledger)
	admission := &Admission{ExecutionID: "blocked-two-plans", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
	_, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitAccepted})
	require.NoError(t, err)
	worker := &RecoveryWorker{Engine: engine, Store: ledger, Owner: "blocked-recovery", BatchSize: 2, LeaseDuration: time.Second}
	processed, err := worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	record, err := ledger.GetExecution(t.Context(), admission.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionBlockedUnknown, record.State)
	laterID := schema.StableSagaID(admission.ExecutionID, "later")
	later, err := store.GetSaga(t.Context(), laterID)
	require.NoError(t, err)
	require.Equal(t, schema.SagaFailed, later.State)
	dispatches, err := store.ListDispatches(t.Context(), laterID)
	require.NoError(t, err)
	require.Len(t, dispatches, 1)
	require.Equal(t, schema.DispatchCanceled, dispatches[0].State)
	require.Equal(t, []string{"Uncertain"}, calls)
}

func TestStoppedExecutionPreservesLaterPlanDispatchEvidence(t *testing.T) {
	for _, test := range []struct {
		name        string
		laterState  schema.DispatchState
		wantState   schema.ExecutionState
		wantSaga    schema.SagaState
		wantCalls   []string
		waitPending bool
	}{
		{name: "active lease", laterState: schema.DispatchInFlight, wantState: schema.ExecutionAccepted, wantSaga: schema.SagaRunning, wantCalls: []string{"Fail"}, waitPending: true},
		{name: "expired lease", laterState: schema.DispatchBlockedUnknown, wantState: schema.ExecutionBlockedUnknown, wantSaga: schema.SagaBlockedUnknown, wantCalls: []string{"Fail"}},
		{name: "prior success", laterState: schema.DispatchSucceeded, wantState: schema.ExecutionBlockedDependency, wantSaga: schema.SagaBlockedDependency, wantCalls: []string{"Later", "Fail"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := ir.Environment{Verbs: map[string]ir.VerbContract{
				"Fail": {ResultType: "bool"}, "Later": {ResultType: "bool"},
			}}
			checked := compileLanguage(t, env, `rule "first" priority 2 { when { true } then { Fail() } } rule "later" priority 1 { when { true } then { Later() } }`, "eff")
			var calls []string
			executor := recoveryExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
				calls = append(calls, request.Verb)
				if request.Verb == "Fail" {
					return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("permanent failure")}
				}
				return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
			})
			store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
			engine := languageEngine(t, languageGeneration(t, env, checked, map[string]invocation.Executor{
				"Fail": executor, "Later": executor,
			}), store, ledger)
			admission := &Admission{ExecutionID: "two-plans-" + test.name, TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
			_, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitAccepted})
			require.NoError(t, err)
			laterID := schema.StableSagaID(admission.ExecutionID, "later")
			pending, err := store.ListDispatches(t.Context(), laterID)
			require.NoError(t, err)
			require.Len(t, pending, 1)
			switch test.laterState {
			case schema.DispatchInFlight:
				_, err = store.ClaimDispatch(t.Context(), schema.ClaimOptions{Owner: "external", LeaseDuration: time.Minute, TargetDispatchID: pending[0].ID})
			case schema.DispatchBlockedUnknown:
				_, err = store.ClaimDispatch(t.Context(), schema.ClaimOptions{Owner: "external", LeaseDuration: time.Second, Now: time.Now().Add(-2 * time.Second), TargetDispatchID: pending[0].ID})
			case schema.DispatchSucceeded:
				var dispatcher *schema.Dispatcher
				dispatcher, err = schema.NewDispatcher(store, nil, checkedWorkflowInvocationExecutor{generation: engine.Generation(), store: store}, schema.DispatcherOptions{Owner: "external", RequestID: admission.ExecutionID})
				if err == nil {
					_, err = dispatcher.Dispatch(t.Context(), pending[0].ID)
				}
			}
			require.NoError(t, err)
			worker := &RecoveryWorker{Engine: engine, Store: ledger, Owner: "evidence-recovery", BatchSize: 2, LeaseDuration: time.Second}
			processed, err := worker.RunOnce(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, processed)
			record, err := ledger.GetExecution(t.Context(), admission.ExecutionID)
			require.NoError(t, err)
			require.Equal(t, test.wantState, record.State)
			require.Empty(t, record.RecoveryToken)
			saga, err := store.GetSaga(t.Context(), laterID)
			require.NoError(t, err)
			require.Equal(t, test.wantSaga, saga.State)
			later, err := store.GetDispatch(t.Context(), pending[0].ID)
			require.NoError(t, err)
			require.Equal(t, test.laterState, later.State)
			require.Equal(t, test.wantCalls, calls)
			if test.waitPending {
				require.NotEmpty(t, later.LeaseToken)
			} else {
				processed, err = worker.RunOnce(t.Context())
				require.NoError(t, err)
				require.Zero(t, processed)
			}
		})
	}
}

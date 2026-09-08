package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

func TestEngineFirstAttemptAndReplayPreserveDisposition(t *testing.T) {
	for _, test := range []struct {
		state   schema.ExecutionState
		outcome invocation.OutcomeClass
	}{
		{schema.ExecutionCompleted, invocation.OutcomeSuccess},
		{schema.ExecutionFailed, invocation.OutcomePermanentFailure},
		{schema.ExecutionBlockedUnknown, invocation.OutcomeUnknown},
		{schema.ExecutionBlockedFence, invocation.OutcomeStaleFence},
	} {
		t.Run(string(test.state), func(t *testing.T) {
			durable := schema.NewInMemoryExecutionLedger()
			calls := 0
			cause := errors.New("destination response")
			engine := recoveryFixture(t, durable, 0, recoveryExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
				calls++
				if test.outcome == invocation.OutcomeSuccess {
					return invocation.Outcome{Class: test.outcome, Result: true}
				}
				return invocation.Outcome{Class: test.outcome, Err: cause}
			}))
			admission := &Admission{ExecutionID: "execute", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
			for attempt := range 2 {
				result, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitTerminal})
				require.Equal(t, string(test.state), result.State)
				if test.state == schema.ExecutionCompleted {
					require.NoError(t, err)
				} else {
					var terminal *TerminalExecutionError
					require.ErrorAs(t, err, &terminal, "attempt %d", attempt)
					require.ErrorIs(t, err, ErrTerminalExecution)
					require.Equal(t, test.state, terminal.State)
				}
			}
			accepted, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitAccepted})
			require.NoError(t, err)
			require.Equal(t, string(test.state), accepted.State)
			require.Equal(t, 1, calls)
		})
	}
}

func TestEngineEveryTerminalReplayWithoutResolvingExecutors(t *testing.T) {
	for _, state := range []schema.ExecutionState{schema.ExecutionCompleted, schema.ExecutionFailed, schema.ExecutionBlockedUnknown, schema.ExecutionBlockedFence, schema.ExecutionBlockedDependency, schema.ExecutionBlockedCompensation} {
		t.Run(string(state), func(t *testing.T) {
			durable := schema.NewInMemoryExecutionLedger()
			engine := recoveryFixture(t, durable, 1, recoveryExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
				t.Fatal("terminal replay invoked an executor")
				return invocation.Outcome{}
			}))
			record, err := durable.GetExecution(t.Context(), "item-0")
			require.NoError(t, err)
			_, err = durable.SetExecutionState(t.Context(), record.ExecutionID, record.Revision, state, "original disposition")
			require.NoError(t, err)
			for _, wait := range []WaitMode{WaitAccepted, WaitTerminal} {
				result, err := engine.Execute(t.Context(), ExecuteRequest{ResumeExecutionID: record.ExecutionID, WaitMode: wait})
				require.Equal(t, string(state), result.State)
				if wait == WaitAccepted || state == schema.ExecutionCompleted {
					require.NoError(t, err)
				} else {
					var terminal *TerminalExecutionError
					require.ErrorAs(t, err, &terminal)
					require.Equal(t, state, terminal.State)
					if state == schema.ExecutionBlockedDependency {
						require.ErrorIs(t, err, ErrBlockedDependency)
					}
				}
			}
		})
	}
}

type countedAdmissionLedger struct {
	*schema.InMemoryExecutionLedger
	writes atomic.Int64
}

func (s *countedAdmissionLedger) PutArtifact(ctx context.Context, artifact schema.ExecutionArtifact) error {
	s.writes.Add(1)
	return s.InMemoryExecutionLedger.PutArtifact(ctx, artifact)
}

func TestEngineRejectsGenerationLabelsBeforeWrites(t *testing.T) {
	store := &countedAdmissionLedger{InMemoryExecutionLedger: schema.NewInMemoryExecutionLedger()}
	calls := 0
	engine := recoveryFixture(t, store, 0, recoveryExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		calls++
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	}))
	for _, admission := range []*Admission{
		{ExecutionID: "wrong-ruleset", TenantNamespace: "tenant", Ruleset: "other", Version: "1"},
		{ExecutionID: "wrong-version", TenantNamespace: "tenant", Ruleset: "language", Version: "2"},
		{ExecutionID: "wrong-digest", TenantNamespace: "tenant", Ruleset: "language", Version: "1", ExpectedGenerationDigest: "wrong"},
	} {
		_, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission})
		require.ErrorIs(t, err, ErrGenerationMismatch)
		_, err = store.GetExecution(t.Context(), admission.ExecutionID)
		require.ErrorIs(t, err, schema.ErrExecutionNotFound)
	}
	require.Zero(t, store.writes.Load())
	require.Zero(t, calls)
}

func identityEnvironment() ir.Environment {
	return ir.Environment{Facts: map[string]string{"n": "int", "blob": "bytes", "tags": "list<int>", "user.score": "int"}, Verbs: map[string]ir.VerbContract{"Review": {ResultType: "bool"}}}
}
func equivalentIdentityFacts() []map[string]any {
	return []map[string]any{
		{"n": json.Number("1.0"), "blob": []byte{0, 1}, "tags": []byte{1, 2}, "user": map[string]any{"score": json.Number("2e0")}},
		{"n": int64(1), "blob": "AAE=", "tags": []any{1, 2}, "user.score": int64(2)},
		{"n": json.Number("1e0"), "blob": "AAE=", "tags": []int{1, 2}, "user": map[string]any{"score": 99}, "user.score": 2},
	}
}

func TestEngineSemanticIdentityAndCallerOwnership(t *testing.T) {
	env := identityEnvironment()
	checked := compileLanguage(t, env, `rule "review" priority 1 { when { true } then { Review() } }`, "eff")
	generation := languageGeneration(t, env, checked, map[string]invocation.Executor{"Review": recoveryTestExecutor{}})
	store := schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, generation, schema.NewInMemoryOutboxStore(), store)
	for i, facts := range equivalentIdentityFacts() {
		input := &Admission{ExecutionID: " identity ", AdmissionID: " delivery ", TenantNamespace: " tenant ", Ruleset: "language", Version: "1", Facts: facts}
		if i != 0 {
			input.MergePolicy = "merge"
		}
		before, err := json.Marshal(input)
		require.NoError(t, err)
		_, err = engine.Execute(t.Context(), ExecuteRequest{Admission: input, WaitMode: WaitAccepted})
		require.NoError(t, err)
		after, err := json.Marshal(input)
		require.NoError(t, err)
		require.Equal(t, before, after)
	}
	conflict := &Admission{ExecutionID: "identity", AdmissionID: "delivery", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: equivalentIdentityFacts()[1]}
	conflict.Facts["n"] = 3
	_, err := engine.Execute(t.Context(), ExecuteRequest{Admission: conflict, WaitMode: WaitAccepted})
	require.ErrorIs(t, err, ErrIdentityConflict)
	engine.mu.Lock()
	require.Empty(t, engine.executions, "idle executions must not be retained")
	engine.mu.Unlock()
	require.Error(t, engine.ConfigureLedger(store, nil), "cache eviction must not reopen configuration")
}

func TestEngineLegacyHashReplayUsesPinnedEffectiveFacts(t *testing.T) {
	env := identityEnvironment()
	checked := compileLanguage(t, env, `rule "review" priority 1 { when { true } then { Review() } }`, "eff")
	generation := languageGeneration(t, env, checked, map[string]invocation.Executor{"Review": recoveryTestExecutor{}})
	store := schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, generation, schema.NewInMemoryOutboxStore(), store)
	old := &Admission{ExecutionID: "legacy", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: equivalentIdentityFacts()[0]}
	hash, err := admissionHash(old)
	require.NoError(t, err)
	admission, _, _, err := buildDurableAdmission(t.Context(), generation, old, hash)
	require.NoError(t, err)
	require.NoError(t, store.PutArtifact(t.Context(), admission.Artifact))
	_, _, err = store.AdmitExecution(t.Context(), admission)
	require.NoError(t, err)
	replay := *old
	replay.MergePolicy, replay.Facts = "merge", equivalentIdentityFacts()[1]
	_, err = engine.Execute(t.Context(), ExecuteRequest{Admission: &replay, WaitMode: WaitAccepted})
	require.NoError(t, err)
	record, err := store.GetExecution(t.Context(), old.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, hash, record.RequestHash, "do not migrate persisted hashes implicitly")
}

func TestEngineTwoInstancesObserveTerminalStateAndCoalesce(t *testing.T) {
	store := schema.NewInMemoryExecutionLedger()
	outbox := schema.NewInMemoryOutboxStore()
	var calls atomic.Int64
	env := ir.Environment{Verbs: map[string]ir.VerbContract{"Review": {ResultType: "bool"}}}
	checked := compileLanguage(t, env, `rule "review" priority 1 { when { true } then { Review() } }`, "eff")
	executors := map[string]invocation.Executor{"Review": recoveryExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		calls.Add(1)
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	})}
	first := languageEngine(t, languageGeneration(t, env, checked, executors), outbox, store)
	second := languageEngine(t, languageGeneration(t, env, checked, executors), outbox, store)
	admission := &Admission{ExecutionID: "shared", TenantNamespace: "tenant", Ruleset: "language", Version: "1", Facts: map[string]any{}}
	_, err := first.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitAccepted})
	require.NoError(t, err)
	var group sync.WaitGroup
	results := make(chan error, 12)
	for index := range 12 {
		group.Go(func() {
			engine := first
			if index%2 != 0 {
				engine = second
			}
			result, err := engine.Execute(t.Context(), ExecuteRequest{Admission: admission, WaitMode: WaitTerminal})
			if err == nil && !result.Completed {
				err = fmt.Errorf("not completed: %s", result.State)
			}
			results <- err
		})
	}
	group.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.Equal(t, int64(1), calls.Load())
	result, err := first.Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "shared", WaitMode: WaitTerminal})
	require.NoError(t, err)
	require.True(t, result.Completed)
}

func TestEngineDoesNotImpersonateRecoveryOwner(t *testing.T) {
	store := schema.NewInMemoryExecutionLedger()
	calls := 0
	engine := recoveryFixture(t, store, 1, recoveryExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		calls++
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	}))
	leases, err := store.LeaseExecutions(t.Context(), "owner", 1, 60000000000)
	require.NoError(t, err)
	_, err = engine.Execute(t.Context(), ExecuteRequest{ResumeExecutionID: "item-0", WaitMode: WaitTerminal})
	require.ErrorIs(t, err, ErrExecutionBusy)
	record, err := store.GetExecution(t.Context(), "item-0")
	require.NoError(t, err)
	require.Equal(t, leases[0].Token, record.RecoveryToken)
	require.Zero(t, calls)
}

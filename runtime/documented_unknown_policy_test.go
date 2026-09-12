package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

func TestDocumentedCheckedUnknownOutcomePolicy(t *testing.T) {
	for _, test := range []struct {
		name                string
		policy              ir.IdempotencyPolicy
		checkedPolicy       effectusv1.IdempotencyPolicy
		maxAttempts         uint32
		succeedAfterUnknown bool
		wantOutcomes        []invocation.OutcomeClass
	}{
		{"sink guarantee permits retry", ir.IdempotencySinkGuaranteed, effectusv1.IdempotencyPolicy_IDEMPOTENCY_POLICY_SINK_GUARANTEED, 2, true, []invocation.OutcomeClass{invocation.OutcomeUnknown, invocation.OutcomeSuccess}},
		{"key alone does not permit retry", ir.IdempotencyKeyRequired, effectusv1.IdempotencyPolicy_IDEMPOTENCY_POLICY_KEY_REQUIRED, 2, true, []invocation.OutcomeClass{invocation.OutcomeUnknown}},
		{"sink guarantee still exhausts", ir.IdempotencySinkGuaranteed, effectusv1.IdempotencyPolicy_IDEMPOTENCY_POLICY_SINK_GUARANTEED, 2, false, []invocation.OutcomeClass{invocation.OutcomeUnknown, invocation.OutcomeUnknown}},
		{"default attempt budget is one", ir.IdempotencySinkGuaranteed, effectusv1.IdempotencyPolicy_IDEMPOTENCY_POLICY_SINK_GUARANTEED, 0, true, []invocation.OutcomeClass{invocation.OutcomeUnknown}},
	} {
		t.Run(test.name, func(t *testing.T) {
			environment := ir.Environment{
				Facts: map[string]string{"order.id": "string"},
				Verbs: map[string]ir.VerbContract{"Review": {
					Arguments: map[string]string{"orderId": "string"}, ResultType: "void",
					IdempotencyPolicy: test.policy,
					RetryPolicy:       ir.RetryPolicy{MaxAttempts: test.maxAttempts, InitialBackoffMillis: 1, MaxBackoffMillis: 1},
				}},
			}
			checked := compileLanguage(t, environment, `rule "ReviewOrder" priority 1 { when { true } then { Review(orderId: order.id) } }`, "eff")
			artifact := checked.CloneArtifact()
			require.Len(t, artifact.Plans, 1)
			require.Len(t, artifact.Plans[0].Steps, 1)
			step := artifact.Plans[0].Steps[0]
			require.Equal(t, test.checkedPolicy, step.IdempotencyPolicy)
			limit := test.maxAttempts
			if limit == 0 {
				limit = 1
			}
			require.Equal(t, limit, step.GetRetryPolicy().GetMaxAttempts())

			// A process-local destination fixture records one business commit but
			// initially loses the response. A duplicate can return the stored result.
			// This fixture is not proof of a deployed destination's guarantee.
			var mu sync.Mutex
			var requests []invocation.Request
			businessCommits := 0
			commits := make(map[string]bool)
			executor := languageExecutor(func(_ context.Context, request invocation.Request) invocation.Outcome {
				mu.Lock()
				defer mu.Unlock()
				requests = append(requests, request)
				key := request.Metadata.Saga.IdempotencyKey
				duplicate := commits[key]
				if !duplicate {
					businessCommits++
					commits[key] = true
				}
				if duplicate && test.succeedAfterUnknown {
					return invocation.Outcome{Class: invocation.OutcomeSuccess}
				}
				return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: errors.New("fixture committed but response was lost")}
			})
			store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
			generation := languageGeneration(t, environment, checked, map[string]invocation.Executor{"Review": executor})
			engine := languageEngine(t, generation, store, ledger)
			// A larger dispatcher default must not override the checked attempt cap.
			require.NoError(t, engine.ConfigureWorkflow(store, nil, schema.DispatcherOptions{
				Owner: "documented-policy", MaxAttempts: 9,
				InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
			}))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			request := ExecuteRequest{Admission: &Admission{
				ExecutionID: "documented-policy", AdmissionID: "documented-policy", TenantNamespace: "docs",
				Ruleset: "language", Version: "1", Facts: map[string]any{"order.id": "one"},
			}, WaitMode: WaitTerminal}
			done, executionErr := engine.Execute(ctx, request)
			wantState, wantSaga, wantDispatch := schema.ExecutionBlockedUnknown, schema.SagaBlockedUnknown, schema.DispatchBlockedUnknown
			succeeded := test.wantOutcomes[len(test.wantOutcomes)-1] == invocation.OutcomeSuccess
			if succeeded {
				wantState, wantSaga, wantDispatch = schema.ExecutionCompleted, schema.SagaCompleted, schema.DispatchSucceeded
				require.NoError(t, executionErr)
			} else {
				var terminal *TerminalExecutionError
				require.ErrorAs(t, executionErr, &terminal)
				require.Equal(t, wantState, terminal.State)
			}
			require.NoError(t, ctx.Err())
			require.Equal(t, string(wantState), done.State)
			require.True(t, done.DurablyAccepted)
			require.Equal(t, succeeded, done.Completed)
			record, err := ledger.GetExecution(ctx, done.ExecutionID)
			require.NoError(t, err)
			require.Equal(t, wantState, record.State)
			sagaID := schema.StableSagaID(done.ExecutionID, artifact.Plans[0].Id)
			saga, err := store.GetSaga(ctx, sagaID)
			require.NoError(t, err)
			require.Equal(t, wantSaga, saga.State)
			dispatches, err := store.ListDispatches(ctx, sagaID)
			require.NoError(t, err)
			require.Len(t, dispatches, 1)
			dispatch := dispatches[0]
			require.Equal(t, invocation.DirectionForward, dispatch.Direction)
			require.Equal(t, wantDispatch, dispatch.State)
			require.Equal(t, uint64(len(test.wantOutcomes)), dispatch.Attempt)
			require.NotEmpty(t, dispatch.IdempotencyKey)
			attempts, err := store.ListAttempts(ctx, dispatch.ID)
			require.NoError(t, err)
			require.Len(t, attempts, len(test.wantOutcomes))
			for index, attempt := range attempts {
				require.Equal(t, dispatch.ID, attempt.DispatchID)
				require.Equal(t, uint64(index+1), attempt.Attempt)
				require.Equal(t, test.wantOutcomes[index], attempt.Outcome)
				require.False(t, attempt.CompletedAt.IsZero())
			}

			// Terminal replay does not resume an exhausted or unauthorized retry.
			replayed, replayErr := engine.Execute(ctx, request)
			require.Equal(t, done, replayed)
			if succeeded {
				require.NoError(t, replayErr)
			} else {
				var terminal *TerminalExecutionError
				require.ErrorAs(t, replayErr, &terminal)
				require.Equal(t, wantState, terminal.State)
			}
			mu.Lock()
			defer mu.Unlock()
			require.Len(t, requests, len(test.wantOutcomes), "replay must not invoke the executor again")
			require.Len(t, commits, 1, "stable identity must address one fixture business commit")
			require.Equal(t, 1, businessCommits, "the fixture coordinates deduplication with its business mutation")
			for index, invocation := range requests {
				require.Equal(t, done.ExecutionID, invocation.Metadata.RequestID)
				require.Equal(t, done.ExecutionID, invocation.Metadata.ExecutionID)
				require.Equal(t, sagaID, invocation.Metadata.Saga.SagaID)
				require.Equal(t, step.Id, invocation.Metadata.Saga.EffectID)
				require.Equal(t, dispatch.Direction, invocation.Metadata.Saga.Direction)
				require.Equal(t, dispatch.IdempotencyKey, invocation.Metadata.Saga.IdempotencyKey)
				require.Equal(t, uint64(index+1), invocation.Metadata.Saga.Attempt)
				require.Equal(t, dispatch.ArgumentHash, invocation.ArgumentHash)
				require.Equal(t, step.ContractHash, invocation.ContractHash)
				require.Equal(t, map[string]any{"orderId": "one"}, invocation.Arguments)
			}
		})
	}
}

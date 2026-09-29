package schema

import (
	"context"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema/fencing"
	"github.com/stretchr/testify/require"
)

type countingFenceProvider struct {
	fencing.Provider
	acquisitions int
}

func (provider *countingFenceProvider) Acquire(ctx context.Context, request fencing.Request) (fencing.Lease, error) {
	provider.acquisitions++
	return provider.Provider.Acquire(ctx, request)
}

func TestDispatcherClosesPreviouslyQueuedInverseAtNewAttemptCap(t *testing.T) {
	for _, outcome := range []invocation.OutcomeClass{
		invocation.OutcomeRetryableKnownNotCommitted,
		invocation.OutcomeUnknown,
	} {
		t.Run(string(outcome), func(t *testing.T) {
			assertQueuedInverseAtAttemptCap(t, NewInMemoryOutboxStore(), "exhausted-inverse-"+string(outcome), outcome)
		})
	}
}

// Model a retry_wait row left by a worker that used the failed forward step's
// larger budget. A later worker must honor the inverse's smaller budget.
func assertQueuedInverseAtAttemptCap(t *testing.T, store OutboxStore, sagaID string, previousOutcome invocation.OutcomeClass) {
	t.Helper()
	ctx := t.Context()
	_, err := store.CreateSaga(ctx, CreateSagaRequest{
		Namespace: "test", SagaID: sagaID, ExecutionID: sagaID + "-execution",
		PlanID: "plan", PlanDigest: "digest", Serial: true, AllowUnstableIdentityForTest: true,
	})
	require.NoError(t, err)
	charge, err := store.EnqueueStep(ctx, EnqueueStepRequest{
		SagaID: sagaID, EffectID: "charge", Sequence: 1, Verb: "charge", ContractHash: "charge-contract",
		Arguments: map[string]any{}, CompensationVerb: "undo-charge", CompensationContract: "undo-charge-contract",
		Fencing: []FencingRequirement{{Authority: "accounts", Resource: sagaID}},
	})
	require.NoError(t, err)
	claimed, err := store.ClaimDispatch(ctx, ClaimOptions{Owner: "previous", LeaseDuration: time.Minute, TargetDispatchID: charge.ID})
	require.NoError(t, err)
	require.NoError(t, store.SaveFencingGrants(ctx, claimed.ID, claimed.Attempt, claimed.LeaseToken,
		[]invocation.FencingGrant{{Authority: "accounts", Resource: sagaID, Token: 1}}))
	require.NoError(t, store.CompleteDispatch(ctx, Completion{
		DispatchID: claimed.ID, Attempt: claimed.Attempt, LeaseToken: claimed.LeaseToken,
		Outcome: invocation.OutcomeSuccess, Result: []byte(`true`),
	}))
	fail, err := store.EnqueueStep(ctx, EnqueueStepRequest{
		SagaID: sagaID, EffectID: "fail", Sequence: 2, Verb: "fail", ContractHash: "fail-contract", Arguments: map[string]any{},
	})
	require.NoError(t, err)
	claimed, err = store.ClaimDispatch(ctx, ClaimOptions{Owner: "previous", LeaseDuration: time.Minute, TargetDispatchID: fail.ID})
	require.NoError(t, err)
	require.NoError(t, store.CompleteDispatch(ctx, Completion{
		DispatchID: claimed.ID, Attempt: claimed.Attempt, LeaseToken: claimed.LeaseToken,
		Outcome: invocation.OutcomePermanentFailure, Error: "forward step failed",
	}))
	dispatches, err := store.ListDispatches(ctx, sagaID)
	require.NoError(t, err)
	var inverse *Dispatch
	for _, dispatch := range dispatches {
		if dispatch.Direction == invocation.DirectionCompensation {
			inverse = dispatch
			break
		}
	}
	require.NotNil(t, inverse)
	claimed, err = store.ClaimDispatch(ctx, ClaimOptions{Owner: "previous", LeaseDuration: time.Minute, TargetDispatchID: inverse.ID})
	require.NoError(t, err)
	require.NoError(t, store.SaveFencingGrants(ctx, claimed.ID, claimed.Attempt, claimed.LeaseToken,
		[]invocation.FencingGrant{{Authority: "accounts", Resource: sagaID, Token: 2}}))
	require.NoError(t, store.CompleteDispatch(ctx, Completion{
		DispatchID: claimed.ID, Attempt: claimed.Attempt, LeaseToken: claimed.LeaseToken,
		Outcome: previousOutcome, Error: "previous inverse result", NextAttemptAt: time.Now().Add(-time.Second),
	}))
	pending, err := store.GetDispatch(ctx, inverse.ID)
	require.NoError(t, err)
	require.Equal(t, DispatchRetryWait, pending.State)
	require.Equal(t, uint64(1), pending.Attempt)

	calls := 0
	provider := &countingFenceProvider{Provider: fencing.NewInMemoryProvider()}
	worker, err := NewDispatcher(store, provider, invocationExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		calls++
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	}), DispatcherOptions{Owner: "current", MaxAttempts: 1})
	require.NoError(t, err)
	closed, err := worker.Dispatch(ctx, inverse.ID)
	require.NoError(t, err)
	require.Zero(t, calls, "the exhausted inverse must not be invoked again")
	require.Zero(t, provider.acquisitions, "the exhausted inverse must not acquire another fence")
	require.Equal(t, uint64(2), closed.Attempt, "claim is recorded before the budget guard")
	require.Equal(t, inverse.IdempotencyKey, closed.IdempotencyKey)
	wantState := DispatchFailedPermanent
	if previousOutcome == invocation.OutcomeUnknown {
		wantState = DispatchBlockedUnknown
	}
	require.Equal(t, wantState, closed.State)
	saga, err := store.GetSaga(ctx, sagaID)
	require.NoError(t, err)
	require.Equal(t, SagaBlockedCompensation, saga.State)
	attempts, err := store.ListAttempts(ctx, inverse.ID)
	require.NoError(t, err)
	require.Len(t, attempts, 2)
	require.Equal(t, previousOutcome, attempts[0].Outcome)
	require.Equal(t, previousOutcome, attempts[1].Outcome)
	require.False(t, attempts[1].CompletedAt.IsZero())
	require.Empty(t, attempts[1].FencingGrants)
	_, err = worker.Dispatch(ctx, inverse.ID)
	require.ErrorIs(t, err, ErrNoDispatch)
	require.Zero(t, calls)
}

package schema

import (
	"context"
	"errors"
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
	for _, test := range []struct {
		name     string
		outcomes []invocation.OutcomeClass
	}{
		{"known", []invocation.OutcomeClass{invocation.OutcomeRetryableKnownNotCommitted}},
		{"unknown", []invocation.OutcomeClass{invocation.OutcomeUnknown}},
		{"earlier unknown followed by known", []invocation.OutcomeClass{invocation.OutcomeUnknown, invocation.OutcomeRetryableKnownNotCommitted}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertQueuedInverseAtAttemptCap(t, NewInMemoryOutboxStore(), "exhausted-inverse-"+test.name, test.outcomes)
		})
	}
}

func TestDispatcherPreservesEarlierUnknownAfterTerminalRetry(t *testing.T) {
	for _, outcome := range []invocation.OutcomeClass{
		invocation.OutcomeRetryableKnownNotCommitted,
		invocation.OutcomePermanentFailure,
	} {
		t.Run(string(outcome), func(t *testing.T) {
			assertTerminalRetryPreservesEarlierUnknown(t, NewInMemoryOutboxStore(), "mixed-outcomes-"+string(outcome), outcome)
		})
	}
}

func assertTerminalRetryPreservesEarlierUnknown(t *testing.T, store OutboxStore, sagaID string, finalOutcome invocation.OutcomeClass) {
	t.Helper()
	ctx := t.Context()
	_, err := store.CreateSaga(ctx, CreateSagaRequest{
		Namespace: "test", SagaID: sagaID, ExecutionID: sagaID + "-execution",
		PlanID: "plan", PlanDigest: "digest", Serial: true, AllowUnstableIdentityForTest: true,
	})
	require.NoError(t, err)
	dispatch, err := store.EnqueueStep(ctx, EnqueueStepRequest{
		SagaID: sagaID, EffectID: "effect", Sequence: 1, Verb: "verb", ContractHash: "contract", Arguments: map[string]any{},
	})
	require.NoError(t, err)
	claimed, err := store.ClaimDispatch(ctx, ClaimOptions{Owner: "previous", LeaseDuration: time.Minute, TargetDispatchID: dispatch.ID})
	require.NoError(t, err)
	require.NoError(t, store.CompleteDispatch(ctx, Completion{
		DispatchID: claimed.ID, Attempt: claimed.Attempt, LeaseToken: claimed.LeaseToken,
		Outcome: invocation.OutcomeUnknown, Error: "first response lost", NextAttemptAt: time.Now().Add(-time.Second),
	}))
	calls := 0
	worker, err := NewDispatcher(store, nil, invocationExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		calls++
		return invocation.Outcome{Class: finalOutcome, Err: errors.New("last attempt did not commit")}
	}), DispatcherOptions{Owner: "current", MaxAttempts: 2})
	require.NoError(t, err)
	closed, err := worker.Dispatch(ctx, dispatch.ID)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, DispatchBlockedUnknown, closed.State)
	require.Equal(t, uint64(2), closed.Attempt)
	saga, err := store.GetSaga(ctx, sagaID)
	require.NoError(t, err)
	require.Equal(t, SagaBlockedUnknown, saga.State)
	attempts, err := store.ListAttempts(ctx, dispatch.ID)
	require.NoError(t, err)
	require.Len(t, attempts, 2)
	require.Equal(t, invocation.OutcomeUnknown, attempts[0].Outcome)
	require.Equal(t, finalOutcome, attempts[1].Outcome, "the audit record keeps the current attempt's observed outcome")
	require.False(t, attempts[1].CompletedAt.IsZero())
	_, err = worker.Dispatch(ctx, dispatch.ID)
	require.ErrorIs(t, err, ErrNoDispatch)
}

// Model a retry_wait row left by a worker that used the failed forward step's
// larger budget. A later worker must honor the inverse's smaller budget.
func assertQueuedInverseAtAttemptCap(t *testing.T, store OutboxStore, sagaID string, previousOutcomes []invocation.OutcomeClass) {
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
	for index, outcome := range previousOutcomes {
		claimed, err = store.ClaimDispatch(ctx, ClaimOptions{Owner: "previous", LeaseDuration: time.Minute, TargetDispatchID: inverse.ID})
		require.NoError(t, err)
		require.NoError(t, store.SaveFencingGrants(ctx, claimed.ID, claimed.Attempt, claimed.LeaseToken,
			[]invocation.FencingGrant{{Authority: "accounts", Resource: sagaID, Token: uint64(index + 2)}}))
		require.NoError(t, store.CompleteDispatch(ctx, Completion{
			DispatchID: claimed.ID, Attempt: claimed.Attempt, LeaseToken: claimed.LeaseToken,
			Outcome: outcome, Error: "previous inverse result", NextAttemptAt: time.Now().Add(-time.Second),
		}))
	}
	pending, err := store.GetDispatch(ctx, inverse.ID)
	require.NoError(t, err)
	require.Equal(t, DispatchRetryWait, pending.State)
	require.Equal(t, uint64(len(previousOutcomes)), pending.Attempt)

	calls := 0
	provider := &countingFenceProvider{Provider: fencing.NewInMemoryProvider()}
	worker, err := NewDispatcher(store, provider, invocationExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		calls++
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	}), DispatcherOptions{Owner: "current", MaxAttempts: uint64(len(previousOutcomes))})
	require.NoError(t, err)
	closed, err := worker.Dispatch(ctx, inverse.ID)
	require.NoError(t, err)
	require.Zero(t, calls, "the exhausted inverse must not be invoked again")
	require.Zero(t, provider.acquisitions, "the exhausted inverse must not acquire another fence")
	require.Equal(t, uint64(len(previousOutcomes)+1), closed.Attempt, "claim is recorded before the budget guard")
	require.Equal(t, inverse.IdempotencyKey, closed.IdempotencyKey)
	wantState := DispatchFailedPermanent
	for _, outcome := range previousOutcomes {
		if outcome != invocation.OutcomeRetryableKnownNotCommitted {
			wantState = DispatchBlockedUnknown
		}
	}
	require.Equal(t, wantState, closed.State)
	saga, err := store.GetSaga(ctx, sagaID)
	require.NoError(t, err)
	require.Equal(t, SagaBlockedCompensation, saga.State)
	attempts, err := store.ListAttempts(ctx, inverse.ID)
	require.NoError(t, err)
	require.Len(t, attempts, len(previousOutcomes)+1)
	for index, outcome := range previousOutcomes {
		require.Equal(t, outcome, attempts[index].Outcome)
	}
	wantOutcome := invocation.OutcomeRetryableKnownNotCommitted
	if wantState == DispatchBlockedUnknown {
		wantOutcome = invocation.OutcomeUnknown
	}
	require.Equal(t, wantOutcome, attempts[len(previousOutcomes)].Outcome)
	require.False(t, attempts[len(previousOutcomes)].CompletedAt.IsZero())
	require.Empty(t, attempts[len(previousOutcomes)].FencingGrants)
	_, err = worker.Dispatch(ctx, inverse.ID)
	require.ErrorIs(t, err, ErrNoDispatch)
	require.Zero(t, calls)
}

package schema

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/effectus/effectus-go/invocation"
	"github.com/effectus/effectus-go/schema/fencing"
	"github.com/stretchr/testify/require"
)

type invocationExecutorFunc func(context.Context, invocation.Request) invocation.Outcome

func (function invocationExecutorFunc) Invoke(ctx context.Context, request invocation.Request) invocation.Outcome {
	return function(ctx, request)
}

type sinkIdempotentExecutor struct{ invocationExecutorFunc }

func (sinkIdempotentExecutor) RetryUnknownOutcome(invocation.Request) bool { return true }

func createOutboxSaga(t *testing.T, store OutboxStore, sagaID string) {
	t.Helper()
	_, err := store.CreateSaga(t.Context(), CreateSagaRequest{
		Namespace: "test", SagaID: sagaID, ExecutionID: "execution-1",
		PlanID: "plan-1", PlanDigest: "plan-digest", Serial: true, AllowUnstableIdentityForTest: true,
	})
	require.NoError(t, err)
}

func enqueueOutboxStep(t *testing.T, store OutboxStore, sagaID, effectID, verb string, sequence int) *Dispatch {
	t.Helper()
	dispatch, err := store.EnqueueStep(t.Context(), EnqueueStepRequest{
		SagaID: sagaID, EffectID: effectID, Sequence: sequence, Verb: verb,
		ContractHash: "contract-" + verb, Arguments: map[string]any{"id": effectID},
		CompensationVerb: "undo-" + verb, CompensationContract: "contract-undo-" + verb,
		Fencing: []FencingRequirement{{Authority: "accounts", Resource: effectID}},
	})
	require.NoError(t, err)
	return dispatch
}

func TestOutboxRejectsNonSerialSagaUntilLateSuccessIsAtomic(t *testing.T) {
	_, err := NewInMemoryOutboxStore().CreateSaga(t.Context(), CreateSagaRequest{
		Namespace: "test", SagaID: "parallel", ExecutionID: "execution",
		PlanID: "plan", PlanDigest: "digest", Serial: false,
	})
	require.ErrorContains(t, err, "non-serial durable sagas are not supported")
}

func TestStableSagaIdentitiesUseUnambiguousComponents(t *testing.T) {
	require.NotEqual(t,
		StableExecutionID("ab", "c", "rules", "1"),
		StableExecutionID("a", "bc", "rules", "1"),
	)
	executionID := StableExecutionID("tenant", "delivery", "rules", "1")
	require.Equal(t, 64, len(executionID))
	require.Equal(t, StableSagaID(executionID, "plan"), StableSagaID(executionID, "plan"))
	key := IdempotencyKey("tenant", "saga", "effect", invocation.DirectionForward)
	require.Equal(t, key, IdempotencyKey("tenant", "saga", "effect", invocation.DirectionForward))
	require.NotEqual(t, key, IdempotencyKey("tenant", "saga", "effect", invocation.DirectionCompensation))
}

func TestOutboxIntentPrecedesInvocationAndCarriesFencing(t *testing.T) {
	store := NewInMemoryOutboxStore()
	createOutboxSaga(t, store, "saga-intent")
	dispatch := enqueueOutboxStep(t, store, "saga-intent", "effect-1", "charge", 1)
	require.Equal(t, DispatchQueued, dispatch.State)

	calls := 0
	executor := invocationExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
		calls++
		require.Equal(t, dispatch.IdempotencyKey, request.Metadata.Saga.IdempotencyKey)
		require.Equal(t, uint64(1), request.Metadata.Saga.Attempt)
		require.Equal(t, invocation.DirectionForward, request.Metadata.Saga.Direction)
		require.Equal(t, "execution-1", request.Metadata.ExecutionID)
		require.Equal(t, "contract-charge", request.ContractHash)
		require.Equal(t, "effect-1", request.Arguments["id"])
		require.Equal(t, []invocation.FencingGrant{{Authority: "accounts", Resource: "effect-1", Token: 1}}, request.Metadata.FencingGrants)
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: map[string]any{"receipt": "one"}}
	})
	worker, err := NewDispatcher(store, fencing.NewInMemoryProvider(), executor, DispatcherOptions{Owner: "worker-1"})
	require.NoError(t, err)
	require.Equal(t, 0, calls, "enqueue must not invoke the destination")

	completed, err := worker.DispatchOne(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, DispatchSucceeded, completed.State)
	attempts, err := store.ListAttempts(t.Context(), dispatch.ID)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, invocation.OutcomeSuccess, attempts[0].Outcome)
	require.Equal(t, uint64(1), attempts[0].FencingGrants[0].Token)
}

func TestOutboxLeaseExpiryRejectsStaleCompletion(t *testing.T) {
	store := NewInMemoryOutboxStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	createOutboxSaga(t, store, "saga-lease")
	enqueueOutboxStep(t, store, "saga-lease", "effect-1", "charge", 1)

	first, err := store.ClaimDispatch(t.Context(), ClaimOptions{Owner: "old", LeaseDuration: time.Second, Now: now})
	require.NoError(t, err)
	now = now.Add(2 * time.Second)
	err = store.CompleteDispatch(t.Context(), Completion{
		DispatchID: first.ID, Attempt: first.Attempt, LeaseToken: first.LeaseToken,
		Outcome: invocation.OutcomeSuccess, Result: []byte(`{"receipt":"expired"}`), Now: now,
	})
	require.ErrorIs(t, err, ErrStaleLease)
	second, err := store.ClaimDispatch(t.Context(), ClaimOptions{Owner: "new", LeaseDuration: time.Second, Now: now})
	require.NoError(t, err)
	require.Equal(t, uint64(2), second.Attempt)
	require.Equal(t, first.IdempotencyKey, second.IdempotencyKey)
	require.NotEqual(t, first.LeaseToken, second.LeaseToken)

	err = store.CompleteDispatch(t.Context(), Completion{
		DispatchID: first.ID, Attempt: first.Attempt, LeaseToken: first.LeaseToken,
		Outcome: invocation.OutcomeSuccess, Result: []byte(`{"receipt":"stale"}`), Now: now,
	})
	require.ErrorIs(t, err, ErrStaleLease)
	require.NoError(t, store.CompleteDispatch(t.Context(), Completion{
		DispatchID: second.ID, Attempt: second.Attempt, LeaseToken: second.LeaseToken,
		Outcome: invocation.OutcomeSuccess, Result: []byte(`{"receipt":"current"}`), Now: now,
	}))
	err = store.CompleteDispatch(t.Context(), Completion{
		DispatchID: second.ID, Attempt: second.Attempt, LeaseToken: second.LeaseToken,
		Outcome: invocation.OutcomePermanentFailure, Error: "late failure", Now: now,
	})
	require.ErrorIs(t, err, ErrStaleLease, "success must be irreversible")
}

func TestOutboxUnknownOutcomeRetriesStableIdentityWithoutCompensation(t *testing.T) {
	store := NewInMemoryOutboxStore()
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	createOutboxSaga(t, store, "saga-unknown")
	dispatch := enqueueOutboxStep(t, store, "saga-unknown", "effect-1", "charge", 1)

	var requests []invocation.Request
	executor := sinkIdempotentExecutor{invocationExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
		requests = append(requests, request)
		return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: errors.New("connection reset after send")}
	})}
	worker, err := NewDispatcher(store, fencing.NewInMemoryProvider(), executor, DispatcherOptions{
		Owner: "worker", MaxAttempts: 2, InitialBackoff: time.Second, MaxBackoff: time.Second,
	})
	require.NoError(t, err)
	worker.now = func() time.Time { return now }
	first, err := worker.DispatchOne(t.Context())
	require.NoError(t, err)
	require.Equal(t, DispatchRetryWait, first.State)
	now = first.NextAttemptAt.Add(time.Millisecond)
	second, err := worker.DispatchOne(t.Context())
	require.NoError(t, err)
	require.Equal(t, DispatchBlockedUnknown, second.State)
	require.Len(t, requests, 2)
	require.Equal(t, dispatch.IdempotencyKey, requests[0].Metadata.Saga.IdempotencyKey)
	require.Equal(t, requests[0].Metadata.Saga.IdempotencyKey, requests[1].Metadata.Saga.IdempotencyKey)
	require.Equal(t, uint64(1), requests[0].Metadata.Saga.Attempt)
	require.Equal(t, uint64(2), requests[1].Metadata.Saga.Attempt)

	saga, err := store.GetSaga(t.Context(), "saga-unknown")
	require.NoError(t, err)
	require.Equal(t, SagaBlockedUnknown, saga.State)
	dispatches, err := store.ListDispatches(t.Context(), saga.SagaID)
	require.NoError(t, err)
	require.Len(t, dispatches, 1, "unknown forward outcomes must not enqueue compensation")
}

func TestOutboxCompensationIsDurableAndReverseOrdered(t *testing.T) {
	store := NewInMemoryOutboxStore()
	createOutboxSaga(t, store, "saga-compensate")
	enqueueOutboxStep(t, store, "saga-compensate", "effect-1", "first", 1)
	enqueueOutboxStep(t, store, "saga-compensate", "effect-2", "second", 2)
	enqueueOutboxStep(t, store, "saga-compensate", "effect-3", "fail", 3)

	var trace []string
	executor := invocationExecutorFunc(func(_ context.Context, request invocation.Request) invocation.Outcome {
		trace = append(trace, string(request.Metadata.Saga.Direction)+":"+request.Metadata.Saga.EffectID)
		if request.Verb == "fail" {
			return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("declined")}
		}
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: map[string]any{"ok": true}}
	})
	worker, err := NewDispatcher(store, fencing.NewInMemoryProvider(), executor, DispatcherOptions{Owner: "worker"})
	require.NoError(t, err)
	for range 3 {
		_, err := worker.DispatchOne(t.Context())
		require.NoError(t, err)
	}
	afterFailure, err := store.ListDispatches(t.Context(), "saga-compensate")
	require.NoError(t, err)
	require.Len(t, afterFailure, 4, "permanent failure must atomically enqueue the first compensation")
	_, err = worker.DispatchOne(t.Context())
	require.NoError(t, err)
	afterFirstCompensation, err := store.ListDispatches(t.Context(), "saga-compensate")
	require.NoError(t, err)
	require.Len(t, afterFirstCompensation, 5, "compensation success must atomically enqueue the next compensation")
	_, err = worker.DispatchOne(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{
		"forward:effect-1", "forward:effect-2", "forward:effect-3",
		"compensation:effect-2", "compensation:effect-1",
	}, trace)
	saga, err := store.GetSaga(t.Context(), "saga-compensate")
	require.NoError(t, err)
	require.Equal(t, SagaCompensated, saga.State)
	dispatches, err := store.ListDispatches(t.Context(), saga.SagaID)
	require.NoError(t, err)
	require.Len(t, dispatches, 5)
	for _, dispatch := range dispatches {
		if dispatch.Direction == invocation.DirectionCompensation {
			require.Equal(t, DispatchSucceeded, dispatch.State)
		}
	}
}

func TestOutboxIdentityConflictAndTerminalReopen(t *testing.T) {
	store := NewInMemoryOutboxStore()
	createOutboxSaga(t, store, "saga-replay")
	first := enqueueOutboxStep(t, store, "saga-replay", "effect-1", "charge", 1)
	replayed := enqueueOutboxStep(t, store, "saga-replay", "effect-1", "charge", 1)
	require.Equal(t, first.ID, replayed.ID)
	err := store.CompleteSaga(t.Context(), "saga-replay")
	require.ErrorIs(t, err, ErrInvalidTransition)
	_, err = store.EnqueueStep(t.Context(), EnqueueStepRequest{
		SagaID: "saga-replay", EffectID: "effect-1", Sequence: 1, Verb: "charge",
		ContractHash: "contract-charge", Arguments: map[string]any{"id": "changed"},
		CompensationVerb: "undo-charge", CompensationContract: "contract-undo-charge",
		Fencing: []FencingRequirement{{Authority: "accounts", Resource: "effect-1"}},
	})
	require.ErrorIs(t, err, ErrIdentityConflict)

	claimed, err := store.ClaimDispatch(t.Context(), ClaimOptions{Owner: "worker", LeaseDuration: time.Second})
	require.NoError(t, err)
	require.NoError(t, store.SaveFencingGrants(t.Context(), claimed.ID, claimed.Attempt, claimed.LeaseToken,
		[]invocation.FencingGrant{{Authority: "accounts", Resource: "effect-1", Token: 1}}))
	require.NoError(t, store.CompleteDispatch(t.Context(), Completion{
		DispatchID: claimed.ID, Attempt: claimed.Attempt, LeaseToken: claimed.LeaseToken,
		Outcome: invocation.OutcomeSuccess, Result: []byte(`null`),
	}))
	require.NoError(t, store.CompleteSaga(t.Context(), "saga-replay"))
	_, err = store.EnqueueStep(t.Context(), EnqueueStepRequest{
		SagaID: "saga-replay", EffectID: "effect-2", Sequence: 2, Verb: "other",
		ContractHash: "contract-other", Arguments: map[string]any{},
	})
	require.ErrorIs(t, err, ErrTerminalSaga)
	_, err = store.CreateSaga(t.Context(), CreateSagaRequest{
		Namespace: "changed", SagaID: "saga-replay", ExecutionID: "execution-1",
		PlanID: "plan-1", PlanDigest: "plan-digest", Serial: true, AllowUnstableIdentityForTest: true,
	})
	require.ErrorIs(t, err, ErrIdentityConflict)
}

func TestDispatcherRejectsLocalProviderWhenDurableFencingIsRequired(t *testing.T) {
	_, err := NewDispatcher(
		NewInMemoryOutboxStore(),
		fencing.NewInMemoryProvider(),
		invocationExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome { return invocation.Outcome{} }),
		DispatcherOptions{Owner: "worker", RequireDurableFencing: true},
	)
	require.ErrorContains(t, err, "not durable_monotonic")
}

func TestDispatcherReleasesPartialFencingAcquisition(t *testing.T) {
	store := NewInMemoryOutboxStore()
	createOutboxSaga(t, store, "saga-partial-fence")
	_, err := store.EnqueueStep(t.Context(), EnqueueStepRequest{
		SagaID: "saga-partial-fence", EffectID: "effect-1", Sequence: 1, Verb: "charge",
		ContractHash: "contract", Arguments: map[string]any{},
		Fencing: []FencingRequirement{
			{Authority: "db", Resource: "a"},
			{Authority: "db", Resource: "b"},
		},
	})
	require.NoError(t, err)
	provider := fencing.NewInMemoryProvider()
	held, err := provider.Acquire(t.Context(), fencing.Request{Authority: "db", Resource: "b", Holder: "other", TTL: time.Minute})
	require.NoError(t, err)
	worker, err := NewDispatcher(store, provider, invocationExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		t.Fatal("external invocation must not occur after partial fencing acquisition")
		return invocation.Outcome{}
	}), DispatcherOptions{Owner: "worker"})
	require.NoError(t, err)
	_, err = worker.DispatchOne(t.Context())
	require.ErrorIs(t, err, fencing.ErrLeaseHeld)
	probe, err := provider.Acquire(t.Context(), fencing.Request{Authority: "db", Resource: "a", Holder: "probe", TTL: time.Minute})
	require.NoError(t, err, "the first partial lease must be released")
	require.NoError(t, probe.Release(t.Context()))
	require.NoError(t, held.Release(t.Context()))
}

func TestDispatcherBlocksUnserializableSuccessfulResultAsUnknown(t *testing.T) {
	store := NewInMemoryOutboxStore()
	createOutboxSaga(t, store, "saga-invalid-result")
	enqueueOutboxStep(t, store, "saga-invalid-result", "effect-1", "charge", 1)
	worker, err := NewDispatcher(store, fencing.NewInMemoryProvider(), invocationExecutorFunc(func(_ context.Context, _ invocation.Request) invocation.Outcome {
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: make(chan int)}
	}), DispatcherOptions{Owner: "worker", MaxAttempts: 1})
	require.NoError(t, err)
	_, err = worker.DispatchOne(t.Context())
	require.ErrorContains(t, err, "not serializable")
	saga, getErr := store.GetSaga(t.Context(), "saga-invalid-result")
	require.NoError(t, getErr)
	require.Equal(t, SagaBlockedUnknown, saga.State)
}

func TestOutboxStaleFenceBlocksWithoutGenericRetry(t *testing.T) {
	store := NewInMemoryOutboxStore()
	createOutboxSaga(t, store, "saga-stale-fence")
	enqueueOutboxStep(t, store, "saga-stale-fence", "effect-1", "charge", 1)
	calls := 0
	executor := invocationExecutorFunc(func(_ context.Context, _ invocation.Request) invocation.Outcome {
		calls++
		return invocation.Outcome{Class: invocation.OutcomeStaleFence, Err: errors.New("sink rejected stale fence")}
	})
	worker, err := NewDispatcher(store, fencing.NewInMemoryProvider(), executor, DispatcherOptions{Owner: "worker"})
	require.NoError(t, err)
	dispatch, err := worker.DispatchOne(t.Context())
	require.NoError(t, err)
	require.Equal(t, DispatchBlockedFence, dispatch.State)
	saga, err := store.GetSaga(t.Context(), "saga-stale-fence")
	require.NoError(t, err)
	require.Equal(t, SagaBlockedFence, saga.State)
	_, err = worker.DispatchOne(t.Context())
	require.ErrorIs(t, err, ErrNoDispatch)
	require.Equal(t, 1, calls)
}

func TestOutboxSerialSagaAllowsOneCurrentClaim(t *testing.T) {
	store := NewInMemoryOutboxStore()
	createOutboxSaga(t, store, "saga-serial")
	enqueueOutboxStep(t, store, "saga-serial", "effect-1", "first", 1)
	enqueueOutboxStep(t, store, "saga-serial", "effect-2", "second", 2)
	_, err := store.ClaimDispatch(t.Context(), ClaimOptions{Owner: "one", LeaseDuration: time.Minute})
	require.NoError(t, err)
	_, err = store.ClaimDispatch(t.Context(), ClaimOptions{Owner: "two", LeaseDuration: time.Minute})
	require.ErrorIs(t, err, ErrNoDispatch)
}

func TestOutboxConcurrentClaimHasOneWinner(t *testing.T) {
	store := NewInMemoryOutboxStore()
	createOutboxSaga(t, store, "saga-race")
	enqueueOutboxStep(t, store, "saga-race", "effect-1", "charge", 1)
	var wait sync.WaitGroup
	wait.Add(2)
	results := make(chan error, 2)
	for _, owner := range []string{"one", "two"} {
		owner := owner
		go func() {
			defer wait.Done()
			_, err := store.ClaimDispatch(context.Background(), ClaimOptions{Owner: owner, LeaseDuration: time.Minute})
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	var successes, empty int
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrNoDispatch) {
			empty++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, empty)
}

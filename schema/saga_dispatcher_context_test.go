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

func TestDispatcherRecordsOutcomeAfterCallerCancellation(t *testing.T) {
	for _, outcome := range []invocation.Outcome{
		{Class: invocation.OutcomeSuccess, Result: map[string]any{"receipt": "committed"}},
		{Class: invocation.OutcomeUnknown, Err: errors.New("ack lost")},
		{Class: invocation.OutcomePermanentFailure, Err: errors.New("declined")},
	} {
		t.Run(string(outcome.Class), func(t *testing.T) {
			store := NewInMemoryOutboxStore()
			createOutboxSaga(t, store, "cancel")
			dispatch := enqueueOutboxStep(t, store, "cancel", "effect", "charge", 1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			executor := invocationExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
				cancel()
				return outcome
			})
			worker, err := NewDispatcher(store, fencing.NewInMemoryProvider(), executor, DispatcherOptions{Owner: "worker"})
			require.NoError(t, err)
			_, err = worker.DispatchOne(ctx)
			require.NoError(t, err)
			attempts, err := store.ListAttempts(t.Context(), dispatch.ID)
			require.NoError(t, err)
			require.Len(t, attempts, 1)
			require.Equal(t, outcome.Class, attempts[0].Outcome)
			require.False(t, attempts[0].CompletedAt.IsZero())
			stored, err := store.GetDispatch(t.Context(), dispatch.ID)
			require.NoError(t, err)
			require.NotEqual(t, DispatchInFlight, stored.State)
		})
	}
}

func TestDispatcherInvalidOutcomeBlocksWithoutAutomaticRetry(t *testing.T) {
	store := NewInMemoryOutboxStore()
	createOutboxSaga(t, store, "invalid")
	dispatch := enqueueOutboxStep(t, store, "invalid", "effect", "charge", 1)
	worker, err := NewDispatcher(store, fencing.NewInMemoryProvider(), invocationExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: make(chan int)}
	}), DispatcherOptions{Owner: "worker"})
	require.NoError(t, err)
	_, err = worker.DispatchOne(t.Context())
	require.ErrorContains(t, err, "not serializable")
	stored, err := store.GetDispatch(t.Context(), dispatch.ID)
	require.NoError(t, err)
	require.Equal(t, DispatchBlockedUnknown, stored.State)
}

func TestDispatcherCannotCompleteAfterLeaseExpiry(t *testing.T) {
	store := NewInMemoryOutboxStore()
	now := time.Now()
	store.now = func() time.Time { return now }
	createOutboxSaga(t, store, "expired")
	dispatch := enqueueOutboxStep(t, store, "expired", "effect", "charge", 1)
	worker, err := NewDispatcher(store, fencing.NewInMemoryProvider(), invocationExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		now = now.Add(time.Minute)
		return invocation.Outcome{Class: invocation.OutcomeSuccess}
	}), DispatcherOptions{Owner: "worker", LeaseDuration: time.Second})
	require.NoError(t, err)
	worker.now = func() time.Time { return now }
	_, err = worker.DispatchOne(t.Context())
	require.Error(t, err)
	stored, err := store.GetDispatch(t.Context(), dispatch.ID)
	require.NoError(t, err)
	require.Equal(t, DispatchInFlight, stored.State)
	attempts, err := store.ListAttempts(t.Context(), dispatch.ID)
	require.NoError(t, err)
	require.True(t, attempts[0].CompletedAt.IsZero())
}

type failedCompletionStore struct {
	OutboxStore
	err error
}

func (store failedCompletionStore) CompleteDispatch(context.Context, Completion) error {
	return store.err
}

func TestDispatcherCompletionFailureRemainsExplicit(t *testing.T) {
	store := NewInMemoryOutboxStore()
	createOutboxSaga(t, store, "storage")
	dispatch := enqueueOutboxStep(t, store, "storage", "effect", "charge", 1)
	unavailable := errors.New("completion storage unavailable")
	worker, err := NewDispatcher(failedCompletionStore{store, unavailable}, fencing.NewInMemoryProvider(), invocationExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		return invocation.Outcome{Class: invocation.OutcomeSuccess}
	}), DispatcherOptions{Owner: "worker"})
	require.NoError(t, err)
	_, err = worker.DispatchOne(t.Context())
	require.ErrorIs(t, err, unavailable)
	stored, err := store.GetDispatch(t.Context(), dispatch.ID)
	require.NoError(t, err)
	require.Equal(t, DispatchInFlight, stored.State, "a failed durable write cannot be reported as committed")
}

type skewedDispatchStore struct {
	OutboxStore
	skew time.Duration
}

func (s skewedDispatchStore) ClaimDispatch(ctx context.Context, options ClaimOptions) (*Dispatch, error) {
	dispatch, err := s.OutboxStore.ClaimDispatch(ctx, options)
	if err == nil {
		dispatch.LeaseDeadline = dispatch.LeaseDeadline.Add(s.skew)
	}
	return dispatch, err
}

func TestDispatcherDoesNotScheduleFromDatabaseWallClock(t *testing.T) {
	for _, skew := range []time.Duration{-24 * time.Hour, 24 * time.Hour} {
		t.Run(skew.String(), func(t *testing.T) {
			store := NewInMemoryOutboxStore()
			createOutboxSaga(t, store, "clock")
			enqueueOutboxStep(t, store, "clock", "effect", "charge", 1)
			calls := 0
			worker, err := NewDispatcher(skewedDispatchStore{store, skew}, fencing.NewInMemoryProvider(), invocationExecutorFunc(func(ctx context.Context, request invocation.Request) invocation.Outcome {
				calls++
				require.NoError(t, ctx.Err())
				require.Positive(t, time.Until(request.Metadata.Deadline))
				require.Less(t, time.Until(request.Metadata.Deadline), time.Second)
				return invocation.Outcome{Class: invocation.OutcomeSuccess}
			}), DispatcherOptions{Owner: "worker", LeaseDuration: time.Second})
			require.NoError(t, err)
			_, err = worker.DispatchOne(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, calls)
		})
	}
}

func TestDispatcherDefaultAndValidOptions(t *testing.T) {
	for _, options := range []DispatcherOptions{
		{Owner: "worker"},
		{Owner: "worker", LeaseDuration: time.Second, InvocationTimeout: 500 * time.Millisecond, MaxAttempts: 2, InitialBackoff: time.Millisecond, MaxBackoff: time.Second},
	} {
		worker, err := NewDispatcher(NewInMemoryOutboxStore(), nil, invocationExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
			return invocation.Outcome{Class: invocation.OutcomeSuccess}
		}), options)
		require.NoError(t, err)
		if options.LeaseDuration == 0 {
			require.Equal(t, 30*time.Second, worker.options.LeaseDuration)
			require.Equal(t, 22500*time.Millisecond, worker.options.InvocationTimeout)
			require.Equal(t, uint64(8), worker.options.MaxAttempts)
			require.Equal(t, time.Second, worker.options.InitialBackoff)
			require.Equal(t, time.Minute, worker.options.MaxBackoff)
		} else {
			require.Equal(t, options, worker.options)
		}
		for _, attempt := range []uint64{1, 20, ^uint64(0)} {
			delay := worker.backoff(&Dispatch{Attempt: attempt, IdempotencyKey: "jitter"})
			require.Positive(t, delay)
			require.LessOrEqual(t, delay, worker.options.MaxBackoff)
		}
	}
}

func TestDispatcherRejectsInvalidOptions(t *testing.T) {
	for _, options := range []DispatcherOptions{
		{Owner: "worker", LeaseDuration: -1},
		{Owner: "worker", InvocationTimeout: -1},
		{Owner: "worker", InitialBackoff: -1},
		{Owner: "worker", MaxBackoff: -1},
		{Owner: "worker", LeaseDuration: time.Second, InvocationTimeout: time.Second},
		{Owner: "worker", InitialBackoff: time.Second, MaxBackoff: time.Millisecond},
	} {
		_, err := NewDispatcher(NewInMemoryOutboxStore(), nil, invocationExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
			return invocation.Outcome{Class: invocation.OutcomeSuccess}
		}), options)
		require.Error(t, err, "%+v", options)
	}
}

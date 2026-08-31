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

type deduplicatingFenceSink struct {
	ledger       map[string]sinkRecord
	watermarks   map[string]uint64
	mutations    int
	loseFirstAck bool
}

type sinkRecord struct {
	argumentHash string
	result       any
}

func newDeduplicatingFenceSink() *deduplicatingFenceSink {
	return &deduplicatingFenceSink{
		ledger: make(map[string]sinkRecord), watermarks: make(map[string]uint64), loseFirstAck: true,
	}
}

func (*deduplicatingFenceSink) RetryUnknownOutcome(invocation.Request) bool { return true }

func (sink *deduplicatingFenceSink) Invoke(_ context.Context, request invocation.Request) invocation.Outcome {
	grant := request.Metadata.FencingGrants[0]
	resource := grant.Authority + "/" + grant.Resource
	if grant.Token < sink.watermarks[resource] {
		return invocation.Outcome{Class: invocation.OutcomeStaleFence, Err: errors.New("stale fence")}
	}
	sink.watermarks[resource] = grant.Token
	key := request.Metadata.Saga.IdempotencyKey
	if recorded, ok := sink.ledger[key]; ok {
		if recorded.argumentHash != request.ArgumentHash {
			return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: errors.New("idempotency payload conflict")}
		}
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: recorded.result}
	}
	sink.mutations++
	result := map[string]any{"receipt": "committed"}
	sink.ledger[key] = sinkRecord{argumentHash: request.ArgumentHash, result: result}
	if sink.loseFirstAck {
		sink.loseFirstAck = false
		return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: errors.New("connection lost after destination commit")}
	}
	return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: result}
}

func TestDestinationCommitThenConnectionLossReplaysStableKey(t *testing.T) {
	store := NewInMemoryOutboxStore()
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	createOutboxSaga(t, store, "saga-crash-window")
	dispatch := enqueueOutboxStep(t, store, "saga-crash-window", "effect-1", "charge", 1)
	sink := newDeduplicatingFenceSink()
	worker, err := NewDispatcher(store, fencing.NewInMemoryProvider(), sink, DispatcherOptions{
		Owner: "worker", MaxAttempts: 3, InitialBackoff: time.Second, MaxBackoff: time.Second,
	})
	require.NoError(t, err)
	worker.now = func() time.Time { return now }

	first, err := worker.DispatchOne(t.Context())
	require.NoError(t, err)
	require.Equal(t, DispatchRetryWait, first.State)
	now = first.NextAttemptAt.Add(time.Millisecond)
	second, err := worker.DispatchOne(t.Context())
	require.NoError(t, err)
	require.Equal(t, DispatchSucceeded, second.State)
	require.Equal(t, 1, sink.mutations, "destination deduplication must prevent a second business mutation")
	require.Equal(t, dispatch.IdempotencyKey, second.IdempotencyKey)
	require.Equal(t, uint64(2), second.Attempt)

	attempts, err := store.ListAttempts(t.Context(), dispatch.ID)
	require.NoError(t, err)
	require.Len(t, attempts, 2)
	require.Equal(t, invocation.OutcomeUnknown, attempts[0].Outcome)
	require.Equal(t, invocation.OutcomeSuccess, attempts[1].Outcome)
	require.Greater(t, attempts[1].FencingGrants[0].Token, attempts[0].FencingGrants[0].Token)
}

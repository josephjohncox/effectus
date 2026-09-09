package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/ledger"
	"github.com/stretchr/testify/require"
)

// Embedding the interface deliberately does not expose optional renewal.
type delayedNonrenewingClaimLedger struct {
	ledger.ExecutionLedger
}

func (store delayedNonrenewingClaimLedger) LeaseExecutions(ctx context.Context, owner string, limit int, duration time.Duration) ([]ledger.ExecutionLease, error) {
	// Delay before the server grants the lease. Its fresh deadline must not
	// reset the worker's conservative window measured before this RPC.
	timer := time.NewTimer(2 * duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return store.ExecutionLedger.LeaseExecutions(ctx, owner, limit, duration)
	}
}

type recoveryClaimObserver struct {
	observations []RecoveryObservation
}

func (*recoveryClaimObserver) ObserveExecution(ExecuteResult, error) {}

func (observer *recoveryClaimObserver) ObserveRecovery(observation RecoveryObservation) {
	observer.observations = append(observer.observations, observation)
}

func TestRecoveryNonrenewingClaimLatencyConsumesOriginalSafeWindow(t *testing.T) {
	durable := schema.NewInMemoryExecutionLedger()
	store := delayedNonrenewingClaimLedger{ExecutionLedger: durable}
	_, canRenew := any(store).(ledger.ExecutionLeaseRenewer)
	require.False(t, canRenew)
	var calls atomic.Int32
	engine := recoveryFixture(t, store, 1, recoveryExecutorFunc(func(context.Context, invocation.Request) invocation.Outcome {
		calls.Add(1)
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
	}))
	observer := &recoveryClaimObserver{}
	worker := &RecoveryWorker{
		Engine: engine, Store: store, Owner: "delayed", BatchSize: 1, LeaseDuration: 100 * time.Millisecond,
		Observer: observer,
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	processed, err := worker.RunOnce(ctx)
	require.NoError(t, err)
	require.NoError(t, ctx.Err(), "the caller itself did not expire")
	require.Equal(t, 1, processed)
	require.Zero(t, calls.Load(), "claim latency must not buy a new execution window")
	require.Len(t, observer.observations, 1)
	require.ErrorIs(t, observer.observations[0].Err, context.DeadlineExceeded)
	record, err := durable.GetExecution(ctx, "item-0")
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionAccepted, record.State)
	require.Equal(t, "delayed", record.RecoveryOwner)
	require.NotEmpty(t, record.RecoveryToken)

	// Wait for actual expiry, then reclaim normally. Do not backdate a row or
	// return a synthetic lease deadline to make the execution eligible.
	timer := time.NewTimer(max(time.Until(record.RecoveryDeadline), 0) + time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}
	leases, err := durable.LeaseExecutions(ctx, "next", 1, time.Second)
	require.NoError(t, err)
	require.Len(t, leases, 1)
	require.Equal(t, record.ExecutionID, leases[0].ExecutionID)
	require.Equal(t, "next", leases[0].Owner)
	require.NotEqual(t, record.RecoveryToken, leases[0].Token)
	done, err := engine.Execute(ctx, ExecuteRequest{ResumeExecutionID: record.ExecutionID, WaitMode: WaitTerminal, RecoveryLease: &leases[0]})
	require.NoError(t, err)
	require.True(t, done.Completed)
	require.Equal(t, int32(1), calls.Load())
	completed, err := durable.GetExecution(ctx, record.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionCompleted, completed.State)
	require.Empty(t, completed.RecoveryOwner)
	require.Empty(t, completed.RecoveryToken)
	require.True(t, completed.RecoveryDeadline.IsZero())
}

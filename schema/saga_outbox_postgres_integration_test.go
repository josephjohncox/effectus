//go:build integration

package schema

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema/fencing"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestPostgresOutboxLeaseCASAndReplay(t *testing.T) {
	db := openSagaIntegrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, MigrateSagaV2(ctx, db))
	storeOne, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	storeTwo, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	sagaID := "integration-" + uuid.NewString()
	cleanupSagaIntegration(t, db, sagaID)

	_, err = storeOne.CreateSaga(ctx, CreateSagaRequest{
		Namespace: "integration", SagaID: sagaID, ExecutionID: "execution-1",
		PlanID: "plan-1", PlanDigest: "digest-1", Serial: true, AllowUnstableIdentityForTest: true,
	})
	require.NoError(t, err)
	dispatch, err := storeOne.EnqueueStep(ctx, EnqueueStepRequest{
		SagaID: sagaID, EffectID: "effect-1", Sequence: 1, Verb: "charge",
		ContractHash: "contract-1", Arguments: map[string]any{"amount": 42},
		Fencing: []FencingRequirement{{Authority: "accounts", Resource: sagaID}},
	})
	require.NoError(t, err)
	replayed, err := storeTwo.EnqueueStep(ctx, EnqueueStepRequest{
		SagaID: sagaID, EffectID: "effect-1", Sequence: 1, Verb: "charge",
		ContractHash: "contract-1", Arguments: map[string]any{"amount": 42},
		Fencing: []FencingRequirement{{Authority: "accounts", Resource: sagaID}},
	})
	require.NoError(t, err)
	require.Equal(t, dispatch.ID, replayed.ID)

	first, err := storeOne.ClaimDispatch(ctx, ClaimOptions{Owner: "one", LeaseDuration: 20 * time.Millisecond, TargetDispatchID: dispatch.ID})
	require.NoError(t, err)
	time.Sleep(40 * time.Millisecond)
	second, err := storeTwo.ClaimDispatch(ctx, ClaimOptions{Owner: "two", LeaseDuration: time.Second, TargetDispatchID: dispatch.ID})
	require.NoError(t, err)
	require.Equal(t, uint64(2), second.Attempt)
	require.Equal(t, first.IdempotencyKey, second.IdempotencyKey)
	require.Len(t, second.Fencing, 1)
	err = storeOne.CompleteDispatch(ctx, Completion{
		DispatchID: first.ID, Attempt: first.Attempt, LeaseToken: first.LeaseToken,
		Outcome: invocation.OutcomeSuccess, Result: []byte(`null`),
	})
	require.ErrorIs(t, err, ErrStaleLease)
	require.NoError(t, storeTwo.SaveFencingGrants(ctx, second.ID, second.Attempt, second.LeaseToken,
		[]invocation.FencingGrant{{Authority: "accounts", Resource: sagaID, Token: 1}}))
	require.NoError(t, storeTwo.CompleteDispatch(ctx, Completion{
		DispatchID: second.ID, Attempt: second.Attempt, LeaseToken: second.LeaseToken,
		Outcome: invocation.OutcomeSuccess, Result: []byte(`{"receipt":"ok"}`),
	}))
	require.NoError(t, storeTwo.CompleteSaga(ctx, sagaID))
	saga, err := storeOne.GetSaga(ctx, sagaID)
	require.NoError(t, err)
	require.Equal(t, SagaCompleted, saga.State)
	attempts, err := storeOne.ListAttempts(ctx, dispatch.ID)
	require.NoError(t, err)
	require.Len(t, attempts, 2)
}

func TestPostgresSerialSagaWaitsForEarlierRetry(t *testing.T) {
	db := openSagaIntegrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, MigrateSagaV2(ctx, db))
	store, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	sagaID := "serial-retry-" + uuid.NewString()
	cleanupSagaIntegration(t, db, sagaID)
	_, err = store.CreateSaga(ctx, CreateSagaRequest{
		Namespace: "integration", SagaID: sagaID, ExecutionID: "execution-serial-retry",
		PlanID: "plan", PlanDigest: "digest", Serial: true, AllowUnstableIdentityForTest: true,
	})
	require.NoError(t, err)
	first, err := store.EnqueueStep(ctx, EnqueueStepRequest{
		SagaID: sagaID, EffectID: "first", Sequence: 1, Verb: "first",
		ContractHash: "contract", Arguments: map[string]any{},
	})
	require.NoError(t, err)
	second, err := store.EnqueueStep(ctx, EnqueueStepRequest{
		SagaID: sagaID, EffectID: "second", Sequence: 2, Verb: "second",
		ContractHash: "contract", Arguments: map[string]any{},
	})
	require.NoError(t, err)
	claimed, err := store.ClaimDispatch(ctx, ClaimOptions{Owner: "worker", LeaseDuration: time.Minute, TargetDispatchID: first.ID})
	require.NoError(t, err)
	require.NoError(t, store.CompleteDispatch(ctx, Completion{
		DispatchID: first.ID, Attempt: claimed.Attempt, LeaseToken: claimed.LeaseToken,
		Outcome: invocation.OutcomeRetryableKnownNotCommitted, Error: "not committed",
		NextAttemptAt: time.Now().Add(time.Hour),
	}))
	_, err = store.ClaimDispatch(ctx, ClaimOptions{Owner: "worker", LeaseDuration: time.Minute, TargetDispatchID: second.ID})
	require.ErrorIs(t, err, ErrNoDispatch)
}

func TestPostgresStopFinalizationSerializesWithClaims(t *testing.T) {
	db := openSagaIntegrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, MigrateSagaV2(ctx, db))
	store, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	sagaID, executionID := "stop-race-"+uuid.NewString(), "execution-stop-"+uuid.NewString()
	cleanupSagaIntegration(t, db, sagaID)
	_, err = store.CreateSaga(ctx, CreateSagaRequest{
		Namespace: "integration", SagaID: sagaID, ExecutionID: executionID,
		PlanID: "plan", PlanDigest: "digest", Serial: true, AllowUnstableIdentityForTest: true,
	})
	require.NoError(t, err)
	dispatch, err := store.EnqueueStep(ctx, EnqueueStepRequest{
		SagaID: sagaID, EffectID: "first", Sequence: 1, Verb: "first",
		ContractHash: "contract", Arguments: map[string]any{},
	})
	require.NoError(t, err)
	blocker, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer blocker.Rollback() //nolint:errcheck -- cleanup after commit
	var lockedSagaID string
	err = blocker.QueryRowContext(ctx, `SELECT saga_id FROM effectus_saga_instances WHERE saga_id = $1 FOR UPDATE`, sagaID).Scan(&lockedSagaID)
	require.NoError(t, err)
	finalized := make(chan error, 1)
	go func() { finalized <- store.FinalizeStoppedExecution(ctx, executionID) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		probeCtx, probeCancel := context.WithTimeout(ctx, time.Second)
		var lockedID string
		probeErr := db.QueryRowContext(probeCtx, `SELECT dispatch_id FROM effectus_saga_outbox WHERE dispatch_id = $1 FOR UPDATE NOWAIT`, dispatch.ID).Scan(&lockedID)
		probeCancel()
		if probeErr != nil {
			var state interface{ SQLState() string }
			require.True(t, errors.As(probeErr, &state) && state.SQLState() == "55P03", "unexpected lock probe error: %v", probeErr)
			break // finalizer holds the outbox row while waiting on the saga row
		}
		if time.Now().After(deadline) {
			t.Fatal("finalizer never locked the dispatch before the saga")
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, err = store.ClaimDispatch(ctx, ClaimOptions{Owner: "racing-worker", LeaseDuration: time.Minute, TargetDispatchID: dispatch.ID})
	require.ErrorIs(t, err, ErrNoDispatch)
	require.NoError(t, blocker.Commit())
	require.NoError(t, <-finalized)
	stored, err := store.GetDispatch(ctx, dispatch.ID)
	require.NoError(t, err)
	require.Equal(t, DispatchCanceled, stored.State)
	saga, err := store.GetSaga(ctx, sagaID)
	require.NoError(t, err)
	require.Equal(t, SagaFailed, saga.State)
}

func TestPostgresStopFinalizationPreservesEffectEvidence(t *testing.T) {
	db := openSagaIntegrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, MigrateSagaV2(ctx, db))
	store, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	executionID := "execution-stop-states-" + uuid.NewString()
	makeSaga := func(kind string) string {
		sagaID := "stop-" + kind + "-" + uuid.NewString()
		cleanupSagaIntegration(t, db, sagaID)
		_, err := store.CreateSaga(ctx, CreateSagaRequest{
			Namespace: "integration", SagaID: sagaID, ExecutionID: executionID,
			PlanID: "plan-" + kind, PlanDigest: "digest", Serial: true, AllowUnstableIdentityForTest: true,
		})
		require.NoError(t, err)
		return sagaID
	}
	enqueue := func(sagaID, effectID string, sequence int) *Dispatch {
		dispatch, err := store.EnqueueStep(ctx, EnqueueStepRequest{
			SagaID: sagaID, EffectID: effectID, Sequence: sequence, Verb: "write",
			ContractHash: "contract", Arguments: map[string]any{},
		})
		require.NoError(t, err)
		return dispatch
	}
	committedSaga := makeSaga("committed")
	committed := enqueue(committedSaga, "first", 1)
	pending := enqueue(committedSaga, "second", 2)
	claimed, err := store.ClaimDispatch(ctx, ClaimOptions{Owner: "worker", LeaseDuration: time.Minute, TargetDispatchID: committed.ID})
	require.NoError(t, err)
	require.NoError(t, store.CompleteDispatch(ctx, Completion{
		DispatchID: committed.ID, Attempt: claimed.Attempt, LeaseToken: claimed.LeaseToken,
		Outcome: invocation.OutcomeSuccess, Result: []byte(`null`),
	}))
	unstartedSaga := makeSaga("unstarted")
	unstarted := enqueue(unstartedSaga, "first", 1)
	unknownRetrySaga := makeSaga("unknown-retry")
	unknownRetry := enqueue(unknownRetrySaga, "first", 1)
	claimed, err = store.ClaimDispatch(ctx, ClaimOptions{Owner: "worker", LeaseDuration: time.Minute, TargetDispatchID: unknownRetry.ID})
	require.NoError(t, err)
	require.NoError(t, store.CompleteDispatch(ctx, Completion{
		DispatchID: unknownRetry.ID, Attempt: claimed.Attempt, LeaseToken: claimed.LeaseToken,
		Outcome: invocation.OutcomeUnknown, Error: "sink outcome unknown", NextAttemptAt: time.Now().Add(time.Hour),
	}))
	_, err = db.ExecContext(ctx, `UPDATE effectus_saga_outbox SET next_attempt_at = now()-interval '1 second' WHERE dispatch_id = $1`, unknownRetry.ID)
	require.NoError(t, err)
	claimed, err = store.ClaimDispatch(ctx, ClaimOptions{Owner: "worker", LeaseDuration: time.Minute, TargetDispatchID: unknownRetry.ID})
	require.NoError(t, err)
	require.NoError(t, store.CompleteDispatch(ctx, Completion{
		DispatchID: unknownRetry.ID, Attempt: claimed.Attempt, LeaseToken: claimed.LeaseToken,
		Outcome: invocation.OutcomeRetryableKnownNotCommitted, Error: "retry not committed", NextAttemptAt: time.Now().Add(time.Hour),
	}))
	uncertainSaga := makeSaga("uncertain")
	uncertain := enqueue(uncertainSaga, "first", 1)
	_, err = store.ClaimDispatch(ctx, ClaimOptions{Owner: "worker", LeaseDuration: time.Minute, TargetDispatchID: uncertain.ID})
	require.NoError(t, err)
	terminalSaga := makeSaga("terminal")
	terminal := enqueue(terminalSaga, "first", 1)
	_, err = db.ExecContext(ctx, `UPDATE effectus_saga_instances SET state = 'blocked_unknown' WHERE saga_id = $1`, terminalSaga)
	require.NoError(t, err)
	terminalLeaseSaga := makeSaga("terminal-lease")
	terminalLease := enqueue(terminalLeaseSaga, "first", 1)
	_, err = store.ClaimDispatch(ctx, ClaimOptions{Owner: "worker", LeaseDuration: time.Minute, TargetDispatchID: terminalLease.ID})
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE effectus_saga_instances SET state = 'failed' WHERE saga_id = $1`, terminalLeaseSaga)
	require.NoError(t, err)
	historicalUnknownSaga := makeSaga("historical-unknown")
	historicalUnknown := enqueue(historicalUnknownSaga, "first", 1)
	_, err = db.ExecContext(ctx, `UPDATE effectus_saga_instances SET state = 'failed' WHERE saga_id = $1`, historicalUnknownSaga)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE effectus_saga_outbox SET state = 'blocked_unknown' WHERE dispatch_id = $1`, historicalUnknown.ID)
	require.NoError(t, err)
	require.ErrorIs(t, store.FinalizeStoppedExecution(ctx, executionID), ErrActiveDispatchLease)
	saga, err := store.GetSaga(ctx, unstartedSaga)
	require.NoError(t, err)
	require.Equal(t, SagaRunning, saga.State)
	_, err = db.ExecContext(ctx, `UPDATE effectus_saga_outbox SET lease_deadline = now()-interval '1 second' WHERE dispatch_id = $1`, uncertain.ID)
	require.NoError(t, err)
	require.ErrorIs(t, store.FinalizeStoppedExecution(ctx, executionID), ErrActiveDispatchLease,
		"an active lease in a terminal saga must also prevent finalization")
	_, err = db.ExecContext(ctx, `UPDATE effectus_saga_outbox SET lease_deadline = now()-interval '1 second' WHERE dispatch_id = $1`, terminalLease.ID)
	require.NoError(t, err)
	require.NoError(t, store.FinalizeStoppedExecution(ctx, executionID))
	for _, check := range []struct {
		sagaID      string
		sagaState   SagaState
		dispatchID  string
		outboxState DispatchState
	}{
		{committedSaga, SagaBlockedDependency, committed.ID, DispatchSucceeded},
		{committedSaga, SagaBlockedDependency, pending.ID, DispatchCanceled},
		{unstartedSaga, SagaFailed, unstarted.ID, DispatchCanceled},
		{unknownRetrySaga, SagaBlockedUnknown, unknownRetry.ID, DispatchBlockedUnknown},
		{uncertainSaga, SagaBlockedUnknown, uncertain.ID, DispatchBlockedUnknown},
		{terminalSaga, SagaBlockedUnknown, terminal.ID, DispatchCanceled},
		{terminalLeaseSaga, SagaBlockedUnknown, terminalLease.ID, DispatchBlockedUnknown},
		{historicalUnknownSaga, SagaBlockedUnknown, historicalUnknown.ID, DispatchBlockedUnknown},
	} {
		saga, err := store.GetSaga(ctx, check.sagaID)
		require.NoError(t, err)
		require.Equal(t, check.sagaState, saga.State)
		dispatch, err := store.GetDispatch(ctx, check.dispatchID)
		require.NoError(t, err)
		require.Equal(t, check.outboxState, dispatch.State)
	}
	require.NoError(t, store.FinalizeStoppedExecution(ctx, executionID))
}

func TestPostgresFencingTokensIncreaseAcrossProviderClients(t *testing.T) {
	db := openSagaIntegrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, MigrateSagaV2(ctx, db))
	one, err := fencing.NewPostgresProvider(db)
	require.NoError(t, err)
	two, err := fencing.NewPostgresProvider(db)
	require.NoError(t, err)
	resource := "integration-" + uuid.NewString()
	first, err := one.Acquire(ctx, fencing.Request{Authority: "sink", Resource: resource, Holder: "one", TTL: time.Second})
	require.NoError(t, err)
	_, err = two.Acquire(ctx, fencing.Request{Authority: "sink", Resource: resource, Holder: "two", TTL: time.Second})
	require.ErrorIs(t, err, fencing.ErrLeaseHeld)
	require.NoError(t, first.Release(ctx))
	second, err := two.Acquire(ctx, fencing.Request{Authority: "sink", Resource: resource, Holder: "two", TTL: time.Second})
	require.NoError(t, err)
	require.Greater(t, second.Grant().Token, first.Grant().Token)
	require.NoError(t, second.Release(ctx))
}

func openSagaIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		t.Skip("DB_DSN is required for PostgreSQL saga integration tests")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	require.NoError(t, db.PingContext(t.Context()))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func cleanupSagaIntegration(t *testing.T, db *sql.DB, sagaID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = db.ExecContext(ctx, `DELETE FROM effectus_saga_attempts WHERE dispatch_id IN (SELECT dispatch_id FROM effectus_saga_outbox WHERE saga_id = $1)`, sagaID)
		_, _ = db.ExecContext(ctx, `DELETE FROM effectus_saga_outbox WHERE saga_id = $1`, sagaID)
		_, _ = db.ExecContext(ctx, `DELETE FROM effectus_saga_steps WHERE saga_id = $1`, sagaID)
		_, _ = db.ExecContext(ctx, `DELETE FROM effectus_saga_instances WHERE saga_id = $1`, sagaID)
	})
}

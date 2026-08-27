//go:build integration

package schema

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/effectus/effectus-go/invocation"
	"github.com/effectus/effectus-go/schema/fencing"
	"github.com/google/uuid"
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
		PlanID: "plan-1", PlanDigest: "digest-1", Serial: true, allowUnstableIdentityForTest: true,
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

	first, err := storeOne.ClaimDispatch(ctx, ClaimOptions{Owner: "one", LeaseDuration: 20 * time.Millisecond})
	require.NoError(t, err)
	time.Sleep(40 * time.Millisecond)
	second, err := storeTwo.ClaimDispatch(ctx, ClaimOptions{Owner: "two", LeaseDuration: time.Second})
	require.NoError(t, err)
	require.Equal(t, uint64(2), second.Attempt)
	require.Equal(t, first.IdempotencyKey, second.IdempotencyKey)
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
	db, err := sql.Open("pgx", dsn)
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

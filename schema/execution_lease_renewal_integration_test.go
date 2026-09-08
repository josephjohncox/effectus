//go:build integration

package schema

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPostgresExecutionLeaseRenewalAndExpiry(t *testing.T) {
	db := openSagaIntegrationDB(t)
	require.NoError(t, MigrateSagaV2(t.Context(), db))
	store, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	id, identity, generation := "renew-"+uuid.NewString(), "delivery-"+uuid.NewString(), "generation-"+uuid.NewString()
	admission := testDurableAdmission(id, identity, "payload", generation)
	sagaID := StableSagaID(id, "plan")
	admission.Plans = []ExecutionPlanRecord{{ExecutionID: id, PlanID: "plan", SagaID: sagaID, Ordinal: 0}}
	admission.Sagas = []CreateSagaRequest{{Namespace: "tenant", SagaID: sagaID, ExecutionID: id, PlanID: "plan", PlanDigest: admission.Artifact.IRDigest, Serial: true}}
	admission.InitialSteps = []EnqueueStepRequest{{SagaID: sagaID, EffectID: "effect", Sequence: 1, Verb: "write", ContractHash: "contract", Arguments: map[string]any{}}}
	t.Cleanup(func() { cleanupExecutionIntegration(t, db, id, sagaID, generation) })
	_, _, err = store.AdmitExecutionAtomic(t.Context(), admission)
	require.NoError(t, err)
	original, err := store.LeaseExecution(t.Context(), id, "owner", time.Minute)
	require.NoError(t, err)
	renewed, err := store.RenewExecutionLease(t.Context(), original, 2*time.Minute)
	require.NoError(t, err)
	require.True(t, renewed.Deadline.After(original.Deadline))
	require.Equal(t, original.Token, renewed.Token)
	require.Equal(t, original.Revision, renewed.Revision)
	shortened, err := store.RenewExecutionLease(t.Context(), original, time.Second)
	require.NoError(t, err)
	require.False(t, shortened.Deadline.Before(renewed.Deadline))
	_, err = store.LeaseExecution(t.Context(), id, "competitor", time.Minute)
	require.ErrorIs(t, err, ErrStaleExecutionLease)
	_, err = db.ExecContext(t.Context(), `UPDATE effectus_executions SET recovery_deadline = clock_timestamp() - interval '1 second' WHERE execution_id = $1`, id)
	require.NoError(t, err)
	_, err = store.RenewExecutionLease(t.Context(), renewed, time.Minute)
	require.ErrorIs(t, err, ErrStaleExecutionLease)
	require.ErrorIs(t, store.FinishExecutionLease(t.Context(), original, ExecutionCompleted, ""), ErrStaleExecutionLease)
	next, err := store.LeaseExecution(t.Context(), id, "competitor", time.Minute)
	require.NoError(t, err)
	_, err = store.RenewExecutionLease(t.Context(), original, time.Minute)
	require.ErrorIs(t, err, ErrStaleExecutionLease)
	_, err = store.RenewExecutionLease(t.Context(), next, 2*time.Minute)
	require.NoError(t, err)
	require.NoError(t, store.FinishExecutionLease(t.Context(), next, ExecutionCompleted, ""))
	_, err = store.RenewExecutionLease(t.Context(), next, time.Minute)
	require.ErrorIs(t, err, ErrStaleExecutionLease)
}

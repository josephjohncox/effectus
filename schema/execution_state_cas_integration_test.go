//go:build integration

package schema

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPostgresExecutionStateCASPreservesOwnedAndTerminalRows(t *testing.T) {
	db := openSagaIntegrationDB(t)
	require.NoError(t, MigrateSagaV2(t.Context(), db))
	store, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	id, generation := "guard-"+uuid.NewString(), "generation-"+uuid.NewString()
	admission := testDurableAdmission(id, "delivery-"+uuid.NewString(), "payload", generation)
	sagaID := StableSagaID(id, "plan")
	admission.Plans = []ExecutionPlanRecord{{ExecutionID: id, PlanID: "plan", SagaID: sagaID, Ordinal: 0}}
	admission.Sagas = []CreateSagaRequest{{Namespace: "tenant", SagaID: sagaID, ExecutionID: id, PlanID: "plan", PlanDigest: admission.Artifact.IRDigest, Serial: true}}
	admission.InitialSteps = []EnqueueStepRequest{{SagaID: sagaID, EffectID: "effect", Sequence: 1, Verb: "write", ContractHash: "contract", Arguments: map[string]any{}}}
	t.Cleanup(func() { cleanupExecutionIntegration(t, db, id, sagaID, generation) })
	_, _, err = store.AdmitExecutionAtomic(t.Context(), admission)
	require.NoError(t, err)
	lease, err := store.LeaseExecution(t.Context(), id, "owner", time.Minute)
	require.NoError(t, err)
	owned, err := store.GetExecution(t.Context(), id)
	require.NoError(t, err)
	_, err = store.SetExecutionState(t.Context(), id, owned.Revision, ExecutionCompleted, "wrong owner")
	require.ErrorIs(t, err, ErrOptimisticConflict)
	after, err := store.GetExecution(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, owned, after, "failed owner guard must not change execution or plan rows")
	require.NoError(t, store.FinishExecutionLease(t.Context(), lease, ExecutionCompleted, "done"))
	terminal, err := store.GetExecution(t.Context(), id)
	require.NoError(t, err)
	_, err = store.SetExecutionState(t.Context(), id, terminal.Revision, ExecutionRunning, "resurrect")
	require.ErrorIs(t, err, ErrOptimisticConflict)
	after, err = store.GetExecution(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, terminal, after, "failed terminal guard must not change execution or plan rows")
}

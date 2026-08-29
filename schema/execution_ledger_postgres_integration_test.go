//go:build integration

package schema

import (
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPostgresAtomicAdmissionReplayAndRollback(t *testing.T) {
	db := openSagaIntegrationDB(t)
	require.NoError(t, MigrateSagaV2(t.Context(), db))
	store, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	executionID := "execution-" + uuid.NewString()
	identity := "delivery-" + uuid.NewString()
	generation := "generation-" + uuid.NewString()
	admission := testDurableAdmission(executionID, identity, "payload", generation)
	planID := "plan"
	sagaID := StableSagaID(executionID, planID)
	admission.Plans = []ExecutionPlanRecord{{ExecutionID: executionID, PlanID: planID, SagaID: sagaID, Ordinal: 0}}
	admission.Sagas = []CreateSagaRequest{{Namespace: "tenant", SagaID: sagaID, ExecutionID: executionID, PlanID: planID, PlanDigest: admission.Artifact.IRDigest, Serial: true}}
	admission.InitialSteps = []EnqueueStepRequest{{SagaID: sagaID, EffectID: "effect", Sequence: 1, Verb: "write", ContractHash: "contract", Arguments: map[string]any{"id": "42"}}}
	t.Cleanup(func() { cleanupExecutionIntegration(t, db, executionID, sagaID, generation) })

	record, created, err := store.AdmitExecutionAtomic(t.Context(), admission)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, generation, record.GenerationDigest)
	replayed, created, err := store.AdmitExecutionAtomic(t.Context(), admission)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, generation, replayed.GenerationDigest)
	var applications int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM effectus_fact_applications WHERE execution_id = $1`, executionID).Scan(&applications))
	require.Equal(t, 1, applications, "replay must not apply facts twice")
	dispatches, err := store.ListDispatches(t.Context(), sagaID)
	require.NoError(t, err)
	require.Len(t, dispatches, 1)

	conflict := admission
	conflict.Execution.RequestHash = "different"
	_, _, err = store.AdmitExecutionAtomic(t.Context(), conflict)
	require.ErrorIs(t, err, ErrIdentityConflict)

	rollbackExecution := "execution-" + uuid.NewString()
	rollbackGeneration := "generation-" + uuid.NewString()
	rollback := testDurableAdmission(rollbackExecution, "delivery-"+uuid.NewString(), "payload", rollbackGeneration)
	rollbackPlan := "plan"
	rollbackSaga := StableSagaID(rollbackExecution, rollbackPlan)
	rollback.Plans = []ExecutionPlanRecord{{ExecutionID: rollbackExecution, PlanID: rollbackPlan, SagaID: rollbackSaga, Ordinal: 0}}
	rollback.Sagas = []CreateSagaRequest{{Namespace: "tenant", SagaID: rollbackSaga, ExecutionID: rollbackExecution, PlanID: rollbackPlan, PlanDigest: rollback.Artifact.IRDigest, Serial: true}}
	rollback.InitialSteps = []EnqueueStepRequest{{SagaID: rollbackSaga, EffectID: "bad", Sequence: 2, Verb: "write", ContractHash: "contract", Arguments: map[string]any{}}}
	_, _, err = store.AdmitExecutionAtomic(t.Context(), rollback)
	require.Error(t, err)
	_, err = store.GetExecution(t.Context(), rollbackExecution)
	require.ErrorIs(t, err, ErrExecutionNotFound)
	_, err = store.GetArtifact(t.Context(), rollbackGeneration)
	require.ErrorIs(t, err, ErrArtifactNotFound)
}

func TestPostgresConcurrentDuplicateAdmissionConverges(t *testing.T) {
	db := openSagaIntegrationDB(t)
	require.NoError(t, MigrateSagaV2(t.Context(), db))
	store, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	executionID := "execution-" + uuid.NewString()
	generation := "generation-" + uuid.NewString()
	admission := testDurableAdmission(executionID, "delivery-"+uuid.NewString(), "payload", generation)
	planID := "concurrent-plan"
	sagaID := StableSagaID(executionID, planID)
	admission.Plans = []ExecutionPlanRecord{{ExecutionID: executionID, PlanID: planID, SagaID: sagaID, Ordinal: 0}}
	admission.Sagas = []CreateSagaRequest{{Namespace: "tenant", SagaID: sagaID, ExecutionID: executionID, PlanID: planID, PlanDigest: admission.Artifact.IRDigest, Serial: true}}
	admission.InitialSteps = []EnqueueStepRequest{{SagaID: sagaID, EffectID: "effect", Sequence: 1, Verb: "write", ContractHash: "contract", Arguments: map[string]any{"id": "42"}}}
	t.Cleanup(func() { cleanupExecutionIntegration(t, db, executionID, sagaID, generation) })

	const workers = 8
	start := make(chan struct{})
	results := make(chan struct {
		record  ExecutionRecord
		created bool
		err     error
	}, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for range workers {
		go func() {
			defer wait.Done()
			<-start
			record, created, err := store.AdmitExecutionAtomic(t.Context(), admission)
			results <- struct {
				record  ExecutionRecord
				created bool
				err     error
			}{record: record, created: created, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	createdCount := 0
	for result := range results {
		require.NoError(t, result.err)
		require.Equal(t, executionID, result.record.ExecutionID)
		require.Equal(t, generation, result.record.GenerationDigest)
		if result.created {
			createdCount++
		}
	}
	require.Equal(t, 1, createdCount)
}

func TestPostgresExecutionRecoveryLeaseCAS(t *testing.T) {
	db := openSagaIntegrationDB(t)
	require.NoError(t, MigrateSagaV2(t.Context(), db))
	store, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	executionID := "execution-" + uuid.NewString()
	generation := "generation-" + uuid.NewString()
	admission := testDurableAdmission(executionID, "delivery-"+uuid.NewString(), "payload", generation)
	planID := "lease-plan"
	sagaID := StableSagaID(executionID, planID)
	admission.Plans = []ExecutionPlanRecord{{ExecutionID: executionID, PlanID: planID, SagaID: sagaID, Ordinal: 0}}
	admission.Sagas = []CreateSagaRequest{{Namespace: "tenant", SagaID: sagaID, ExecutionID: executionID, PlanID: planID, PlanDigest: admission.Artifact.IRDigest, Serial: true}}
	admission.InitialSteps = []EnqueueStepRequest{{SagaID: sagaID, EffectID: "effect", Sequence: 1, Verb: "write", ContractHash: "contract", Arguments: map[string]any{}}}
	t.Cleanup(func() { cleanupExecutionIntegration(t, db, executionID, sagaID, generation) })
	_, created, err := store.AdmitExecutionAtomic(t.Context(), admission)
	require.NoError(t, err)
	require.True(t, created)
	first, err := store.LeaseExecution(t.Context(), executionID, "one", 20*time.Millisecond)
	require.NoError(t, err)
	time.Sleep(40 * time.Millisecond)
	second, err := store.LeaseExecution(t.Context(), executionID, "two", time.Second)
	require.NoError(t, err)
	require.ErrorIs(t, store.FinishExecutionLease(t.Context(), first, ExecutionCompleted, ""), ErrStaleExecutionLease)
	require.NoError(t, store.FinishExecutionLease(t.Context(), second, ExecutionCompleted, ""))
}

func cleanupExecutionIntegration(t *testing.T, db *sql.DB, executionID, sagaID, generation string) {
	t.Helper()
	_, _ = db.Exec(`DELETE FROM effectus_execution_plans WHERE execution_id = $1`, executionID)
	_, _ = db.Exec(`DELETE FROM effectus_fact_snapshots WHERE execution_id = $1`, executionID)
	_, _ = db.Exec(`DELETE FROM effectus_fact_applications WHERE execution_id = $1`, executionID)
	_, _ = db.Exec(`DELETE FROM effectus_executions WHERE execution_id = $1`, executionID)
	if sagaID != "" {
		_, _ = db.Exec(`DELETE FROM effectus_saga_attempts WHERE dispatch_id IN (SELECT dispatch_id FROM effectus_saga_outbox WHERE saga_id = $1)`, sagaID)
		_, _ = db.Exec(`DELETE FROM effectus_saga_outbox WHERE saga_id = $1`, sagaID)
		_, _ = db.Exec(`DELETE FROM effectus_saga_steps WHERE saga_id = $1`, sagaID)
		_, _ = db.Exec(`DELETE FROM effectus_saga_instances WHERE saga_id = $1`, sagaID)
	}
	_, _ = db.Exec(`DELETE FROM effectus_rule_generations WHERE generation_digest = $1`, generation)
	_, _ = db.Exec(`DELETE FROM effectus_execution_artifacts WHERE generation_digest = $1`, generation)
}

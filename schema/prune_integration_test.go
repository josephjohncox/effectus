//go:build integration

package schema

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMigrationApplyAndValidate(t *testing.T) {
	db := openSagaIntegrationDB(t)
	require.NoError(t, MigrateSagaV2(t.Context(), db))
	require.NoError(t, ValidateSagaV2(t.Context(), db))
}

func TestPruneDoesNotDeleteReactivatedGeneration(t *testing.T) {
	db := openSagaIntegrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, MigrateSagaV2(ctx, db))
	store, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	generation := "reactivated-" + uuid.NewString()
	executionID := "reactivation-" + uuid.NewString()
	admission := testDurableAdmission(executionID, "delivery-"+uuid.NewString(), "payload", generation)
	t.Cleanup(func() { cleanupExecutionIntegration(t, db, executionID, "", generation) })
	require.NoError(t, store.PutArtifact(ctx, admission.Artifact))
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err = db.ExecContext(ctx, `UPDATE effectus_execution_artifacts SET created_at = $2 WHERE generation_digest = $1`, generation, old)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO effectus_rule_generations
		(ruleset, version, environment, generation_digest, state, created_at, retired_at)
		VALUES ($1, $2, $3, $4, 'retired', $5, $5)
	`, admission.Execution.Ruleset, admission.Execution.Version, admission.Execution.TenantNamespace, generation, old)
	require.NoError(t, err)

	// Hold the Kafka table only after candidate selection. The prune operation
	// waits there before deleting its materialized generation candidates.
	blocker, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer blocker.Rollback() //nolint:errcheck -- cleanup after commit
	_, err = blocker.ExecContext(ctx, `LOCK TABLE effectus_kafka_deliveries IN SHARE MODE`)
	require.NoError(t, err)
	pruned := make(chan error, 1)
	go func() {
		_, pruneErr := PruneTerminalRecords(ctx, db, PruneOptions{
			Before: old.Add(365 * 24 * time.Hour), BatchSize: 10,
		})
		pruned <- pruneErr
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_locks waiting
				JOIN pg_class relation ON relation.oid = waiting.relation
				WHERE relation.relname = 'effectus_kafka_deliveries'
				  AND waiting.granted = false AND waiting.pid <> pg_backend_pid()
			)
		`).Scan(&waiting)
		require.NoError(t, err)
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("prune never reached the post-selection delete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, created, err := store.AdmitExecutionAtomic(ctx, admission)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, blocker.Commit())
	require.NoError(t, <-pruned)
	var state string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT state FROM effectus_rule_generations WHERE generation_digest = $1
	`, generation).Scan(&state))
	require.Equal(t, "active", state)
	_, err = store.GetArtifact(ctx, generation)
	require.NoError(t, err)
	_, err = store.GetExecution(ctx, executionID)
	require.NoError(t, err)
}

func TestPruneDryRunTerminalGraphAndBlockedStatePreserved(t *testing.T) {
	db := openSagaIntegrationDB(t)
	require.NoError(t, MigrateSagaV2(t.Context(), db))
	prefix := uuid.NewString()
	terminalExecution := "terminal-execution-" + prefix
	terminalSaga := "terminal-saga-" + prefix
	terminalArtifact := "terminal-artifact-" + prefix
	blockedExecution := "blocked-execution-" + prefix
	blockedSaga := "blocked-saga-" + prefix
	blockedArtifact := "blocked-artifact-" + prefix
	poisonAck := "poison-ack-" + prefix
	poisonBlocked := "poison-blocked-" + prefix
	old := time.Now().UTC().Add(-72 * time.Hour)
	cutoff := time.Now().UTC().Add(-24 * time.Hour)

	insertArtifact := func(digest string) {
		_, err := db.Exec(`INSERT INTO effectus_execution_artifacts
			(generation_digest, ir_digest, ir_bytes, environment, executor_manifest, function_manifest, source_digest, compiler_metadata, created_at)
			VALUES ($1, 'ir', '\x01', '{}', '{}', '{}', 'source', '{}', $2)`, digest, old)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO effectus_rule_generations
			(ruleset, version, environment, generation_digest, state, created_at, retired_at)
			VALUES ($1, '1', $1, $2, 'retired', $3, $3)`, "rules-"+digest, digest, old)
		require.NoError(t, err)
	}
	insertExecution := func(executionID, sagaID, digest, state string) {
		insertArtifact(digest)
		_, err := db.Exec(`INSERT INTO effectus_executions
			(execution_id, admission_identity, request_hash, ruleset, version, tenant_namespace, merge_policy, generation_digest, effective_facts, state, created_at, updated_at)
			VALUES ($1, $2, 'request', 'rules', '1', 'tenant', 'last', $3, '{}', $4, $5, $5)`, executionID, "admission-"+executionID, digest, state, old)
		require.NoError(t, err)
		sagaState := "completed"
		if state == "blocked_unknown" {
			sagaState = "blocked_unknown"
		}
		_, err = db.Exec(`INSERT INTO effectus_saga_instances
			(saga_id, namespace, execution_id, plan_id, plan_digest, state, created_at, updated_at)
			VALUES ($1, 'tenant', $2, 'plan', 'digest', $3, $4, $4)`, sagaID, executionID, sagaState, old)
		require.NoError(t, err)
		planState := "completed"
		if state == "blocked_unknown" {
			planState = "blocked"
		}
		_, err = db.Exec(`INSERT INTO effectus_execution_plans (execution_id, plan_id, saga_id, ordinal, state)
			VALUES ($1, 'plan', $2, 0, $3)`, executionID, sagaID, planState)
		require.NoError(t, err)
	}
	insertExecution(terminalExecution, terminalSaga, terminalArtifact, "completed")
	insertExecution(blockedExecution, blockedSaga, blockedArtifact, "blocked_unknown")
	_, err := db.Exec(`INSERT INTO effectus_kafka_deliveries (delivery_id, failures, poison_acknowledged, updated_at) VALUES ($1, 3, true, $3), ($2, 3, false, $3)`, poisonAck, poisonBlocked, old)
	require.NoError(t, err)

	t.Cleanup(func() {
		for _, query := range []string{
			`DELETE FROM effectus_execution_plans WHERE execution_id LIKE '%' || $1`,
			`DELETE FROM effectus_saga_instances WHERE saga_id LIKE '%' || $1`,
			`DELETE FROM effectus_executions WHERE execution_id LIKE '%' || $1`,
			`DELETE FROM effectus_rule_generations WHERE generation_digest LIKE '%' || $1`,
			`DELETE FROM effectus_execution_artifacts WHERE generation_digest LIKE '%' || $1`,
			`DELETE FROM effectus_kafka_deliveries WHERE delivery_id LIKE '%' || $1`,
		} {
			_, cleanupErr := db.Exec(query, prefix)
			if cleanupErr != nil {
				t.Logf("cleanup %q: %v", query, cleanupErr)
			}
		}
	})

	dryRun, err := PruneTerminalRecords(t.Context(), db, PruneOptions{Before: cutoff, BatchSize: 10, DryRun: true})
	require.NoError(t, err)
	require.Equal(t, int64(1), dryRun.Executions)
	require.Equal(t, int64(1), dryRun.SagaInstances)
	require.Equal(t, int64(1), dryRun.KafkaDeliveries)
	assertRowExists(t, db, "effectus_executions", "execution_id", terminalExecution, true)

	report, err := PruneTerminalRecords(t.Context(), db, PruneOptions{Before: cutoff, BatchSize: 10})
	require.NoError(t, err)
	require.Equal(t, dryRun, report)
	assertRowExists(t, db, "effectus_executions", "execution_id", terminalExecution, false)
	assertRowExists(t, db, "effectus_executions", "execution_id", blockedExecution, true)
	assertRowExists(t, db, "effectus_kafka_deliveries", "delivery_id", poisonAck, false)
	assertRowExists(t, db, "effectus_kafka_deliveries", "delivery_id", poisonBlocked, true)
}

func assertRowExists(t *testing.T, db interface{ QueryRow(string, ...any) *sql.Row }, table, column, value string, want bool) {
	t.Helper()
	var exists bool
	query := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE %s = $1)`, table, column)
	require.NoError(t, db.QueryRow(query, value).Scan(&exists))
	require.Equal(t, want, exists)
}

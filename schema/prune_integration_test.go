//go:build integration

package schema

import (
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

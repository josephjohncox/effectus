//go:build integration

package schema

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestConcurrentSagaMigratorsFinish(t *testing.T) {
	db := openSagaIntegrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	start := make(chan struct{})
	errors := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for index := 0; index < 2; index++ {
		go func() {
			ready.Done()
			<-start
			errors <- MigrateSagaV2(ctx, db)
		}()
	}
	ready.Wait()
	close(start)
	for index := 0; index < 2; index++ {
		require.NoError(t, <-errors)
	}
}

func TestSagaMigrationWithOneOpenConnection(t *testing.T) {
	db := openSagaIntegrationDB(t)
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, MigrateSagaV2(ctx, db))
	require.NoError(t, ValidateSagaV2(ctx, db))
}

func TestSagaMigrationBackfillsStoppedTerminalIntents(t *testing.T) {
	admin := openSagaIntegrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	schemaName := "effectus_backfill_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schemaName)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + schemaName + ` CASCADE`) })
	dsn, err := url.Parse(os.Getenv("DB_DSN"))
	require.NoError(t, err)
	query := dsn.Query()
	query.Set("search_path", schemaName)
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("postgres", dsn.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(ctx))
	provider, err := newSagaMigrationProvider(db)
	require.NoError(t, err)
	_, err = provider.UpTo(ctx, 10004)
	require.NoError(t, err)

	generation := "generation-" + uuid.NewString()
	executionID := "execution-" + uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO effectus_execution_artifacts
		(generation_digest, ir_digest, ir_bytes, environment, executor_manifest, function_manifest, source_digest, compiler_metadata)
		VALUES ($1, 'ir', '\x01', '{}', '{}', '{}', 'source', '{}')`, generation)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO effectus_executions
		(execution_id, admission_identity, request_hash, ruleset, version, tenant_namespace,
		 merge_policy, generation_digest, effective_facts, state)
		VALUES ($1, $2, 'request', 'rules', '1', 'tenant', 'last', $3, '{}', 'failed')`,
		executionID, "admission-"+executionID, generation)
	require.NoError(t, err)
	insertSaga := func(sagaID, effectID, dispatchState string, ordinal int) {
		_, err := db.ExecContext(ctx, `INSERT INTO effectus_saga_instances
			(saga_id, namespace, execution_id, plan_id, plan_digest, state)
			VALUES ($1, 'tenant', $2, $3, 'digest', 'running')`, sagaID, executionID, "plan-"+effectID)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO effectus_execution_plans
			(execution_id, plan_id, saga_id, ordinal, state)
			VALUES ($1, $2, $3, $4, 'selected')`, executionID, "plan-"+effectID, sagaID, ordinal)
		require.NoError(t, err)
		stepState := "pending"
		if dispatchState == "succeeded" {
			stepState = "succeeded"
		}
		_, err = db.ExecContext(ctx, `INSERT INTO effectus_saga_steps
			(saga_id, effect_id, sequence, verb, contract_hash, arguments, argument_hash, state)
			VALUES ($1, $2, 1, 'write', 'contract', '{}', 'hash', $3)`, sagaID, effectID, stepState)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO effectus_saga_outbox
			(dispatch_id, saga_id, effect_id, sequence, direction, verb, contract_hash,
			 arguments, argument_hash, idempotency_key, state)
			VALUES ($1, $2, $3, 1, 'forward', 'write', 'contract', '{}', 'hash', $4, $5)`,
			"dispatch-"+effectID, sagaID, effectID, "key-"+effectID, dispatchState)
		require.NoError(t, err)
	}
	untouchedSaga, evidenceSaga := "untouched-"+uuid.NewString(), "evidence-"+uuid.NewString()
	attemptedSaga, terminalSaga := "attempted-"+uuid.NewString(), "terminal-"+uuid.NewString()
	unknownSaga := "unknown-" + uuid.NewString()
	historicalUnknownSaga := "historical-unknown-" + uuid.NewString()
	insertSaga(untouchedSaga, "untouched", "queued", 0)
	insertSaga(evidenceSaga, "evidence", "succeeded", 1)
	insertSaga(attemptedSaga, "attempted", "queued", 2)
	insertSaga(terminalSaga, "terminal", "retry_wait", 3)
	insertSaga(unknownSaga, "unknown", "retry_wait", 4)
	insertSaga(historicalUnknownSaga, "historical-unknown", "queued", 5)
	_, err = db.ExecContext(ctx, `UPDATE effectus_saga_instances SET state = 'failed' WHERE saga_id = $1`, historicalUnknownSaga)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE effectus_saga_outbox SET state = 'blocked_unknown' WHERE saga_id = $1`, historicalUnknownSaga)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE effectus_saga_instances SET state = 'blocked_unknown' WHERE saga_id = $1`, terminalSaga)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE effectus_saga_outbox SET last_outcome = 'retryable_failure_known_not_committed' WHERE saga_id = $1`, unknownSaga)
	require.NoError(t, err)
	for _, effectID := range []string{"attempted", "terminal", "unknown"} {
		_, err = db.ExecContext(ctx, `UPDATE effectus_saga_outbox SET attempt = 1 WHERE dispatch_id = $1`, "dispatch-"+effectID)
		require.NoError(t, err)
		outcome := "retryable_failure_known_not_committed"
		if effectID == "unknown" {
			outcome = "unknown_outcome"
		}
		_, err = db.ExecContext(ctx, `INSERT INTO effectus_saga_attempts
			(dispatch_id, attempt, lease_owner, lease_token, lease_deadline, outcome, started_at, completed_at)
			VALUES ($1, 1, 'worker', 'token', now()-interval '1 hour',
			        $2, now()-interval '2 hours', now()-interval '1 hour')`,
			"dispatch-"+effectID, outcome)
		require.NoError(t, err)
	}
	require.NoError(t, MigrateSagaV2(ctx, db))
	store, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	var sagaState, dispatchState string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state FROM effectus_saga_instances WHERE saga_id = $1`, untouchedSaga).Scan(&sagaState))
	require.Equal(t, "failed", sagaState)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state FROM effectus_saga_outbox WHERE saga_id = $1`, untouchedSaga).Scan(&dispatchState))
	require.Equal(t, "canceled", dispatchState)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state FROM effectus_saga_instances WHERE saga_id = $1`, evidenceSaga).Scan(&sagaState))
	require.Equal(t, "blocked_dependency", sagaState)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state FROM effectus_saga_outbox WHERE saga_id = $1`, evidenceSaga).Scan(&dispatchState))
	require.Equal(t, "succeeded", dispatchState)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state FROM effectus_saga_instances WHERE saga_id = $1`, attemptedSaga).Scan(&sagaState))
	require.Equal(t, "failed", sagaState)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state FROM effectus_saga_outbox WHERE saga_id = $1`, attemptedSaga).Scan(&dispatchState))
	require.Equal(t, "canceled", dispatchState)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state FROM effectus_saga_instances WHERE saga_id = $1`, terminalSaga).Scan(&sagaState))
	require.Equal(t, "blocked_unknown", sagaState)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state FROM effectus_saga_outbox WHERE saga_id = $1`, terminalSaga).Scan(&dispatchState))
	require.Equal(t, "canceled", dispatchState)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state FROM effectus_saga_instances WHERE saga_id = $1`, unknownSaga).Scan(&sagaState))
	require.Equal(t, "blocked_unknown", sagaState)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state FROM effectus_saga_outbox WHERE saga_id = $1`, unknownSaga).Scan(&dispatchState))
	require.Equal(t, "blocked_unknown", dispatchState)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state FROM effectus_saga_instances WHERE saga_id = $1`, historicalUnknownSaga).Scan(&sagaState))
	require.Equal(t, "blocked_unknown", sagaState)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state FROM effectus_saga_outbox WHERE saga_id = $1`, historicalUnknownSaga).Scan(&dispatchState))
	require.Equal(t, "blocked_unknown", dispatchState)
	var executionState string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state FROM effectus_executions WHERE execution_id = $1`, executionID).Scan(&executionState))
	require.Equal(t, "blocked_unknown", executionState)
	var attempts int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM effectus_saga_attempts WHERE dispatch_id = 'dispatch-terminal'`).Scan(&attempts))
	require.Equal(t, 1, attempts)
	var revision uint64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT revision FROM effectus_saga_outbox WHERE dispatch_id = 'dispatch-terminal'`).Scan(&revision))
	require.NoError(t, MigrateSagaV2(ctx, db))
	var replayedRevision uint64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT revision FROM effectus_saga_outbox WHERE dispatch_id = 'dispatch-terminal'`).Scan(&replayedRevision))
	require.Equal(t, revision, replayedRevision)
	_, err = db.ExecContext(ctx, `DELETE FROM effectus_saga_attempts WHERE dispatch_id IN ('dispatch-attempted', 'dispatch-unknown')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM effectus_saga_outbox WHERE dispatch_id IN ('dispatch-attempted', 'dispatch-unknown', 'dispatch-historical-unknown')`)
	require.NoError(t, err)
	stats, err := store.RecoveryStats(ctx)
	require.NoError(t, err)
	require.True(t, stats.OldestOutbox.IsZero(), "canceled and succeeded dispatches must not age as open work")
	postMigrationSaga := "postmigration-" + uuid.NewString()
	insertSaga(postMigrationSaga, "postmigration", "queued", 6)
	_, err = store.ClaimDispatch(ctx, ClaimOptions{
		Owner: "worker", LeaseDuration: time.Minute, TargetDispatchID: "dispatch-postmigration",
	})
	require.ErrorIs(t, err, ErrNoDispatch, "terminal execution must gate historical queued work")
}

func TestValidateSagaSchemaWithRuntimeRoleNoDDL(t *testing.T) {
	admin := openSagaIntegrationDB(t)
	require.NoError(t, MigrateSagaV2(t.Context(), admin))
	dsn, err := url.Parse(os.Getenv("DB_DSN"))
	if err != nil || dsn.Scheme == "" {
		t.Skip("DB_DSN must be a URL for runtime-role validation")
	}
	role := "effectus_runtime_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	password := "runtime-test-password"
	_, err = admin.Exec(`CREATE ROLE ` + role + ` LOGIN PASSWORD '` + password + `'`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP OWNED BY ` + role)
		_, _ = admin.Exec(`DROP ROLE IF EXISTS ` + role)
	})
	var database string
	require.NoError(t, admin.QueryRow(`SELECT current_database()`).Scan(&database))
	_, err = admin.Exec(`GRANT CONNECT ON DATABASE "` + database + `" TO ` + role)
	require.NoError(t, err)
	_, err = admin.Exec(`GRANT USAGE ON SCHEMA public TO ` + role)
	require.NoError(t, err)
	_, err = admin.Exec(`GRANT SELECT ON effectus_saga_goose_db_version TO ` + role)
	require.NoError(t, err)
	dsn.User = url.UserPassword(role, password)
	runtimeDB, err := sql.Open("postgres", dsn.String())
	require.NoError(t, err)
	defer runtimeDB.Close()
	require.NoError(t, ValidateSagaV2(t.Context(), runtimeDB))
	_, err = runtimeDB.ExecContext(t.Context(), `CREATE TABLE effectus_forbidden_ddl(id integer)`)
	require.Error(t, err)
}

func TestRetentionPruneDryRunBatchAndStateSafety(t *testing.T) {
	db := openSagaIntegrationDB(t)
	require.NoError(t, MigrateSagaV2(t.Context(), db))
	store, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	makeExecution := func(state ExecutionState) (string, string) {
		executionID, generation := "prune-"+uuid.NewString(), "generation-"+uuid.NewString()
		admission := testDurableAdmission(executionID, "delivery-"+uuid.NewString(), "payload", generation)
		_, _, err := store.AdmitExecutionAtomic(t.Context(), admission)
		require.NoError(t, err)
		_, err = db.Exec(`UPDATE effectus_executions SET state=$2, updated_at=now()-interval '60 days' WHERE execution_id=$1`, executionID, state)
		require.NoError(t, err)
		_, err = db.Exec(`UPDATE effectus_fact_applications SET applied_at=now()-interval '60 days' WHERE execution_id=$1`, executionID)
		require.NoError(t, err)
		_, err = db.Exec(`UPDATE effectus_fact_snapshots SET created_at=now()-interval '60 days' WHERE execution_id=$1`, executionID)
		require.NoError(t, err)
		t.Cleanup(func() { cleanupExecutionIntegration(t, db, executionID, "", generation) })
		return executionID, generation
	}
	terminalID, _ := makeExecution(ExecutionCompleted)
	nonterminalID, _ := makeExecution(ExecutionRunning)
	blockedID, _ := makeExecution(ExecutionBlockedUnknown)
	poisonID, activeID := "poison-"+uuid.NewString(), "active-"+uuid.NewString()
	_, err = db.Exec(`INSERT INTO effectus_kafka_deliveries(delivery_id, poison_acknowledged, updated_at) VALUES ($1,true,now()-interval '60 days'),($2,false,now()-interval '60 days')`, poisonID, activeID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM effectus_kafka_deliveries WHERE delivery_id IN ($1,$2)`, poisonID, activeID)
	})

	options := PruneOptions{Retention: 30 * 24 * time.Hour, BatchSize: 1, DryRun: true}
	result, err := PruneTerminalRecords(t.Context(), db, options)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Executions)
	require.Equal(t, int64(1), result.KafkaDeliveries)
	_, err = store.GetExecution(t.Context(), terminalID)
	require.NoError(t, err, "dry-run must not mutate")

	options.DryRun = false
	_, err = PruneTerminalRecords(t.Context(), db, options)
	require.NoError(t, err)
	_, err = store.GetExecution(t.Context(), terminalID)
	require.ErrorIs(t, err, ErrExecutionNotFound)
	_, err = store.GetExecution(t.Context(), nonterminalID)
	require.NoError(t, err)
	_, err = store.GetExecution(t.Context(), blockedID)
	require.NoError(t, err)
	var activeCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM effectus_kafka_deliveries WHERE delivery_id=$1`, activeID).Scan(&activeCount))
	require.Equal(t, 1, activeCount)
}

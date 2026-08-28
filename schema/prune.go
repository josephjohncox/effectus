package schema

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PruneOptions bounds one durable-record retention pass.
type PruneOptions struct {
	Before    time.Time
	Retention time.Duration
	BatchSize int
	DryRun    bool
}

// PruneReport reports candidate or deleted rows by durable table.
type PruneReport struct {
	Executions       int64 `json:"executions"`
	ExecutionPlans   int64 `json:"execution_plans"`
	FactApplications int64 `json:"fact_applications"`
	FactSnapshots    int64 `json:"fact_snapshots"`
	SagaInstances    int64 `json:"saga_instances"`
	// Sagas is a compatibility alias for SagaInstances.
	Sagas           int64 `json:"-"`
	SagaSteps       int64 `json:"saga_steps"`
	SagaOutbox      int64 `json:"saga_outbox"`
	SagaAttempts    int64 `json:"saga_attempts"`
	RuleGenerations int64 `json:"rule_generations"`
	Artifacts       int64 `json:"artifacts"`
	KafkaDeliveries int64 `json:"kafka_deliveries"`
}

// PruneResult is retained for compatibility with the retention-based API.
type PruneResult = PruneReport

// PruneTerminalRecords removes only old terminal execution graphs. Blocked,
// admitting, running, leased, retrying, and unacknowledged poison state is
// never selected. Deletions run in FK order in one bounded transaction.
func PruneTerminalRecords(ctx context.Context, db *sql.DB, options PruneOptions) (PruneReport, error) {
	if db == nil {
		return PruneReport{}, fmt.Errorf("prune database is required")
	}
	before := options.Before
	if before.IsZero() && options.Retention > 0 {
		before = time.Now().Add(-options.Retention)
	}
	if before.IsZero() {
		return PruneReport{}, fmt.Errorf("prune cutoff or positive retention is required")
	}
	if options.BatchSize < 1 || options.BatchSize > 10_000 {
		return PruneReport{}, fmt.Errorf("prune batch size must be between 1 and 10000")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return PruneReport{}, fmt.Errorf("begin prune transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck -- commit below owns the successful path

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(0x457072756e653031)); err != nil {
		return PruneReport{}, fmt.Errorf("lock prune operation: %w", err)
	}
	statements := []struct {
		name string
		sql  string
	}{
		{"execution candidates", `
			CREATE TEMPORARY TABLE effectus_prune_executions ON COMMIT DROP AS
			SELECT execution_id
			FROM effectus_executions execution
			WHERE execution.state IN ('completed', 'failed')
			  AND execution.updated_at < $1
			  AND execution.recovery_owner IS NULL
			  AND execution.recovery_token IS NULL
			  AND execution.recovery_deadline IS NULL
			  AND NOT EXISTS (
				SELECT 1
				FROM effectus_execution_plans plan
				JOIN effectus_saga_instances saga ON saga.saga_id = plan.saga_id
				WHERE plan.execution_id = execution.execution_id
				  AND (saga.state NOT IN ('completed', 'compensated', 'failed') OR saga.updated_at >= $1)
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM effectus_execution_plans plan
				JOIN effectus_saga_outbox outbox ON outbox.saga_id = plan.saga_id
				WHERE plan.execution_id = execution.execution_id
				  AND (
					outbox.state NOT IN ('succeeded', 'failed_permanent')
					OR outbox.updated_at >= $1
					OR outbox.lease_owner IS NOT NULL
					OR outbox.lease_token IS NOT NULL
					OR outbox.lease_deadline IS NOT NULL
				  )
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM effectus_execution_plans plan
				JOIN effectus_saga_outbox outbox ON outbox.saga_id = plan.saga_id
				JOIN effectus_saga_attempts attempt ON attempt.dispatch_id = outbox.dispatch_id
				WHERE plan.execution_id = execution.execution_id
				  AND (attempt.completed_at IS NULL OR attempt.completed_at >= $1)
			  )
			  AND NOT EXISTS (
				SELECT 1 FROM effectus_fact_applications application
				WHERE application.execution_id = execution.execution_id
				  AND application.applied_at >= $1
			  )
			  AND NOT EXISTS (
				SELECT 1 FROM effectus_fact_snapshots snapshot
				WHERE snapshot.execution_id = execution.execution_id
				  AND snapshot.created_at >= $1
			  )
			ORDER BY execution.updated_at, execution.execution_id
			LIMIT $2
		`},
		{"saga candidates", `
			CREATE TEMPORARY TABLE effectus_prune_sagas ON COMMIT DROP AS
			SELECT saga.saga_id
			FROM effectus_saga_instances saga
			WHERE saga.state IN ('completed', 'compensated', 'failed')
			  AND saga.updated_at < $1
			  AND NOT EXISTS (
				SELECT 1 FROM effectus_saga_outbox outbox
				WHERE outbox.saga_id = saga.saga_id
				  AND (
					outbox.state NOT IN ('succeeded', 'failed_permanent')
					OR outbox.updated_at >= $1
					OR outbox.lease_owner IS NOT NULL
					OR outbox.lease_token IS NOT NULL
					OR outbox.lease_deadline IS NOT NULL
				  )
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM effectus_saga_outbox outbox
				JOIN effectus_saga_attempts attempt ON attempt.dispatch_id = outbox.dispatch_id
				WHERE outbox.saga_id = saga.saga_id
				  AND (attempt.completed_at IS NULL OR attempt.completed_at >= $1)
			  )
			  AND NOT EXISTS (
				SELECT 1 FROM effectus_execution_plans plan
				WHERE plan.saga_id = saga.saga_id
				  AND NOT EXISTS (
					SELECT 1 FROM effectus_prune_executions execution
					WHERE execution.execution_id = plan.execution_id
				  )
			  )
			ORDER BY saga.updated_at, saga.saga_id
			LIMIT $2
		`},
		{"Kafka poison candidates", `
			CREATE TEMPORARY TABLE effectus_prune_kafka ON COMMIT DROP AS
			SELECT delivery_id
			FROM effectus_kafka_deliveries
			WHERE poison_acknowledged AND updated_at < $1
			ORDER BY updated_at, delivery_id
			LIMIT $2
		`},
		{"retired generation candidates", `
			CREATE TEMPORARY TABLE effectus_prune_generations ON COMMIT DROP AS
			SELECT generation.ruleset, generation.version, generation.generation_digest
			FROM effectus_rule_generations generation
			WHERE generation.state = 'retired' AND generation.retired_at < $1
			  AND NOT EXISTS (
				SELECT 1 FROM effectus_executions execution
				WHERE execution.generation_digest = generation.generation_digest
				  AND NOT EXISTS (
					SELECT 1 FROM effectus_prune_executions candidate
					WHERE candidate.execution_id = execution.execution_id
				  )
			  )
			ORDER BY generation.retired_at, generation.generation_digest
			LIMIT $2
		`},
		{"artifact candidates", `
			CREATE TEMPORARY TABLE effectus_prune_artifacts ON COMMIT DROP AS
			SELECT artifact.generation_digest
			FROM effectus_execution_artifacts artifact
			WHERE artifact.created_at < $1
			  AND NOT EXISTS (
				SELECT 1 FROM effectus_executions execution
				WHERE execution.generation_digest = artifact.generation_digest
				  AND NOT EXISTS (
					SELECT 1 FROM effectus_prune_executions candidate
					WHERE candidate.execution_id = execution.execution_id
				  )
			  )
			  AND NOT EXISTS (
				SELECT 1 FROM effectus_rule_generations generation
				WHERE generation.generation_digest = artifact.generation_digest
				  AND NOT EXISTS (
					SELECT 1 FROM effectus_prune_generations candidate
					WHERE candidate.ruleset = generation.ruleset
					  AND candidate.version = generation.version
					  AND candidate.generation_digest = generation.generation_digest
				  )
			  )
			ORDER BY artifact.created_at, artifact.generation_digest
			LIMIT $2
		`},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.sql, before, options.BatchSize); err != nil {
			return PruneReport{}, fmt.Errorf("select %s: %w", statement.name, err)
		}
	}

	report, err := inspectPruneCandidates(ctx, tx, before)
	if err != nil {
		return PruneReport{}, err
	}
	report.Sagas = report.SagaInstances
	if options.DryRun {
		return report, nil
	}

	deletions := []struct {
		name string
		sql  string
	}{
		{"saga attempts", `DELETE FROM effectus_saga_attempts WHERE dispatch_id IN (SELECT dispatch_id FROM effectus_saga_outbox WHERE saga_id IN (SELECT saga_id FROM effectus_prune_sagas))`},
		{"saga outbox", `DELETE FROM effectus_saga_outbox WHERE saga_id IN (SELECT saga_id FROM effectus_prune_sagas)`},
		{"fact snapshots", `DELETE FROM effectus_fact_snapshots WHERE execution_id IN (SELECT execution_id FROM effectus_prune_executions)`},
		{"fact applications", `DELETE FROM effectus_fact_applications WHERE execution_id IN (SELECT execution_id FROM effectus_prune_executions)`},
		{"execution plans", `DELETE FROM effectus_execution_plans WHERE execution_id IN (SELECT execution_id FROM effectus_prune_executions) OR saga_id IN (SELECT saga_id FROM effectus_prune_sagas)`},
		{"saga steps", `DELETE FROM effectus_saga_steps WHERE saga_id IN (SELECT saga_id FROM effectus_prune_sagas)`},
		{"saga instances", `DELETE FROM effectus_saga_instances WHERE saga_id IN (SELECT saga_id FROM effectus_prune_sagas)`},
		{"executions", `DELETE FROM effectus_executions WHERE execution_id IN (SELECT execution_id FROM effectus_prune_executions)`},
		{"Kafka poison records", `DELETE FROM effectus_kafka_deliveries WHERE delivery_id IN (SELECT delivery_id FROM effectus_prune_kafka)`},
		{"retired generations", `DELETE FROM effectus_rule_generations generation USING effectus_prune_generations candidate WHERE generation.ruleset = candidate.ruleset AND generation.version = candidate.version AND generation.generation_digest = candidate.generation_digest`},
		{"unreferenced artifacts", `DELETE FROM effectus_execution_artifacts artifact WHERE artifact.generation_digest IN (SELECT generation_digest FROM effectus_prune_artifacts)`},
	}
	for _, deletion := range deletions {
		var err error
		if strings.Contains(deletion.sql, "$1") {
			_, err = tx.ExecContext(ctx, deletion.sql, before)
		} else {
			_, err = tx.ExecContext(ctx, deletion.sql)
		}
		if err != nil {
			return PruneReport{}, fmt.Errorf("delete %s: %w", deletion.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return PruneReport{}, fmt.Errorf("commit prune transaction: %w", err)
	}
	return report, nil
}

func inspectPruneCandidates(ctx context.Context, tx *sql.Tx, before time.Time) (PruneReport, error) {
	var report PruneReport
	queries := []struct {
		name string
		dest *int64
		sql  string
	}{
		{"executions", &report.Executions, `SELECT count(*) FROM effectus_prune_executions`},
		{"execution plans", &report.ExecutionPlans, `SELECT count(*) FROM effectus_execution_plans WHERE execution_id IN (SELECT execution_id FROM effectus_prune_executions) OR saga_id IN (SELECT saga_id FROM effectus_prune_sagas)`},
		{"fact applications", &report.FactApplications, `SELECT count(*) FROM effectus_fact_applications WHERE execution_id IN (SELECT execution_id FROM effectus_prune_executions)`},
		{"fact snapshots", &report.FactSnapshots, `SELECT count(*) FROM effectus_fact_snapshots WHERE execution_id IN (SELECT execution_id FROM effectus_prune_executions)`},
		{"saga instances", &report.SagaInstances, `SELECT count(*) FROM effectus_prune_sagas`},
		{"saga steps", &report.SagaSteps, `SELECT count(*) FROM effectus_saga_steps WHERE saga_id IN (SELECT saga_id FROM effectus_prune_sagas)`},
		{"saga outbox", &report.SagaOutbox, `SELECT count(*) FROM effectus_saga_outbox WHERE saga_id IN (SELECT saga_id FROM effectus_prune_sagas)`},
		{"saga attempts", &report.SagaAttempts, `SELECT count(*) FROM effectus_saga_attempts WHERE dispatch_id IN (SELECT dispatch_id FROM effectus_saga_outbox WHERE saga_id IN (SELECT saga_id FROM effectus_prune_sagas))`},
		{"Kafka deliveries", &report.KafkaDeliveries, `SELECT count(*) FROM effectus_prune_kafka`},
		{"retired generations", &report.RuleGenerations, `SELECT count(*) FROM effectus_prune_generations`},
		{"artifacts", &report.Artifacts, `SELECT count(*) FROM effectus_prune_artifacts`},
	}
	for _, query := range queries {
		var row *sql.Row
		if strings.Contains(query.sql, "$1") {
			row = tx.QueryRowContext(ctx, query.sql, before)
		} else {
			row = tx.QueryRowContext(ctx, query.sql)
		}
		if err := row.Scan(query.dest); err != nil {
			return PruneReport{}, fmt.Errorf("count prune %s: %w", query.name, err)
		}
	}
	return report, nil
}

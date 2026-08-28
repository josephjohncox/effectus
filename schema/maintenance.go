package schema

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const LatestSagaSchemaVersion int64 = 10003

// ValidateSagaV2 performs read-only startup validation. Runtime credentials do
// not need DDL privileges.
func ValidateSagaV2(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("saga schema database is required")
	}
	var version sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT max(version_id) FROM effectus_saga_goose_db_version WHERE is_applied`).Scan(&version); err != nil {
		return fmt.Errorf("Effectus database schema is missing; run effectusd --migrate-only with a migration credential: %w", err)
	}
	if !version.Valid || version.Int64 < LatestSagaSchemaVersion {
		return fmt.Errorf("Effectus database schema version is %d; version %d is required; run effectusd --migrate-only", version.Int64, LatestSagaSchemaVersion)
	}
	return nil
}

// PruneOptions bounds destructive durable-record maintenance.
type PruneOptions struct {
	Retention time.Duration
	BatchSize int
	DryRun    bool
}

// PruneResult reports rows eligible for or deleted by one FK-ordered batch.
type PruneResult struct {
	Executions      int64
	Sagas           int64
	KafkaDeliveries int64
}

// PruneTerminalRecords deletes only completed/failed executions whose sagas
// are also terminal. Blocked and nonterminal work is never selected.
func PruneTerminalRecords(ctx context.Context, db *sql.DB, options PruneOptions) (PruneResult, error) {
	if db == nil || options.Retention <= 0 || options.BatchSize <= 0 {
		return PruneResult{}, fmt.Errorf("database, positive retention, and positive batch size are required")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return PruneResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `CREATE TEMP TABLE effectus_prune_executions(execution_id text PRIMARY KEY) ON COMMIT DROP`); err != nil {
		return PruneResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO effectus_prune_executions
		SELECT e.execution_id FROM effectus_executions e
		WHERE e.state IN ('completed','failed')
		  AND e.updated_at < now() - ($1 * interval '1 microsecond')
		  AND NOT EXISTS (
			SELECT 1 FROM effectus_execution_plans p JOIN effectus_saga_instances s ON s.saga_id=p.saga_id
			WHERE p.execution_id=e.execution_id AND s.state NOT IN ('completed','compensated','failed'))
		ORDER BY e.updated_at, e.execution_id LIMIT $2
	`, options.Retention.Microseconds(), options.BatchSize); err != nil {
		return PruneResult{}, err
	}
	var result PruneResult
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM effectus_prune_executions`).Scan(&result.Executions); err != nil {
		return PruneResult{}, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM effectus_saga_instances s JOIN effectus_execution_plans p ON p.saga_id=s.saga_id JOIN effectus_prune_executions x ON x.execution_id=p.execution_id`).Scan(&result.Sagas); err != nil {
		return PruneResult{}, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM effectus_kafka_deliveries WHERE poison_acknowledged AND updated_at < now() - ($1 * interval '1 microsecond')`, options.Retention.Microseconds()).Scan(&result.KafkaDeliveries); err != nil {
		return PruneResult{}, err
	}
	if options.DryRun {
		return result, tx.Rollback()
	}
	statements := []string{
		`DELETE FROM effectus_saga_attempts a USING effectus_saga_outbox o, effectus_execution_plans p, effectus_prune_executions x WHERE a.dispatch_id=o.dispatch_id AND o.saga_id=p.saga_id AND p.execution_id=x.execution_id`,
		`DELETE FROM effectus_saga_outbox o USING effectus_execution_plans p, effectus_prune_executions x WHERE o.saga_id=p.saga_id AND p.execution_id=x.execution_id`,
		`DELETE FROM effectus_saga_steps s USING effectus_execution_plans p, effectus_prune_executions x WHERE s.saga_id=p.saga_id AND p.execution_id=x.execution_id`,
		`DELETE FROM effectus_fact_applications f USING effectus_prune_executions x WHERE f.execution_id=x.execution_id`,
		`DELETE FROM effectus_fact_snapshots f USING effectus_prune_executions x WHERE f.execution_id=x.execution_id`,
		`DELETE FROM effectus_execution_plans p USING effectus_prune_executions x WHERE p.execution_id=x.execution_id`,
		`DELETE FROM effectus_executions e USING effectus_prune_executions x WHERE e.execution_id=x.execution_id`,
		`DELETE FROM effectus_saga_instances s WHERE NOT EXISTS (SELECT 1 FROM effectus_execution_plans p WHERE p.saga_id=s.saga_id) AND s.state IN ('completed','compensated','failed') AND s.updated_at < now() - ($1 * interval '1 microsecond')`,
		`DELETE FROM effectus_rule_generations g WHERE g.state='retired' AND g.retired_at < now() - ($1 * interval '1 microsecond') AND NOT EXISTS (SELECT 1 FROM effectus_executions e WHERE e.generation_digest=g.generation_digest)`,
		`DELETE FROM effectus_execution_artifacts a WHERE a.created_at < now() - ($1 * interval '1 microsecond') AND NOT EXISTS (SELECT 1 FROM effectus_executions e WHERE e.generation_digest=a.generation_digest) AND NOT EXISTS (SELECT 1 FROM effectus_rule_generations g WHERE g.generation_digest=a.generation_digest)`,
	}
	for index, statement := range statements {
		if index < 7 {
			_, err = tx.ExecContext(ctx, statement)
		} else {
			_, err = tx.ExecContext(ctx, statement, options.Retention.Microseconds())
		}
		if err != nil {
			return PruneResult{}, err
		}
	}
	kafka, err := tx.ExecContext(ctx, `DELETE FROM effectus_kafka_deliveries WHERE delivery_id IN (SELECT delivery_id FROM effectus_kafka_deliveries WHERE poison_acknowledged AND updated_at < now() - ($1 * interval '1 microsecond') ORDER BY updated_at, delivery_id LIMIT $2)`, options.Retention.Microseconds(), options.BatchSize)
	if err != nil {
		return PruneResult{}, err
	}
	result.KafkaDeliveries, _ = kafka.RowsAffected()
	return result, tx.Commit()
}

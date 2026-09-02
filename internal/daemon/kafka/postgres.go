package kafka

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PostgresAttemptTracker records Kafka handler failures in the durable daemon
// ledger. It is safe to share between consumer instances in one consumer group.
type PostgresAttemptTracker struct{ db *sql.DB }

// NewPostgresAttemptTracker creates the production attempt tracker. The caller
// must have applied the Kafka delivery ledger migration before consumption.
func NewPostgresAttemptTracker(db *sql.DB) (*PostgresAttemptTracker, error) {
	if db == nil {
		return nil, fmt.Errorf("PostgreSQL database is required for Kafka attempt tracking")
	}
	return &PostgresAttemptTracker{db: db}, nil
}

func (tracker *PostgresAttemptTracker) Attempts(ctx context.Context, deliveryID string) (int, error) {
	var failures int
	err := tracker.db.QueryRowContext(ctx, `SELECT failures FROM effectus_kafka_deliveries WHERE delivery_id = $1`, deliveryID).Scan(&failures)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return failures, nil
}

func (tracker *PostgresAttemptTracker) RecordFailure(ctx context.Context, deliveryID string) (int, error) {
	var failures int
	err := tracker.db.QueryRowContext(ctx, `
		INSERT INTO effectus_kafka_deliveries (delivery_id, failures, updated_at)
		VALUES ($1, 1, now())
		ON CONFLICT (delivery_id) DO UPDATE
		SET failures = effectus_kafka_deliveries.failures + 1, updated_at = now()
		RETURNING failures
	`, deliveryID).Scan(&failures)
	if err != nil {
		return 0, err
	}
	return failures, nil
}

func (tracker *PostgresAttemptTracker) ClearAttempts(ctx context.Context, deliveryID string) error {
	_, err := tracker.db.ExecContext(ctx, `DELETE FROM effectus_kafka_deliveries WHERE delivery_id = $1`, deliveryID)
	return err
}

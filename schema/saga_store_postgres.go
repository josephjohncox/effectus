package schema

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresSagaStore persists saga effects in Postgres.
type PostgresSagaStore struct {
	db *sql.DB
}

// NewPostgresSagaStore creates a Postgres-backed saga store using the provided DSN.
func NewPostgresSagaStore(dsn string) (*PostgresSagaStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	store := &PostgresSagaStore{db: db}
	if err := store.ensureSchema(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (ps *PostgresSagaStore) StartTransaction(sagaID, ruleName string) error {
	if ps == nil || ps.db == nil {
		return fmt.Errorf("postgres saga store not initialized")
	}
	_, err := ps.db.Exec(`
		INSERT INTO effectus_sagas (saga_id, rule_name, status, created_at)
		VALUES ($1, $2, 'active', now())
		ON CONFLICT (saga_id) DO UPDATE SET
		  rule_name = EXCLUDED.rule_name,
		  status = 'active',
		  completed_at = NULL
	`, sagaID, ruleName)
	return err
}

func (ps *PostgresSagaStore) RecordEffect(sagaID, verb string, args map[string]interface{}) error {
	if ps == nil || ps.db == nil {
		return fmt.Errorf("postgres saga store not initialized")
	}
	payload, err := json.Marshal(args)
	if err != nil {
		return err
	}
	_, err = ps.db.Exec(`
		INSERT INTO effectus_saga_effects (saga_id, verb, status, args, created_at)
		VALUES ($1, $2, 'pending', $3, now())
	`, sagaID, verb, payload)
	return err
}

func (ps *PostgresSagaStore) MarkSuccess(sagaID, verb string) error {
	return ps.updateEffectStatus(sagaID, verb, "success", "")
}

func (ps *PostgresSagaStore) MarkFailed(sagaID, verb string, reason error) error {
	msg := ""
	if reason != nil {
		msg = reason.Error()
	}
	return ps.updateEffectStatus(sagaID, verb, "failed", msg)
}

func (ps *PostgresSagaStore) MarkCompensated(sagaID, verb string) error {
	return ps.updateEffectStatus(sagaID, verb, "compensated", "")
}

func (ps *PostgresSagaStore) GetTransactionEffects(sagaID string) ([]*SagaEffect, error) {
	if ps == nil || ps.db == nil {
		return nil, fmt.Errorf("postgres saga store not initialized")
	}
	rows, err := ps.db.Query(`
		SELECT verb, status, args, error, created_at
		FROM effectus_saga_effects
		WHERE saga_id = $1
		ORDER BY created_at ASC
	`, sagaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var effects []*SagaEffect
	for rows.Next() {
		var verb string
		var status string
		var argsJSON []byte
		var errMsg sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&verb, &status, &argsJSON, &errMsg, &createdAt); err != nil {
			return nil, err
		}
		args := map[string]interface{}{}
		if len(argsJSON) > 0 {
			_ = json.Unmarshal(argsJSON, &args)
		}
		effect := &SagaEffect{
			Verb:      verb,
			Args:      args,
			Status:    status,
			Timestamp: createdAt,
		}
		if errMsg.Valid {
			effect.Error = errMsg.String
		}
		effects = append(effects, effect)
	}
	return effects, rows.Err()
}

func (ps *PostgresSagaStore) GetActiveSagas() ([]string, error) {
	if ps == nil || ps.db == nil {
		return nil, fmt.Errorf("postgres saga store not initialized")
	}
	rows, err := ps.db.Query(`SELECT saga_id FROM effectus_sagas WHERE completed_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sagas []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		sagas = append(sagas, id)
	}
	return sagas, rows.Err()
}

func (ps *PostgresSagaStore) CompleteSaga(sagaID string) error {
	if ps == nil || ps.db == nil {
		return fmt.Errorf("postgres saga store not initialized")
	}
	_, err := ps.db.Exec(`
		UPDATE effectus_sagas
		SET status = 'completed', completed_at = now()
		WHERE saga_id = $1
	`, sagaID)
	return err
}

func (ps *PostgresSagaStore) updateEffectStatus(sagaID, verb, status, errMsg string) error {
	if ps == nil || ps.db == nil {
		return fmt.Errorf("postgres saga store not initialized")
	}
	var id int64
	err := ps.db.QueryRow(`
		SELECT id
		FROM effectus_saga_effects
		WHERE saga_id = $1 AND verb = $2 AND status = 'pending'
		ORDER BY created_at DESC
		LIMIT 1
	`, sagaID, verb).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("pending effect not found for saga %s verb %s", sagaID, verb)
		}
		return err
	}
	_, err = ps.db.Exec(`
		UPDATE effectus_saga_effects
		SET status = $2, error = $3
		WHERE id = $1
	`, id, status, nullableString(errMsg))
	return err
}

func (ps *PostgresSagaStore) ensureSchema(ctx context.Context) error {
	if ps == nil || ps.db == nil {
		return fmt.Errorf("postgres saga store not initialized")
	}
	_, err := ps.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS effectus_sagas (
			saga_id text PRIMARY KEY,
			rule_name text,
			status text NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			completed_at timestamptz
		);
		CREATE TABLE IF NOT EXISTS effectus_saga_effects (
			id bigserial PRIMARY KEY,
			saga_id text NOT NULL REFERENCES effectus_sagas(saga_id) ON DELETE CASCADE,
			verb text NOT NULL,
			status text NOT NULL,
			args jsonb,
			error text,
			created_at timestamptz NOT NULL DEFAULT now()
		);
	`)
	return err
}

func nullableString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: value, Valid: true}
}

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
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	store := &PostgresSagaStore{db: db}
	if err := store.ensureSchema(ctx); err != nil {
		_ = db.Close()
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

func (ps *PostgresSagaStore) RecordEffect(sagaID, effectID string, sequence int, verb string, args map[string]interface{}) error {
	if ps == nil || ps.db == nil {
		return fmt.Errorf("postgres saga store not initialized")
	}
	payload, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("marshal saga effect arguments: %w", err)
	}
	result, err := ps.db.Exec(`
		INSERT INTO effectus_saga_effects
			(saga_id, effect_id, sequence, verb, status, args, created_at)
		VALUES ($1, $2, $3, $4, 'pending', $5::jsonb, now())
		ON CONFLICT (saga_id, effect_id) DO UPDATE
		SET effect_id = EXCLUDED.effect_id
		WHERE effectus_saga_effects.sequence = EXCLUDED.sequence
		  AND effectus_saga_effects.verb = EXCLUDED.verb
		  AND effectus_saga_effects.args = EXCLUDED.args
	`, sagaID, effectID, sequence, verb, payload)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("effect identity conflict for saga %s effect %s", sagaID, effectID)
	}
	return nil
}

func (ps *PostgresSagaStore) MarkSuccess(sagaID, effectID string, result interface{}) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal saga effect result: %w", err)
	}
	return ps.updateEffectStatus(sagaID, effectID, SagaEffectSuccess, "", payload)
}

func (ps *PostgresSagaStore) MarkFailed(sagaID, effectID string, reason error) error {
	msg := ""
	if reason != nil {
		msg = reason.Error()
	}
	return ps.updateEffectStatus(sagaID, effectID, SagaEffectFailed, msg, nil)
}

func (ps *PostgresSagaStore) MarkCompensated(sagaID, effectID string) error {
	return ps.updateEffectStatus(sagaID, effectID, SagaEffectCompensated, "", nil)
}

func (ps *PostgresSagaStore) GetTransactionEffects(sagaID string) ([]*SagaEffect, error) {
	if ps == nil || ps.db == nil {
		return nil, fmt.Errorf("postgres saga store not initialized")
	}
	rows, err := ps.db.Query(`
		SELECT effect_id, sequence, verb, status, args, result, error, created_at
		FROM effectus_saga_effects
		WHERE saga_id = $1
		ORDER BY sequence ASC, id ASC
	`, sagaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var effects []*SagaEffect
	for rows.Next() {
		var effectID, verb, status string
		var sequence int
		var argsJSON, resultJSON []byte
		var errMsg sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&effectID, &sequence, &verb, &status, &argsJSON, &resultJSON, &errMsg, &createdAt); err != nil {
			return nil, err
		}
		args := map[string]interface{}{}
		if len(argsJSON) > 0 {
			if err := json.Unmarshal(argsJSON, &args); err != nil {
				return nil, fmt.Errorf("decode arguments for effect %s: %w", effectID, err)
			}
		}
		var result interface{}
		if len(resultJSON) > 0 {
			if err := json.Unmarshal(resultJSON, &result); err != nil {
				return nil, fmt.Errorf("decode result for effect %s: %w", effectID, err)
			}
		}
		effect := &SagaEffect{
			ID:        effectID,
			Sequence:  sequence,
			Verb:      verb,
			Args:      args,
			Result:    result,
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

func (ps *PostgresSagaStore) updateEffectStatus(sagaID, effectID, status, errMsg string, resultJSON []byte) error {
	if ps == nil || ps.db == nil {
		return fmt.Errorf("postgres saga store not initialized")
	}
	result, err := ps.db.Exec(`
		UPDATE effectus_saga_effects
		SET status = $3,
		    error = $4,
		    result = CASE WHEN $3 = 'success' THEN $5::jsonb ELSE result END
		WHERE saga_id = $1
		  AND effect_id = $2
		  AND (
		    ($3 IN ('success', 'failed') AND status IN ('pending', $3))
		    OR ($3 = 'compensated' AND status IN ('success', 'compensated'))
		  )
	`, sagaID, effectID, status, nullableString(errMsg), resultJSON)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("effect %s for saga %s is missing or has an invalid status transition to %s", effectID, sagaID, status)
	}
	return nil
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
			effect_id text,
			sequence integer,
			verb text NOT NULL,
			status text NOT NULL,
			args jsonb,
			result jsonb,
			error text,
			created_at timestamptz NOT NULL DEFAULT now()
		);
		ALTER TABLE effectus_saga_effects ADD COLUMN IF NOT EXISTS effect_id text;
		ALTER TABLE effectus_saga_effects ADD COLUMN IF NOT EXISTS sequence integer;
		ALTER TABLE effectus_saga_effects ADD COLUMN IF NOT EXISTS result jsonb;
		UPDATE effectus_saga_effects
		SET effect_id = 'legacy-' || id::text
		WHERE effect_id IS NULL;
		UPDATE effectus_saga_effects
		SET sequence = id::integer
		WHERE sequence IS NULL;
		ALTER TABLE effectus_saga_effects ALTER COLUMN effect_id SET NOT NULL;
		ALTER TABLE effectus_saga_effects ALTER COLUMN sequence SET NOT NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS effectus_saga_effect_identity
			ON effectus_saga_effects (saga_id, effect_id);
	`)
	return err
}

func nullableString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: value, Valid: true}
}

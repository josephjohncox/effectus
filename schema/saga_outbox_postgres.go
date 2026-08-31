package schema

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/josephjohncox/effectus/invocation"
)

// PostgresOutboxStore implements the V2 saga protocol. It never creates or
// alters tables at startup. Apply schema/migrations before construction.
type PostgresOutboxStore struct {
	db *sql.DB
}

func NewPostgresOutboxStore(db *sql.DB) (*PostgresOutboxStore, error) {
	if db == nil {
		return nil, fmt.Errorf("PostgreSQL outbox database is required")
	}
	return &PostgresOutboxStore{db: db}, nil
}

func (store *PostgresOutboxStore) CreateSaga(ctx context.Context, request CreateSagaRequest) (*SagaInstance, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	saga, err := createSagaTx(ctx, tx, request)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return saga, nil
}

func (store *PostgresOutboxStore) EnqueueStep(ctx context.Context, request EnqueueStepRequest) (*Dispatch, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	dispatch, err := enqueueStepTx(ctx, tx, request)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return dispatch, nil
}

func (store *PostgresOutboxStore) ClaimDispatch(ctx context.Context, options ClaimOptions) (*Dispatch, error) {
	if options.Owner == "" || options.LeaseDuration <= 0 {
		return nil, fmt.Errorf("claim owner and positive lease duration are required")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var dispatchID string
	err = tx.QueryRowContext(ctx, `
		SELECT o.dispatch_id
		FROM effectus_saga_outbox o
		JOIN effectus_saga_instances s ON s.saga_id = o.saga_id
		WHERE ($1 = '' OR o.dispatch_id = $1)
		AND (
			o.state = 'queued'
			OR (o.state = 'retry_wait' AND COALESCE(o.next_attempt_at, '-infinity'::timestamptz) <= now())
			OR (o.state = 'in_flight' AND o.lease_deadline <= now())
		)
		AND ((s.state = 'running' AND o.direction = 'forward')
		     OR (s.state = 'compensating' AND o.direction = 'compensation'))
		AND (NOT o.serial_saga OR NOT EXISTS (
			SELECT 1 FROM effectus_saga_outbox active
			WHERE active.saga_id = o.saga_id AND active.state = 'in_flight'
			  AND active.lease_deadline > now() AND active.dispatch_id <> o.dispatch_id
		))
		ORDER BY o.created_at, o.saga_id, o.sequence, o.dispatch_id
		FOR UPDATE OF o SKIP LOCKED
		LIMIT 1
	`, options.TargetDispatchID).Scan(&dispatchID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoDispatch
	}
	if err != nil {
		return nil, fmt.Errorf("select eligible dispatch: %w", err)
	}
	token, err := randomLeaseToken()
	if err != nil {
		return nil, err
	}
	var attempt uint64
	err = tx.QueryRowContext(ctx, `
		UPDATE effectus_saga_outbox
		SET state = 'in_flight', attempt = attempt + 1, lease_owner = $2, lease_token = $3,
		    lease_deadline = now() + ($4 * interval '1 microsecond'), next_attempt_at = NULL,
		    fencing_grants = '[]'::jsonb, revision = revision + 1, updated_at = now()
		WHERE dispatch_id = $1
		RETURNING attempt
	`, dispatchID, options.Owner, token, options.LeaseDuration.Microseconds()).Scan(&attempt)
	if err != nil {
		return nil, fmt.Errorf("claim dispatch: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO effectus_saga_attempts
			(dispatch_id, attempt, lease_owner, lease_token, lease_deadline, started_at)
		SELECT dispatch_id, attempt, lease_owner, lease_token, lease_deadline, now()
		FROM effectus_saga_outbox WHERE dispatch_id = $1
	`, dispatchID)
	if err != nil {
		return nil, fmt.Errorf("record dispatch attempt: %w", err)
	}
	dispatch, err := getDispatchTx(ctx, tx, dispatchID, false)
	if err != nil {
		return nil, err
	}
	if dispatch.Attempt != attempt {
		return nil, fmt.Errorf("claimed attempt mismatch")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return dispatch, nil
}

func (store *PostgresOutboxStore) SaveFencingGrants(ctx context.Context, dispatchID string, attempt uint64, leaseToken string, grants []invocation.FencingGrant) error {
	if err := validateGrants(grants); err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	dispatch, err := getDispatchTx(ctx, tx, dispatchID, true)
	if err != nil {
		return err
	}
	if !currentLease(dispatch, attempt, leaseToken) {
		return ErrStaleLease
	}
	if len(dispatch.Fencing) != len(grants) {
		return fmt.Errorf("fencing grant count does not match dispatch requirements")
	}
	for index, requirement := range dispatch.Fencing {
		if grants[index].Authority != requirement.Authority || grants[index].Resource != requirement.Resource {
			return fmt.Errorf("fencing grant %d does not match dispatch requirement", index)
		}
	}
	if len(dispatch.FencingGrants) != 0 && !sameGrants(dispatch.FencingGrants, grants) {
		return fmt.Errorf("%w: fencing grants already persisted", ErrIdentityConflict)
	}
	payload, err := json.Marshal(grants)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE effectus_saga_outbox
		SET fencing_grants = $4::jsonb, revision = revision + 1, updated_at = now()
		WHERE dispatch_id = $1 AND state = 'in_flight' AND attempt = $2 AND lease_token = $3
		  AND lease_deadline > now()
	`, dispatchID, attempt, leaseToken, payload)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ErrStaleLease
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE effectus_saga_attempts SET fencing_grants = $4::jsonb
		WHERE dispatch_id = $1 AND attempt = $2 AND lease_token = $3
	`, dispatchID, attempt, leaseToken, payload)
	if err != nil {
		return err
	}
	rows, _ = result.RowsAffected()
	if rows != 1 {
		return ErrStaleLease
	}
	return tx.Commit()
}

func (store *PostgresOutboxStore) CompleteDispatch(ctx context.Context, completion Completion) error {
	if completion.Now.IsZero() {
		completion.Now = time.Now().UTC()
	}
	if err := validateCompletion(completion); err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	dispatch, err := getDispatchTx(ctx, tx, completion.DispatchID, true)
	if err != nil {
		return err
	}
	if !currentLease(dispatch, completion.Attempt, completion.LeaseToken) {
		return ErrStaleLease
	}
	saga, err := getSagaTx(ctx, tx, dispatch.SagaID, true)
	if err != nil {
		return err
	}
	step, err := getStepTx(ctx, tx, dispatch.SagaID, dispatch.EffectID, true)
	if err != nil {
		return err
	}
	if err := applyCompletion(dispatch, step, saga, completion); err != nil {
		return err
	}
	updateResult, err := tx.ExecContext(ctx, `
		UPDATE effectus_saga_outbox
		SET state = $4, lease_owner = NULL, lease_token = NULL, lease_deadline = NULL,
		    next_attempt_at = $5, last_outcome = $6, last_error = $7,
		    result = $8::jsonb, revision = $9, updated_at = $10
		WHERE dispatch_id = $1 AND state = 'in_flight' AND attempt = $2 AND lease_token = $3
		  AND lease_deadline > now()
	`, dispatch.ID, completion.Attempt, completion.LeaseToken, dispatch.State,
		nullTime(dispatch.NextAttemptAt), nullString(string(dispatch.LastOutcome)), nullString(dispatch.LastError),
		nullJSON(dispatch.Result), dispatch.Revision, completion.Now)
	if err != nil {
		return err
	}
	updatedRows, err := updateResult.RowsAffected()
	if err != nil {
		return err
	}
	if updatedRows != 1 {
		return ErrStaleLease
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE effectus_saga_steps SET state = $3, result = $4::jsonb
		WHERE saga_id = $1 AND effect_id = $2
	`, step.SagaID, step.EffectID, step.State, nullJSON(step.Result))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE effectus_saga_instances SET state = $2, revision = $3, updated_at = $4 WHERE saga_id = $1
	`, saga.SagaID, saga.State, saga.Revision, completion.Now)
	if err != nil {
		return err
	}
	attemptResult, err := tx.ExecContext(ctx, `
		UPDATE effectus_saga_attempts
		SET outcome = $4, error = $5, completed_at = $6
		WHERE dispatch_id = $1 AND attempt = $2 AND lease_token = $3
	`, dispatch.ID, completion.Attempt, completion.LeaseToken, completion.Outcome, nullString(completion.Error), completion.Now)
	if err != nil {
		return err
	}
	attemptRows, err := attemptResult.RowsAffected()
	if err != nil {
		return err
	}
	if attemptRows != 1 {
		return ErrStaleLease
	}
	if dispatch.Direction == invocation.DirectionForward && (completion.Outcome == invocation.OutcomePermanentFailure ||
		(completion.Outcome == invocation.OutcomeRetryableKnownNotCommitted && completion.Exhausted)) {
		if err := startCompensationPostgres(ctx, tx, saga, completion.Now); err != nil {
			return err
		}
	}
	if dispatch.Direction == invocation.DirectionCompensation && completion.Outcome == invocation.OutcomeSuccess {
		if err := enqueueNextCompensationPostgres(ctx, tx, saga, completion.Now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (store *PostgresOutboxStore) CompleteSaga(ctx context.Context, sagaID string) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	saga, err := getSagaTx(ctx, tx, sagaID, true)
	if err != nil {
		return err
	}
	if saga.State == SagaCompleted {
		return tx.Commit()
	}
	if saga.State != SagaRunning {
		return fmt.Errorf("%w: cannot complete saga from %s", ErrInvalidTransition, saga.State)
	}
	var incomplete int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM effectus_saga_outbox WHERE saga_id = $1 AND state <> 'succeeded'
	`, sagaID).Scan(&incomplete); err != nil {
		return err
	}
	if incomplete != 0 {
		return fmt.Errorf("%w: saga has %d nonterminal dispatches", ErrInvalidTransition, incomplete)
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE effectus_saga_instances
		SET state = 'completed', revision = revision + 1, updated_at = now()
		WHERE saga_id = $1 AND state = 'running'
	`, sagaID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (store *PostgresOutboxStore) GetSaga(ctx context.Context, sagaID string) (*SagaInstance, error) {
	return scanSaga(store.db.QueryRowContext(ctx, `
		SELECT namespace, saga_id, execution_id, plan_id, plan_digest, state, serial,
		       revision, created_at, updated_at
		FROM effectus_saga_instances WHERE saga_id = $1
	`, sagaID))
}

func (store *PostgresOutboxStore) GetDispatch(ctx context.Context, dispatchID string) (*Dispatch, error) {
	return scanDispatch(store.db.QueryRowContext(ctx, dispatchSelect+` WHERE dispatch_id = $1`, dispatchID))
}

func (store *PostgresOutboxStore) ListDispatches(ctx context.Context, sagaID string) ([]*Dispatch, error) {
	rows, err := store.db.QueryContext(ctx, dispatchSelect+` WHERE saga_id = $1 ORDER BY sequence, direction`, sagaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Dispatch
	for rows.Next() {
		dispatch, err := scanDispatch(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, dispatch)
	}
	return result, rows.Err()
}

func (store *PostgresOutboxStore) ListAttempts(ctx context.Context, dispatchID string) ([]DispatchAttempt, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT dispatch_id, attempt, lease_owner, lease_token, lease_deadline,
		       fencing_grants, outcome, error, started_at, completed_at
		FROM effectus_saga_attempts WHERE dispatch_id = $1 ORDER BY attempt
	`, dispatchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var attempts []DispatchAttempt
	for rows.Next() {
		var attempt DispatchAttempt
		var grantsJSON []byte
		var outcome, errorMessage sql.NullString
		var completedAt sql.NullTime
		if err := rows.Scan(&attempt.DispatchID, &attempt.Attempt, &attempt.LeaseOwner, &attempt.LeaseToken,
			&attempt.LeaseDeadline, &grantsJSON, &outcome, &errorMessage, &attempt.StartedAt, &completedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(grantsJSON, &attempt.FencingGrants); err != nil {
			return nil, err
		}
		attempt.Outcome = invocation.OutcomeClass(outcome.String)
		attempt.Error = errorMessage.String
		if completedAt.Valid {
			attempt.CompletedAt = completedAt.Time
		}
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

const dispatchSelect = `
	SELECT dispatch_id, saga_id, effect_id, sequence, direction, verb, contract_hash,
	       arguments, argument_hash, idempotency_key, state, attempt, lease_owner,
	       lease_token, lease_deadline, next_attempt_at, fencing_requirements,
	       fencing_grants, last_outcome, last_error, result, revision, created_at, updated_at
	FROM effectus_saga_outbox`

type rowScanner interface{ Scan(...any) error }

func scanSaga(row rowScanner) (*SagaInstance, error) {
	var saga SagaInstance
	if err := row.Scan(&saga.Namespace, &saga.SagaID, &saga.ExecutionID, &saga.PlanID, &saga.PlanDigest,
		&saga.State, &saga.Serial, &saga.Revision, &saga.CreatedAt, &saga.UpdatedAt); err != nil {
		return nil, err
	}
	return &saga, nil
}

func scanDispatch(row rowScanner) (*Dispatch, error) {
	var dispatch Dispatch
	var direction string
	var leaseOwner, leaseToken, lastOutcome, lastError sql.NullString
	var leaseDeadline, nextAttempt sql.NullTime
	var fencingJSON, grantsJSON, resultJSON []byte
	if err := row.Scan(&dispatch.ID, &dispatch.SagaID, &dispatch.EffectID, &dispatch.Sequence,
		&direction, &dispatch.Verb, &dispatch.ContractHash, &dispatch.Arguments, &dispatch.ArgumentHash,
		&dispatch.IdempotencyKey, &dispatch.State, &dispatch.Attempt, &leaseOwner, &leaseToken,
		&leaseDeadline, &nextAttempt, &fencingJSON, &grantsJSON, &lastOutcome, &lastError,
		&resultJSON, &dispatch.Revision, &dispatch.CreatedAt, &dispatch.UpdatedAt); err != nil {
		return nil, err
	}
	dispatch.Direction = invocation.Direction(direction)
	dispatch.LeaseOwner = leaseOwner.String
	dispatch.LeaseToken = leaseToken.String
	if leaseDeadline.Valid {
		dispatch.LeaseDeadline = leaseDeadline.Time
	}
	if nextAttempt.Valid {
		dispatch.NextAttemptAt = nextAttempt.Time
	}
	if err := json.Unmarshal(fencingJSON, &dispatch.Fencing); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(grantsJSON, &dispatch.FencingGrants); err != nil {
		return nil, err
	}
	dispatch.LastOutcome = invocation.OutcomeClass(lastOutcome.String)
	dispatch.LastError = lastError.String
	dispatch.Result = append(json.RawMessage(nil), resultJSON...)
	return &dispatch, nil
}

func getSagaTx(ctx context.Context, tx *sql.Tx, sagaID string, forUpdate bool) (*SagaInstance, error) {
	query := `SELECT namespace, saga_id, execution_id, plan_id, plan_digest, state, serial, revision, created_at, updated_at
		FROM effectus_saga_instances WHERE saga_id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	return scanSaga(tx.QueryRowContext(ctx, query, sagaID))
}

func getDispatchTx(ctx context.Context, tx *sql.Tx, dispatchID string, forUpdate bool) (*Dispatch, error) {
	query := dispatchSelect + ` WHERE dispatch_id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	return scanDispatch(tx.QueryRowContext(ctx, query, dispatchID))
}

func getDispatchByIdentityTx(ctx context.Context, tx *sql.Tx, sagaID, effectID string, direction invocation.Direction) (*Dispatch, error) {
	return scanDispatch(tx.QueryRowContext(ctx, dispatchSelect+`
		WHERE saga_id = $1 AND effect_id = $2 AND direction = $3
	`, sagaID, effectID, direction))
}

func getStepTx(ctx context.Context, tx *sql.Tx, sagaID, effectID string, forUpdate bool) (*SagaStep, error) {
	query := `SELECT saga_id, effect_id, sequence, verb, contract_hash, arguments, argument_hash,
		compensation_verb, compensation_contract_hash, compensation_arguments,
		compensation_argument_hash, fencing_requirements, state, result
		FROM effectus_saga_steps WHERE saga_id = $1 AND effect_id = $2`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var step SagaStep
	var compensationVerb, compensationContract, compensationHash sql.NullString
	var compensationArguments, fencingJSON, result []byte
	if err := tx.QueryRowContext(ctx, query, sagaID, effectID).Scan(
		&step.SagaID, &step.EffectID, &step.Sequence, &step.Verb, &step.ContractHash,
		&step.Arguments, &step.ArgumentHash, &compensationVerb, &compensationContract,
		&compensationArguments, &compensationHash, &fencingJSON, &step.State, &result,
	); err != nil {
		return nil, err
	}
	step.CompensationVerb = compensationVerb.String
	step.CompensationContract = compensationContract.String
	step.CompensationArguments = append(json.RawMessage(nil), compensationArguments...)
	step.CompensationArgumentHash = compensationHash.String
	step.Result = append(json.RawMessage(nil), result...)
	if err := json.Unmarshal(fencingJSON, &step.Fencing); err != nil {
		return nil, err
	}
	return &step, nil
}

func startCompensationPostgres(ctx context.Context, tx *sql.Tx, saga *SagaInstance, now time.Time) error {
	step, err := compensationCandidatePostgres(ctx, tx, saga.SagaID)
	if errors.Is(err, sql.ErrNoRows) {
		hasSucceeded, checkErr := hasSucceededStepPostgres(ctx, tx, saga.SagaID)
		if checkErr != nil {
			return checkErr
		}
		if hasSucceeded {
			saga.State = SagaBlockedCompensation
		} else {
			saga.State = SagaFailed
		}
	} else if err != nil {
		return err
	} else {
		saga.State = SagaCompensating
		if err := enqueueCompensationPostgres(ctx, tx, saga, step); err != nil {
			return err
		}
	}
	saga.Revision++
	_, err = tx.ExecContext(ctx, `UPDATE effectus_saga_instances SET state = $2, revision = $3, updated_at = $4 WHERE saga_id = $1`,
		saga.SagaID, saga.State, saga.Revision, now)
	return err
}

func enqueueNextCompensationPostgres(ctx context.Context, tx *sql.Tx, saga *SagaInstance, now time.Time) error {
	step, err := compensationCandidatePostgres(ctx, tx, saga.SagaID)
	if errors.Is(err, sql.ErrNoRows) {
		hasSucceeded, checkErr := hasSucceededStepPostgres(ctx, tx, saga.SagaID)
		if checkErr != nil {
			return checkErr
		}
		if hasSucceeded {
			saga.State = SagaBlockedCompensation
		} else {
			saga.State = SagaCompensated
		}
		saga.Revision++
		_, err = tx.ExecContext(ctx, `UPDATE effectus_saga_instances SET state = $2, revision = $3, updated_at = $4 WHERE saga_id = $1`,
			saga.SagaID, saga.State, saga.Revision, now)
		return err
	}
	if err != nil {
		return err
	}
	return enqueueCompensationPostgres(ctx, tx, saga, step)
}

func hasSucceededStepPostgres(ctx context.Context, tx *sql.Tx, sagaID string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM effectus_saga_steps WHERE saga_id = $1 AND state = 'succeeded'
	)`, sagaID).Scan(&exists)
	return exists, err
}

func compensationCandidatePostgres(ctx context.Context, tx *sql.Tx, sagaID string) (*SagaStep, error) {
	var effectID string
	if err := tx.QueryRowContext(ctx, `
		SELECT effect_id FROM effectus_saga_steps
		WHERE saga_id = $1 AND state = 'succeeded' AND compensation_verb IS NOT NULL
		ORDER BY sequence DESC LIMIT 1 FOR UPDATE
	`, sagaID).Scan(&effectID); err != nil {
		return nil, err
	}
	return getStepTx(ctx, tx, sagaID, effectID, false)
}

func enqueueCompensationPostgres(ctx context.Context, tx *sql.Tx, saga *SagaInstance, step *SagaStep) error {
	key := IdempotencyKey(saga.Namespace, saga.SagaID, step.EffectID, invocation.DirectionCompensation)
	fencingJSON, _ := json.Marshal(step.Fencing)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO effectus_saga_outbox
			(dispatch_id, saga_id, effect_id, sequence, direction, verb, contract_hash,
			 arguments, argument_hash, idempotency_key, state, fencing_requirements, serial_saga)
		VALUES ($1, $2, $3, $4, 'compensation', $5, $6, $7::jsonb, $8, $9, 'queued', $10::jsonb, $11)
		ON CONFLICT (saga_id, effect_id, direction) DO NOTHING
	`, "dispatch/"+key, saga.SagaID, step.EffectID, step.Sequence, step.CompensationVerb,
		step.CompensationContract, step.CompensationArguments, step.CompensationArgumentHash,
		key, fencingJSON, saga.Serial)
	return err
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

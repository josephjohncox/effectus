package schema

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/effectus/effectus-go/invocation"
)

func (store *PostgresOutboxStore) PutArtifact(ctx context.Context, artifact ExecutionArtifact) error {
	if err := validateExecutionArtifact(artifact); err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := putArtifactTx(ctx, tx, artifact); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *PostgresOutboxStore) GetArtifact(ctx context.Context, digest string) (ExecutionArtifact, error) {
	return getArtifactQuery(ctx, store.db, digest)
}

func (store *PostgresOutboxStore) AdmitExecution(ctx context.Context, admission DurableAdmission) (ExecutionRecord, bool, error) {
	return store.AdmitExecutionAtomic(ctx, admission)
}

func (store *PostgresOutboxStore) AdmitExecutionAtomic(ctx context.Context, admission DurableAdmission) (ExecutionRecord, bool, error) {
	const maxAttempts = 6
	for attempt := 0; attempt < maxAttempts; attempt++ {
		record, created, err := store.admitExecutionAtomicOnce(ctx, admission)
		if err == nil {
			return record, created, nil
		}
		if !isRetryablePostgresAdmissionError(err) {
			return ExecutionRecord{}, false, err
		}
		if existing, getErr := store.GetExecutionByAdmission(ctx, admission.Execution.AdmissionIdentity); getErr == nil {
			if existing.ExecutionID != admission.Execution.ExecutionID ||
				existing.RequestHash != admission.Execution.RequestHash ||
				existing.GenerationDigest != admission.Execution.GenerationDigest {
				return ExecutionRecord{}, false, fmt.Errorf("%w: admission identity %s", ErrIdentityConflict, admission.Execution.AdmissionIdentity)
			}
			return existing, false, nil
		} else if !errors.Is(getErr, ErrExecutionNotFound) {
			return ExecutionRecord{}, false, getErr
		}
		if attempt+1 == maxAttempts {
			return ExecutionRecord{}, false, fmt.Errorf("admit execution after %d concurrency retries: %w", maxAttempts, err)
		}
		delay := time.Duration(1<<attempt) * 5 * time.Millisecond
		select {
		case <-ctx.Done():
			return ExecutionRecord{}, false, ctx.Err()
		case <-time.After(delay):
		}
	}
	return ExecutionRecord{}, false, fmt.Errorf("admission retry loop exhausted")
}

func isRetryablePostgresAdmissionError(err error) bool {
	var sqlState interface{ SQLState() string }
	if !errors.As(err, &sqlState) {
		return false
	}
	switch sqlState.SQLState() {
	case "23505", "40001", "40P01":
		return true
	default:
		return false
	}
}

func (store *PostgresOutboxStore) admitExecutionAtomicOnce(ctx context.Context, admission DurableAdmission) (ExecutionRecord, bool, error) {
	if err := validateDurableAdmission(admission); err != nil {
		return ExecutionRecord{}, false, err
	}
	if len(admission.Sagas) != len(admission.Plans) {
		return ExecutionRecord{}, false, fmt.Errorf("selected plan and saga counts differ")
	}
	if len(admission.InitialSteps) > len(admission.Plans) {
		return ExecutionRecord{}, false, fmt.Errorf("too many initial steps")
	}
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ExecutionRecord{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	record, created, err := admitExecutionLedgerTx(ctx, tx, admission, false)
	if err != nil || !created {
		return record, created, err
	}
	for index, sagaRequest := range admission.Sagas {
		if sagaRequest.ExecutionID != record.ExecutionID || sagaRequest.SagaID != admission.Plans[index].SagaID || sagaRequest.PlanID != admission.Plans[index].PlanID {
			return ExecutionRecord{}, false, fmt.Errorf("admission saga %d does not match selected plan", index)
		}
		if _, err := createSagaTx(ctx, tx, sagaRequest); err != nil {
			return ExecutionRecord{}, false, err
		}
	}
	if err := insertExecutionPlansTx(ctx, tx, admission.Plans); err != nil {
		return ExecutionRecord{}, false, err
	}
	initialBySaga := make(map[string]struct{}, len(admission.InitialSteps))
	for _, step := range admission.InitialSteps {
		if _, err := enqueueStepTx(ctx, tx, step); err != nil {
			return ExecutionRecord{}, false, err
		}
		initialBySaga[step.SagaID] = struct{}{}
	}
	for _, saga := range admission.Sagas {
		if _, hasStep := initialBySaga[saga.SagaID]; hasStep {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE effectus_saga_instances SET state = 'completed', revision = revision + 1, updated_at = now()
			WHERE saga_id = $1 AND state = 'running'
		`, saga.SagaID); err != nil {
			return ExecutionRecord{}, false, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE effectus_execution_plans SET state = 'completed'
			WHERE execution_id = $1 AND saga_id = $2
		`, record.ExecutionID, saga.SagaID); err != nil {
			return ExecutionRecord{}, false, err
		}
	}
	if len(admission.InitialSteps) == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE effectus_executions SET state = 'completed', revision = revision + 1, updated_at = now() WHERE execution_id = $1`, record.ExecutionID); err != nil {
			return ExecutionRecord{}, false, err
		}
		record.State = ExecutionCompleted
		record.Revision++
		for index := range record.Plans {
			record.Plans[index].State = "completed"
		}
	}
	if err := tx.Commit(); err != nil {
		return ExecutionRecord{}, false, err
	}
	return record, true, nil
}

func (store *PostgresOutboxStore) GetExecution(ctx context.Context, id string) (ExecutionRecord, error) {
	return getExecutionQuery(ctx, store.db, id)
}

func (store *PostgresOutboxStore) GetExecutionByAdmission(ctx context.Context, identity string) (ExecutionRecord, error) {
	var id string
	err := store.db.QueryRowContext(ctx, `SELECT execution_id FROM effectus_executions WHERE admission_identity = $1`, identity).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ExecutionRecord{}, fmt.Errorf("%w: admission %s", ErrExecutionNotFound, identity)
	}
	if err != nil {
		return ExecutionRecord{}, err
	}
	return store.GetExecution(ctx, id)
}

func (store *PostgresOutboxStore) SetExecutionState(ctx context.Context, id string, revision uint64, state ExecutionState, message string) (ExecutionRecord, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return ExecutionRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE effectus_executions
		SET state = $3, last_error = NULLIF($4, ''), revision = revision + 1, updated_at = now()
		WHERE execution_id = $1 AND revision = $2
	`, id, revision, state, message)
	if err != nil {
		return ExecutionRecord{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ExecutionRecord{}, err
	}
	if rows == 0 {
		return ExecutionRecord{}, ErrOptimisticConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE effectus_execution_plans SET state = $2 WHERE execution_id = $1`, id, executionPlanDisposition(state)); err != nil {
		return ExecutionRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ExecutionRecord{}, err
	}
	return store.GetExecution(ctx, id)
}

func (store *PostgresOutboxStore) LeaseExecutions(ctx context.Context, owner string, limit int, duration time.Duration) ([]ExecutionLease, error) {
	if owner == "" || limit <= 0 || duration <= 0 {
		return nil, fmt.Errorf("recovery owner, positive limit, and lease duration are required")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
		SELECT execution_id
		FROM effectus_executions
		WHERE state NOT IN ('completed', 'failed', 'blocked_unknown', 'blocked_fence', 'blocked_dependency', 'blocked_compensation')
		  AND (recovery_token IS NULL OR recovery_deadline <= now())
		ORDER BY updated_at, execution_id
		FOR UPDATE SKIP LOCKED
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	leases := make([]ExecutionLease, 0, len(ids))
	for _, id := range ids {
		token, err := executionLeaseToken()
		if err != nil {
			return nil, err
		}
		var lease ExecutionLease
		lease.ExecutionID, lease.Owner, lease.Token = id, owner, token
		err = tx.QueryRowContext(ctx, `
			UPDATE effectus_executions
			SET recovery_owner = $2, recovery_token = $3,
			    recovery_deadline = now() + ($4 * interval '1 microsecond'),
			    revision = revision + 1, updated_at = now()
			WHERE execution_id = $1
			RETURNING recovery_deadline, revision
		`, id, owner, token, duration.Microseconds()).Scan(&lease.Deadline, &lease.Revision)
		if err != nil {
			return nil, err
		}
		leases = append(leases, lease)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return leases, nil
}

func (store *PostgresOutboxStore) FinishExecutionLease(ctx context.Context, lease ExecutionLease, state ExecutionState, message string) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE effectus_executions
		SET state = CASE WHEN $6 = '' THEN state ELSE $6 END,
		    last_error = NULLIF($5, ''), recovery_owner = NULL, recovery_token = NULL,
		    recovery_deadline = NULL, revision = revision + 1, updated_at = now()
		WHERE execution_id = $1 AND recovery_owner = $2 AND recovery_token = $3 AND revision = $4
	`, lease.ExecutionID, lease.Owner, lease.Token, lease.Revision, message, state)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrStaleExecutionLease
	}
	if state != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE effectus_execution_plans SET state = $2 WHERE execution_id = $1`, lease.ExecutionID, executionPlanDisposition(state)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func putArtifactTx(ctx context.Context, tx *sql.Tx, artifact ExecutionArtifact) error {
	result, err := tx.ExecContext(ctx, `
		INSERT INTO effectus_execution_artifacts
			(generation_digest, ir_digest, ir_bytes, environment, executor_manifest,
			 function_manifest, source_digest, compiler_metadata)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6::jsonb, $7, $8::jsonb)
		ON CONFLICT (generation_digest) DO NOTHING
	`, artifact.GenerationDigest, artifact.IRDigest, artifact.IRBytes, artifact.Environment,
		artifact.ExecutorManifest, artifact.FunctionManifest, artifact.SourceDigest, artifact.CompilerMetadata)
	if err != nil {
		return fmt.Errorf("insert execution artifact: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 1 {
		return nil
	}
	existing, err := getArtifactQuery(ctx, tx, artifact.GenerationDigest)
	if err != nil {
		return err
	}
	if !sameExecutionArtifact(existing, artifact) {
		return fmt.Errorf("%w: generation artifact %s", ErrIdentityConflict, artifact.GenerationDigest)
	}
	return nil
}

func admitExecutionLedgerTx(ctx context.Context, tx *sql.Tx, admission DurableAdmission, insertPlans bool) (ExecutionRecord, bool, error) {
	if err := putArtifactTx(ctx, tx, admission.Artifact); err != nil {
		return ExecutionRecord{}, false, err
	}
	var existingID, existingHash string
	err := tx.QueryRowContext(ctx, `
		SELECT execution_id, request_hash FROM effectus_executions
		WHERE admission_identity = $1 FOR UPDATE
	`, admission.Execution.AdmissionIdentity).Scan(&existingID, &existingHash)
	if err == nil {
		if existingID != admission.Execution.ExecutionID || existingHash != admission.Execution.RequestHash {
			return ExecutionRecord{}, false, fmt.Errorf("%w: admission identity %s", ErrIdentityConflict, admission.Execution.AdmissionIdentity)
		}
		record, err := getExecutionQuery(ctx, tx, existingID)
		return record, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ExecutionRecord{}, false, err
	}
	facts := admission.Execution.EffectiveFacts
	if _, err = tx.ExecContext(ctx, `
		UPDATE effectus_rule_generations SET state = 'retired', retired_at = now()
		WHERE ruleset = $1 AND environment = $2 AND state = 'active'
		  AND (generation_digest <> $3 OR version <> $4)
	`, admission.Execution.Ruleset, admission.Execution.TenantNamespace, admission.Execution.GenerationDigest, admission.Execution.Version); err != nil {
		return ExecutionRecord{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO effectus_rule_generations (ruleset, version, environment, generation_digest, state)
		VALUES ($1, $2, $3, $4, 'active')
		ON CONFLICT (ruleset, version, generation_digest)
		DO UPDATE SET state = 'active', retired_at = NULL
	`, admission.Execution.Ruleset, admission.Execution.Version, admission.Execution.TenantNamespace, admission.Execution.GenerationDigest); err != nil {
		return ExecutionRecord{}, false, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO effectus_executions
			(execution_id, admission_identity, request_hash, ruleset, version, tenant_namespace,
			 merge_policy, generation_digest, effective_facts, state, revision)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, 'accepted', 1)
	`, admission.Execution.ExecutionID, admission.Execution.AdmissionIdentity, admission.Execution.RequestHash,
		admission.Execution.Ruleset, admission.Execution.Version, admission.Execution.TenantNamespace,
		admission.Execution.MergePolicy, admission.Execution.GenerationDigest, facts)
	if err != nil {
		return ExecutionRecord{}, false, fmt.Errorf("insert execution: %w", err)
	}
	application := admission.FactApplication
	_, err = tx.ExecContext(ctx, `
		INSERT INTO effectus_fact_applications
			(execution_id, fact_event_id, merge_policy, facts, applied_revision)
		VALUES ($1, $2, $3, $4::jsonb, $5)
	`, application.ExecutionID, application.FactEventID, application.MergePolicy, application.Facts, application.AppliedRevision)
	if err != nil {
		return ExecutionRecord{}, false, fmt.Errorf("insert fact application: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO effectus_fact_snapshots (execution_id, universe, revision)
		VALUES ($1, $2::jsonb, $3)
	`, admission.Execution.ExecutionID, facts, application.AppliedRevision)
	if err != nil {
		return ExecutionRecord{}, false, fmt.Errorf("insert fact snapshot: %w", err)
	}
	if insertPlans {
		if err := insertExecutionPlansTx(ctx, tx, admission.Plans); err != nil {
			return ExecutionRecord{}, false, err
		}
	}
	record := admission.Execution
	record.State, record.Revision, record.EffectiveFacts, record.Plans = ExecutionAccepted, 1, append([]byte(nil), facts...), append([]ExecutionPlanRecord(nil), admission.Plans...)
	return record, true, nil
}

func insertExecutionPlansTx(ctx context.Context, tx *sql.Tx, plans []ExecutionPlanRecord) error {
	for _, plan := range plans {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO effectus_execution_plans (execution_id, plan_id, saga_id, ordinal, state)
			VALUES ($1, $2, $3, $4, 'selected')
		`, plan.ExecutionID, plan.PlanID, plan.SagaID, plan.Ordinal)
		if err != nil {
			return fmt.Errorf("insert execution plan: %w", err)
		}
	}
	return nil
}

func getArtifactQuery(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, digest string) (ExecutionArtifact, error) {
	var artifact ExecutionArtifact
	err := queryer.QueryRowContext(ctx, `
		SELECT generation_digest, ir_digest, ir_bytes, environment, executor_manifest,
		       function_manifest, source_digest, compiler_metadata, created_at
		FROM effectus_execution_artifacts WHERE generation_digest = $1
	`, digest).Scan(&artifact.GenerationDigest, &artifact.IRDigest, &artifact.IRBytes, &artifact.Environment,
		&artifact.ExecutorManifest, &artifact.FunctionManifest, &artifact.SourceDigest, &artifact.CompilerMetadata, &artifact.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ExecutionArtifact{}, fmt.Errorf("%w: %s", ErrArtifactNotFound, digest)
	}
	return artifact, err
}

func getExecutionQuery(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, id string) (ExecutionRecord, error) {
	var record ExecutionRecord
	var owner, token, lastError sql.NullString
	var deadline sql.NullTime
	err := queryer.QueryRowContext(ctx, `
		SELECT execution_id, admission_identity, request_hash, ruleset, version, tenant_namespace,
		       merge_policy, generation_digest, effective_facts, state, revision,
		       recovery_owner, recovery_token, recovery_deadline, last_error, created_at, updated_at
		FROM effectus_executions WHERE execution_id = $1
	`, id).Scan(&record.ExecutionID, &record.AdmissionIdentity, &record.RequestHash, &record.Ruleset, &record.Version,
		&record.TenantNamespace, &record.MergePolicy, &record.GenerationDigest, &record.EffectiveFacts, &record.State,
		&record.Revision, &owner, &token, &deadline, &lastError, &record.CreatedAt, &record.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ExecutionRecord{}, fmt.Errorf("%w: %s", ErrExecutionNotFound, id)
	}
	if err != nil {
		return ExecutionRecord{}, err
	}
	record.RecoveryOwner, record.RecoveryToken, record.LastError = owner.String, token.String, lastError.String
	if deadline.Valid {
		record.RecoveryDeadline = deadline.Time
	}
	rows, err := queryer.QueryContext(ctx, `
		SELECT execution_id, plan_id, saga_id, ordinal, state
		FROM effectus_execution_plans WHERE execution_id = $1 ORDER BY ordinal
	`, id)
	if err != nil {
		return ExecutionRecord{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var plan ExecutionPlanRecord
		if err := rows.Scan(&plan.ExecutionID, &plan.PlanID, &plan.SagaID, &plan.Ordinal, &plan.State); err != nil {
			return ExecutionRecord{}, err
		}
		record.Plans = append(record.Plans, plan)
	}
	return record, rows.Err()
}

func createSagaTx(ctx context.Context, tx *sql.Tx, request CreateSagaRequest) (*SagaInstance, error) {
	if err := validateSagaRequest(request); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO effectus_saga_instances (saga_id, namespace, execution_id, plan_id, plan_digest, state, serial)
		VALUES ($1, $2, $3, $4, $5, 'running', $6) ON CONFLICT (saga_id) DO NOTHING
	`, request.SagaID, request.Namespace, request.ExecutionID, request.PlanID, request.PlanDigest, request.Serial)
	if err != nil {
		return nil, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	saga, err := getSagaTx(ctx, tx, request.SagaID, false)
	if err != nil {
		return nil, err
	}
	if rows == 0 && (saga.Namespace != request.Namespace || saga.ExecutionID != request.ExecutionID || saga.PlanID != request.PlanID || saga.PlanDigest != request.PlanDigest || saga.Serial != request.Serial) {
		return nil, fmt.Errorf("%w: saga %s", ErrIdentityConflict, request.SagaID)
	}
	return saga, nil
}

func enqueueStepTx(ctx context.Context, tx *sql.Tx, request EnqueueStepRequest) (*Dispatch, error) {
	request, arguments, argumentHash, compensationArguments, compensationHash, err := normalizeEnqueue(request)
	if err != nil {
		return nil, err
	}
	fencingJSON, err := json.Marshal(request.Fencing)
	if err != nil {
		return nil, err
	}
	saga, err := getSagaTx(ctx, tx, request.SagaID, true)
	if err != nil {
		return nil, err
	}
	existing, err := getDispatchByIdentityTx(ctx, tx, request.SagaID, request.EffectID, invocation.DirectionForward)
	if err == nil {
		var sequence int
		var verb, contractHash, storedArgumentHash string
		var compensationVerb, compensationContract, storedCompensationHash sql.NullString
		var storedFencing []byte
		if scanErr := tx.QueryRowContext(ctx, `
			SELECT sequence, verb, contract_hash, argument_hash, compensation_verb,
			       compensation_contract_hash, compensation_argument_hash, fencing_requirements
			FROM effectus_saga_steps WHERE saga_id = $1 AND effect_id = $2
		`, request.SagaID, request.EffectID).Scan(&sequence, &verb, &contractHash, &storedArgumentHash,
			&compensationVerb, &compensationContract, &storedCompensationHash, &storedFencing); scanErr != nil {
			return nil, scanErr
		}
		var existingFencing []FencingRequirement
		if decodeErr := json.Unmarshal(storedFencing, &existingFencing); decodeErr != nil {
			return nil, decodeErr
		}
		if sequence != request.Sequence || verb != request.Verb || contractHash != request.ContractHash || storedArgumentHash != argumentHash ||
			compensationVerb.String != request.CompensationVerb || compensationContract.String != request.CompensationContract || storedCompensationHash.String != compensationHash || !sameRequirements(existingFencing, request.Fencing) {
			return nil, fmt.Errorf("%w: saga %s effect %s", ErrIdentityConflict, request.SagaID, request.EffectID)
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if saga.State != SagaRunning {
		return nil, fmt.Errorf("%w: saga %s is %s", ErrInvalidTransition, saga.SagaID, saga.State)
	}
	var maximum int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(sequence), 0) FROM effectus_saga_steps WHERE saga_id = $1`, saga.SagaID).Scan(&maximum); err != nil {
		return nil, err
	}
	if request.Sequence != maximum+1 {
		return nil, fmt.Errorf("%w: non-dense sequence", ErrIdentityConflict)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO effectus_saga_steps
			(saga_id, effect_id, sequence, verb, contract_hash, arguments, argument_hash,
			 compensation_verb, compensation_contract_hash, compensation_arguments,
			 compensation_argument_hash, fencing_requirements, state)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10::jsonb,$11,$12::jsonb,'pending')
	`, request.SagaID, request.EffectID, request.Sequence, request.Verb, request.ContractHash, arguments, argumentHash,
		nullString(request.CompensationVerb), nullString(request.CompensationContract), nullJSON(compensationArguments), nullString(compensationHash), fencingJSON)
	if err != nil {
		return nil, err
	}
	key := IdempotencyKey(saga.Namespace, saga.SagaID, request.EffectID, invocation.DirectionForward)
	dispatchID := "dispatch/" + key
	_, err = tx.ExecContext(ctx, `
		INSERT INTO effectus_saga_outbox
			(dispatch_id,saga_id,effect_id,sequence,direction,verb,contract_hash,arguments,argument_hash,idempotency_key,state,fencing_requirements,serial_saga)
		VALUES ($1,$2,$3,$4,'forward',$5,$6,$7::jsonb,$8,$9,'queued',$10::jsonb,$11)
	`, dispatchID, saga.SagaID, request.EffectID, request.Sequence, request.Verb, request.ContractHash, arguments, argumentHash, key, fencingJSON, saga.Serial)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE effectus_saga_instances SET revision=revision+1, updated_at=now() WHERE saga_id=$1`, saga.SagaID); err != nil {
		return nil, err
	}
	return getDispatchTx(ctx, tx, dispatchID, false)
}

var _ AtomicAdmissionStore = (*PostgresOutboxStore)(nil)

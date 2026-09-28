-- +goose Up
ALTER TABLE effectus_saga_outbox
DROP CONSTRAINT effectus_saga_outbox_state_check;

ALTER TABLE effectus_saga_outbox
ADD CONSTRAINT effectus_saga_outbox_state_check CHECK (state IN (
    'queued', 'in_flight', 'succeeded', 'retry_wait', 'failed_permanent',
    'canceled', 'blocked_unknown', 'blocked_fence'
));

-- A prior terminal disposition can strand later selected work. Lock outbox
-- rows before the saga, as claim/completion do, then recheck after the saga
-- lock to catch concurrent enqueue or claim transactions.
-- +goose StatementBegin
DO $$
DECLARE
    orphan record;
    prior_state text;
    has_unknown boolean;
    has_effect boolean;
    next_state text;
BEGIN
    FOR orphan IN
        SELECT saga.saga_id
        FROM effectus_saga_instances saga
        JOIN effectus_execution_plans plan
            ON plan.saga_id = saga.saga_id AND plan.execution_id = saga.execution_id
        JOIN effectus_executions execution
            ON execution.execution_id = saga.execution_id
        WHERE saga.state IN ('running', 'compensating')
            AND execution.state IN (
                'failed', 'blocked_unknown', 'blocked_fence',
                'blocked_dependency', 'blocked_compensation'
            )
        ORDER BY saga.saga_id
    LOOP
        PERFORM dispatch_id FROM effectus_saga_outbox
            WHERE saga_id = orphan.saga_id ORDER BY dispatch_id FOR UPDATE;
        PERFORM saga_id FROM effectus_saga_instances
            WHERE saga_id = orphan.saga_id FOR UPDATE;
        PERFORM dispatch_id FROM effectus_saga_outbox
            WHERE saga_id = orphan.saga_id ORDER BY dispatch_id FOR UPDATE;

        SELECT saga.state INTO prior_state
        FROM effectus_saga_instances saga
        WHERE saga.saga_id = orphan.saga_id;
        IF prior_state NOT IN ('running', 'compensating') OR NOT EXISTS (
            SELECT 1 FROM effectus_saga_instances saga
            JOIN effectus_execution_plans plan
                ON plan.saga_id = saga.saga_id AND plan.execution_id = saga.execution_id
            JOIN effectus_executions execution
                ON execution.execution_id = saga.execution_id
            WHERE saga.saga_id = orphan.saga_id
                AND execution.state IN (
                    'failed', 'blocked_unknown', 'blocked_fence',
                    'blocked_dependency', 'blocked_compensation'
                )
        ) THEN
            CONTINUE;
        END IF;

        SELECT EXISTS (
            SELECT 1 FROM effectus_saga_outbox outbox
            WHERE outbox.saga_id = orphan.saga_id
                AND (
                    outbox.state IN ('in_flight', 'blocked_unknown')
                    OR (outbox.state = 'retry_wait' AND outbox.last_outcome = 'unknown_outcome')
                )
        ) OR EXISTS (
            SELECT 1 FROM effectus_saga_outbox outbox
            JOIN effectus_saga_attempts attempt ON attempt.dispatch_id = outbox.dispatch_id
            WHERE outbox.saga_id = orphan.saga_id
                AND (attempt.outcome = 'unknown_outcome' OR attempt.completed_at IS NULL)
        ) INTO has_unknown;
        SELECT EXISTS (
            SELECT 1 FROM effectus_saga_outbox outbox
            WHERE outbox.saga_id = orphan.saga_id
                AND (outbox.direction = 'compensation'
                    OR (outbox.direction = 'forward' AND outbox.state = 'succeeded'))
        ) OR EXISTS (
            SELECT 1 FROM effectus_saga_steps step
            WHERE step.saga_id = orphan.saga_id
                AND step.state IN ('succeeded', 'compensated')
        ) INTO has_effect;

        IF has_unknown THEN
            next_state := 'blocked_unknown';
        ELSIF has_effect OR prior_state = 'compensating' THEN
            next_state := 'blocked_dependency';
        ELSE
            next_state := 'failed';
        END IF;

        UPDATE effectus_saga_outbox
        SET state = 'blocked_unknown', lease_owner = NULL, lease_token = NULL,
            lease_deadline = NULL, next_attempt_at = NULL,
            last_error = 'execution stopped with unresolved dispatch outcome',
            revision = revision + 1, updated_at = now()
        WHERE saga_id = orphan.saga_id AND state = 'in_flight';

        UPDATE effectus_saga_outbox
        SET state = 'blocked_unknown', next_attempt_at = NULL,
            revision = revision + 1, updated_at = now()
        WHERE saga_id = orphan.saga_id AND state = 'retry_wait'
            AND (last_outcome = 'unknown_outcome' OR dispatch_id IN (
                SELECT dispatch_id FROM effectus_saga_attempts
                WHERE outcome = 'unknown_outcome' OR completed_at IS NULL
            ));

        UPDATE effectus_saga_outbox
        SET state = 'canceled', next_attempt_at = NULL,
            last_error = 'execution stopped before dispatch could complete',
            revision = revision + 1, updated_at = now()
        WHERE saga_id = orphan.saga_id AND state IN ('queued', 'retry_wait');

        UPDATE effectus_saga_instances
        SET state = next_state, revision = revision + 1, updated_at = now()
        WHERE saga_id = orphan.saga_id AND state IN ('running', 'compensating');
    END LOOP;
END
$$;
-- +goose StatementEnd

-- A terminal saga with an unresolved attempt must keep that diagnosis even if
-- an older binary recorded the execution as failed.
UPDATE effectus_saga_outbox outbox
SET
    state = 'blocked_unknown', lease_owner = NULL, lease_token = NULL,
    lease_deadline = NULL, next_attempt_at = NULL,
    last_error = CASE
        WHEN outbox.state = 'in_flight'
            THEN 'execution stopped with unresolved dispatch outcome'
        ELSE outbox.last_error
    END,
    revision = outbox.revision + 1, updated_at = now()
FROM effectus_saga_instances AS saga
INNER JOIN effectus_execution_plans AS plan ON saga.saga_id = plan.saga_id
INNER JOIN
    effectus_executions AS execution
    ON plan.execution_id = execution.execution_id
WHERE
    outbox.saga_id = saga.saga_id
    AND saga.execution_id = execution.execution_id
    AND saga.state IN (
        'completed', 'compensated', 'failed', 'blocked_unknown',
        'blocked_dependency', 'blocked_fence', 'blocked_compensation'
    )
    AND execution.state IN (
        'failed', 'blocked_unknown', 'blocked_fence',
        'blocked_dependency', 'blocked_compensation'
    )
    AND (outbox.state = 'in_flight' OR (
        outbox.state = 'retry_wait' AND (
            outbox.last_outcome = 'unknown_outcome'
            OR outbox.dispatch_id IN (
                SELECT dispatch_id FROM effectus_saga_attempts
                WHERE outcome = 'unknown_outcome' OR completed_at IS NULL
            )
        )
    ));

UPDATE effectus_saga_instances saga
SET state = 'blocked_unknown', revision = saga.revision + 1, updated_at = now()
FROM effectus_execution_plans AS plan
INNER JOIN effectus_executions AS execution
    ON plan.execution_id = execution.execution_id
WHERE
    saga.saga_id = plan.saga_id
    AND saga.execution_id = execution.execution_id
    AND saga.state IN (
        'completed', 'compensated', 'failed', 'blocked_dependency',
        'blocked_fence', 'blocked_compensation'
    )
    AND execution.state IN (
        'failed', 'blocked_unknown', 'blocked_fence',
        'blocked_dependency', 'blocked_compensation'
    )
    AND EXISTS (
        SELECT 1 FROM effectus_saga_outbox AS outbox
        WHERE outbox.saga_id = saga.saga_id AND outbox.state = 'blocked_unknown'
    );

UPDATE effectus_executions execution
SET
    state = 'blocked_unknown',
    revision = execution.revision + 1,
    updated_at = now()
WHERE execution.state IN (
    'failed', 'blocked_fence', 'blocked_dependency', 'blocked_compensation'
)
AND EXISTS (
    SELECT 1 FROM effectus_execution_plans AS plan
    INNER JOIN effectus_saga_instances AS saga ON plan.saga_id = saga.saga_id
    WHERE
        plan.execution_id = execution.execution_id
        AND saga.state = 'blocked_unknown'
);

-- Terminal sagas can also retain safe queued or known-not-committed retry rows.
UPDATE effectus_saga_outbox outbox
SET
    state = 'canceled', next_attempt_at = NULL,
    last_error = 'execution stopped before dispatch could complete',
    revision = outbox.revision + 1, updated_at = now()
FROM effectus_saga_instances AS saga
INNER JOIN effectus_execution_plans AS plan ON saga.saga_id = plan.saga_id
INNER JOIN
    effectus_executions AS execution
    ON plan.execution_id = execution.execution_id
WHERE
    outbox.saga_id = saga.saga_id
    AND saga.execution_id = execution.execution_id
    AND saga.state IN (
        'completed', 'compensated', 'failed', 'blocked_unknown',
        'blocked_dependency', 'blocked_fence', 'blocked_compensation'
    )
    AND execution.state IN (
        'failed', 'blocked_unknown', 'blocked_fence',
        'blocked_dependency', 'blocked_compensation'
    )
    AND outbox.state IN ('queued', 'retry_wait');

-- +goose Down
-- A canceled dispatch cannot be faithfully represented by the old states.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM effectus_saga_outbox WHERE state = 'canceled') THEN
        RAISE EXCEPTION 'cannot downgrade while canceled saga dispatches exist';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE effectus_saga_outbox
DROP CONSTRAINT effectus_saga_outbox_state_check;

ALTER TABLE effectus_saga_outbox
ADD CONSTRAINT effectus_saga_outbox_state_check CHECK (state IN (
    'queued', 'in_flight', 'succeeded', 'retry_wait', 'failed_permanent',
    'blocked_unknown', 'blocked_fence'
));

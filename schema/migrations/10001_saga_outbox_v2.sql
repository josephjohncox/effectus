-- +goose Up
CREATE TABLE effectus_saga_instances (
    saga_id text PRIMARY KEY,
    namespace text NOT NULL,
    execution_id text NOT NULL,
    plan_id text NOT NULL,
    plan_digest text NOT NULL,
    state text NOT NULL,
    serial boolean NOT NULL DEFAULT true,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT effectus_saga_instances_state_check CHECK (state IN (
        'running', 'completed', 'compensating', 'compensated', 'failed',
        'blocked_unknown', 'blocked_dependency', 'blocked_fence', 'blocked_compensation'
    ))
);

CREATE TABLE effectus_saga_steps (
    saga_id text NOT NULL REFERENCES effectus_saga_instances(saga_id) ON DELETE RESTRICT,
    effect_id text NOT NULL,
    sequence integer NOT NULL CHECK (sequence > 0),
    verb text NOT NULL,
    contract_hash text NOT NULL,
    arguments jsonb NOT NULL,
    argument_hash text NOT NULL,
    compensation_verb text,
    compensation_contract_hash text,
    compensation_arguments jsonb,
    compensation_argument_hash text,
    fencing_requirements jsonb NOT NULL DEFAULT '[]'::jsonb,
    state text NOT NULL DEFAULT 'pending',
    result jsonb,
    PRIMARY KEY (saga_id, effect_id),
    UNIQUE (saga_id, sequence),
    CONSTRAINT effectus_saga_steps_state_check CHECK (state IN ('pending', 'succeeded', 'failed', 'compensated')),
    CONSTRAINT effectus_saga_steps_compensation_check CHECK (
        (compensation_verb IS NULL AND compensation_contract_hash IS NULL AND compensation_arguments IS NULL AND compensation_argument_hash IS NULL)
        OR
        (compensation_verb IS NOT NULL AND compensation_contract_hash IS NOT NULL AND compensation_arguments IS NOT NULL AND compensation_argument_hash IS NOT NULL)
    )
);

CREATE TABLE effectus_saga_outbox (
    dispatch_id text PRIMARY KEY,
    saga_id text NOT NULL REFERENCES effectus_saga_instances(saga_id) ON DELETE RESTRICT,
    effect_id text NOT NULL,
    sequence integer NOT NULL CHECK (sequence > 0),
    direction text NOT NULL,
    verb text NOT NULL,
    contract_hash text NOT NULL,
    arguments jsonb NOT NULL,
    argument_hash text NOT NULL,
    idempotency_key text NOT NULL UNIQUE,
    state text NOT NULL,
    attempt bigint NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    lease_owner text,
    lease_token text,
    lease_deadline timestamptz,
    next_attempt_at timestamptz,
    fencing_requirements jsonb NOT NULL DEFAULT '[]'::jsonb,
    fencing_grants jsonb NOT NULL DEFAULT '[]'::jsonb,
    last_outcome text,
    last_error text,
    result jsonb,
    serial_saga boolean NOT NULL DEFAULT true,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (saga_id, effect_id, direction),
    FOREIGN KEY (saga_id, effect_id) REFERENCES effectus_saga_steps(saga_id, effect_id) ON DELETE RESTRICT,
    CONSTRAINT effectus_saga_outbox_direction_check CHECK (direction IN ('forward', 'compensation')),
    CONSTRAINT effectus_saga_outbox_state_check CHECK (state IN (
        'queued', 'in_flight', 'succeeded', 'retry_wait', 'failed_permanent', 'blocked_unknown', 'blocked_fence'
    )),
    CONSTRAINT effectus_saga_outbox_lease_check CHECK (
        (state = 'in_flight' AND lease_owner IS NOT NULL AND lease_token IS NOT NULL AND lease_deadline IS NOT NULL)
        OR state <> 'in_flight'
    )
);

CREATE INDEX effectus_saga_outbox_claim_idx
    ON effectus_saga_outbox (next_attempt_at, created_at)
    WHERE state IN ('queued', 'retry_wait', 'in_flight');

CREATE UNIQUE INDEX effectus_saga_outbox_serial_inflight_idx
    ON effectus_saga_outbox (saga_id)
    WHERE state = 'in_flight' AND serial_saga;

CREATE TABLE effectus_saga_attempts (
    dispatch_id text NOT NULL REFERENCES effectus_saga_outbox(dispatch_id) ON DELETE RESTRICT,
    attempt bigint NOT NULL CHECK (attempt > 0),
    lease_owner text NOT NULL,
    lease_token text NOT NULL,
    lease_deadline timestamptz NOT NULL,
    fencing_grants jsonb NOT NULL DEFAULT '[]'::jsonb,
    outcome text,
    error text,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    PRIMARY KEY (dispatch_id, attempt)
);

CREATE TABLE effectus_fencing_counters (
    authority text NOT NULL,
    resource text NOT NULL,
    token bigint NOT NULL CHECK (token > 0),
    PRIMARY KEY (authority, resource)
);

CREATE TABLE effectus_fencing_leases (
    authority text NOT NULL,
    resource text NOT NULL,
    holder text NOT NULL,
    token bigint NOT NULL CHECK (token > 0),
    expires_at timestamptz NOT NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    PRIMARY KEY (authority, resource),
    FOREIGN KEY (authority, resource) REFERENCES effectus_fencing_counters(authority, resource) ON DELETE RESTRICT
);

-- +goose Down
DROP TABLE IF EXISTS effectus_fencing_leases;
DROP TABLE IF EXISTS effectus_fencing_counters;
DROP TABLE IF EXISTS effectus_saga_attempts;
DROP TABLE IF EXISTS effectus_saga_outbox;
DROP TABLE IF EXISTS effectus_saga_steps;
DROP TABLE IF EXISTS effectus_saga_instances;

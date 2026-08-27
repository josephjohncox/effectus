-- +goose Up
CREATE TABLE effectus_execution_artifacts (
    generation_digest text PRIMARY KEY,
    ir_digest text NOT NULL,
    ir_bytes bytea NOT NULL,
    environment jsonb NOT NULL,
    executor_manifest jsonb NOT NULL,
    function_manifest jsonb NOT NULL,
    source_digest text NOT NULL,
    compiler_metadata jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (octet_length(ir_bytes) > 0)
);

CREATE TABLE effectus_rule_generations (
    ruleset text NOT NULL,
    version text NOT NULL, -- noqa: RF04
    environment text NOT NULL,
    generation_digest text NOT NULL REFERENCES effectus_execution_artifacts (
        generation_digest
    ) ON DELETE RESTRICT,
    state text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    retired_at timestamptz,
    PRIMARY KEY (ruleset, version, generation_digest),
    CHECK (state IN ('active', 'retired'))
);

CREATE UNIQUE INDEX effectus_rule_generations_one_active
ON effectus_rule_generations (ruleset, environment)
WHERE state = 'active';

CREATE TABLE effectus_executions (
    execution_id text PRIMARY KEY,
    admission_identity text NOT NULL UNIQUE,
    request_hash text NOT NULL,
    ruleset text NOT NULL,
    version text NOT NULL, -- noqa: RF04
    tenant_namespace text NOT NULL,
    merge_policy text NOT NULL,
    generation_digest text NOT NULL REFERENCES effectus_execution_artifacts (
        generation_digest
    ) ON DELETE RESTRICT,
    effective_facts jsonb NOT NULL,
    state text NOT NULL,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    recovery_owner text,
    recovery_token text,
    recovery_deadline timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (state IN (
        'admitting', 'accepted', 'running', 'completed', 'failed',
        'blocked_unknown',
        'blocked_fence',
        'blocked_dependency',
        'blocked_compensation'
    )),
    CHECK (
        (
            recovery_owner IS NULL
            AND recovery_token IS NULL
            AND recovery_deadline IS NULL
        )
        OR
        (
            recovery_owner IS NOT NULL
            AND recovery_token IS NOT NULL
            AND recovery_deadline IS NOT NULL
        )
    )
);

CREATE INDEX effectus_executions_recovery
ON effectus_executions (state, recovery_deadline, updated_at)
WHERE state NOT IN ('completed', 'failed');

CREATE TABLE effectus_execution_plans (
    execution_id text NOT NULL REFERENCES effectus_executions (
        execution_id
    ) ON DELETE RESTRICT,
    plan_id text NOT NULL,
    saga_id text NOT NULL REFERENCES effectus_saga_instances (
        saga_id
    ) ON DELETE RESTRICT,
    ordinal integer NOT NULL CHECK (ordinal >= 0),
    state text NOT NULL DEFAULT 'selected',
    PRIMARY KEY (execution_id, plan_id),
    UNIQUE (execution_id, ordinal),
    UNIQUE (saga_id),
    CHECK (state IN ('selected', 'running', 'completed', 'blocked', 'failed'))
);

CREATE TABLE effectus_fact_applications (
    execution_id text NOT NULL REFERENCES effectus_executions (
        execution_id
    ) ON DELETE RESTRICT,
    fact_event_id text NOT NULL,
    merge_policy text NOT NULL,
    facts jsonb NOT NULL,
    applied_revision bigint NOT NULL CHECK (applied_revision > 0),
    applied_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (execution_id, fact_event_id)
);

CREATE TABLE effectus_fact_snapshots (
    execution_id text PRIMARY KEY REFERENCES effectus_executions (
        execution_id
    ) ON DELETE RESTRICT,
    universe jsonb NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS effectus_fact_snapshots;
DROP TABLE IF EXISTS effectus_fact_applications;
DROP TABLE IF EXISTS effectus_execution_plans;
DROP TABLE IF EXISTS effectus_executions;
DROP INDEX IF EXISTS effectus_rule_generations_one_active;
DROP TABLE IF EXISTS effectus_rule_generations;
DROP TABLE IF EXISTS effectus_execution_artifacts;

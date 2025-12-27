-- Deployment queries for sqlc code generation

-- name: CreateDeployment :one
INSERT INTO deployments (
    ruleset_id, environment, status, strategy,
    config, health_check, rollback_info, canary_config,
    deployed_by, deployment_duration_ms
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;

-- name: GetDeployment :one
SELECT * FROM deployments 
WHERE ruleset_id = $1 AND environment = $2;

-- name: GetDeploymentByID :one
SELECT * FROM deployments 
WHERE id = $1;

-- name: GetActiveDeployment :one
SELECT d.*, r.name as ruleset_name, r.version as ruleset_version
FROM deployments d
JOIN rulesets r ON d.ruleset_id = r.id
WHERE d.environment = $1 AND d.status = 'active'
ORDER BY d.deployed_at DESC
LIMIT 1;

-- name: ListDeployments :many
SELECT d.*, r.name as ruleset_name, r.version as ruleset_version
FROM deployments d
JOIN rulesets r ON d.ruleset_id = r.id
WHERE 
    ($1::text IS NULL OR d.environment = $1)
    AND ($2::deployment_status[] IS NULL OR d.status = ANY($2::deployment_status[]))
    AND ($3::text IS NULL OR d.deployed_by = $3)
    AND ($4::timestamptz IS NULL OR d.deployed_at >= $4)
    AND ($5::timestamptz IS NULL OR d.deployed_at <= $5)
ORDER BY d.deployed_at DESC
LIMIT $6;

-- name: UpdateDeploymentStatus :one
UPDATE deployments 
SET 
    status = $2,
    completed_at = CASE WHEN $2 IN ('active', 'failed', 'inactive') THEN CURRENT_TIMESTAMP ELSE completed_at END,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: UpdateDeploymentHealthCheck :one
UPDATE deployments 
SET 
    health_check = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetDeploymentRollback :one
UPDATE deployments 
SET 
    status = 'rolling_back',
    rollback_info = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: GetDeploymentHistory :many
SELECT d.*, r.name as ruleset_name, r.version as ruleset_version
FROM deployments d
JOIN rulesets r ON d.ruleset_id = r.id
WHERE d.environment = $1
ORDER BY d.deployed_at DESC
LIMIT $2;

-- name: GetDeploymentsByRuleset :many
SELECT d.*, r.name as ruleset_name, r.version as ruleset_version
FROM deployments d
JOIN rulesets r ON d.ruleset_id = r.id
WHERE r.name = $1
ORDER BY d.deployed_at DESC
LIMIT $2;

-- name: GetFailedDeployments :many
SELECT d.*, r.name as ruleset_name, r.version as ruleset_version
FROM deployments d
JOIN rulesets r ON d.ruleset_id = r.id
WHERE d.status = 'failed' AND d.deployed_at >= $1
ORDER BY d.deployed_at DESC
LIMIT $2;

-- name: GetDeploymentStats :one
SELECT 
    COUNT(*) as total_deployments,
    COUNT(*) FILTER (WHERE status = 'active') as active_deployments,
    COUNT(*) FILTER (WHERE status = 'failed') as failed_deployments,
    COUNT(*) FILTER (WHERE status = 'canary') as canary_deployments,
    AVG(deployment_duration_ms) FILTER (WHERE deployment_duration_ms IS NOT NULL) as avg_deployment_duration_ms,
    COUNT(DISTINCT environment) as total_environments
FROM deployments
WHERE deployed_at >= $1;

-- name: DeactivateOldDeployments :exec
UPDATE deployments 
SET 
    status = 'inactive',
    updated_at = CURRENT_TIMESTAMP
WHERE environment = $1 AND status = 'active' AND id != $2;

-- name: GetCanaryDeployments :many
SELECT d.*, r.name as ruleset_name, r.version as ruleset_version
FROM deployments d
JOIN rulesets r ON d.ruleset_id = r.id
WHERE d.status = 'canary'
ORDER BY d.deployed_at DESC;

-- name: CompleteDeployment :one
UPDATE deployments 
SET 
    status = 'active',
    completed_at = CURRENT_TIMESTAMP,
    deployment_duration_ms = EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - deployed_at))::INTEGER * 1000,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: GetEnvironmentDeployments :many
SELECT DISTINCT ON (r.name) 
    d.*, r.name as ruleset_name, r.version as ruleset_version
FROM deployments d
JOIN rulesets r ON d.ruleset_id = r.id
WHERE d.environment = $1 AND d.status = 'active'
ORDER BY r.name, d.deployed_at DESC; 
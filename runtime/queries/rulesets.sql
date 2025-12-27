-- Ruleset queries for sqlc code generation

-- name: CreateRuleset :one
INSERT INTO rulesets (
    name, version, environment, status, ruleset_data, rule_count,
    description, tags, owner, team, metadata,
    git_commit, git_branch, git_tag, git_author, pull_request,
    compiled_at, compiler_version, schema_version, validation_hash,
    created_by, updated_by
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14, $15, $16,
    $17, $18, $19, $20,
    $21, $22
) RETURNING *;

-- name: GetRuleset :one
SELECT * FROM rulesets 
WHERE name = $1 AND version = $2 AND environment = $3;

-- name: GetRulesetByID :one
SELECT * FROM rulesets 
WHERE id = $1;

-- name: GetLatestRuleset :one
SELECT * FROM rulesets 
WHERE name = $1 AND environment = $2
ORDER BY created_at DESC 
LIMIT 1;

-- name: ListRulesets :many
SELECT r.*, 
       COALESCE(d.status, 'inactive') as deployment_status,
       d.deployed_at as last_deployed_at
FROM rulesets r
LEFT JOIN deployments d ON r.id = d.ruleset_id AND d.environment = r.environment
WHERE 
    ($1::text IS NULL OR r.name = ANY($1::text[]))
    AND ($2::text[] IS NULL OR r.environment = ANY($2::text[]))
    AND ($3::ruleset_status[] IS NULL OR r.status = ANY($3::ruleset_status[]))
    AND ($4::text IS NULL OR r.owner = $4)
    AND ($5::text IS NULL OR r.team = $5)
    AND ($6::text[] IS NULL OR r.tags && $6::text[])
    AND ($7::timestamptz IS NULL OR r.created_at >= $7)
    AND ($8::text IS NULL OR r.created_by = $8)
    AND ($9::text IS NULL OR r.git_commit = $9)
ORDER BY r.created_at DESC
LIMIT $10;

-- name: UpdateRuleset :one
UPDATE rulesets 
SET 
    status = COALESCE($2, status),
    ruleset_data = COALESCE($3, ruleset_data),
    rule_count = COALESCE($4, rule_count),
    description = COALESCE($5, description),
    tags = COALESCE($6, tags),
    metadata = COALESCE($7, metadata),
    updated_by = $8,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: UpdateRulesetStatus :one
UPDATE rulesets 
SET 
    status = $2,
    updated_by = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: DeleteRuleset :exec
DELETE FROM rulesets 
WHERE id = $1;

-- name: GetRulesetVersions :many
SELECT r.version, r.created_at, r.created_by, r.git_commit, r.status,
       EXISTS(SELECT 1 FROM deployments d WHERE d.ruleset_id = r.id AND d.status = 'active') as is_deployed
FROM rulesets r
WHERE r.name = $1 AND r.environment = $2
ORDER BY r.created_at DESC;

-- name: SearchRulesets :many
SELECT r.*, 
       ts_rank(to_tsvector('english', r.name || ' ' || COALESCE(r.description, '')), plainto_tsquery($1)) as rank
FROM rulesets r
WHERE to_tsvector('english', r.name || ' ' || COALESCE(r.description, '')) @@ plainto_tsquery($1)
ORDER BY rank DESC, r.created_at DESC
LIMIT $2;

-- name: GetRulesetsByGitCommit :many
SELECT * FROM rulesets 
WHERE git_commit = $1
ORDER BY created_at DESC;

-- name: GetRulesetStats :one
SELECT 
    COUNT(*) as total_rulesets,
    COUNT(*) FILTER (WHERE status = 'deployed') as deployed_rulesets,
    COUNT(*) FILTER (WHERE status = 'failed') as failed_rulesets,
    COUNT(DISTINCT name) as unique_rulesets,
    COUNT(DISTINCT environment) as total_environments
FROM rulesets;

-- name: GetRulesetsByTeam :many
SELECT * FROM rulesets 
WHERE team = $1 AND environment = $2
ORDER BY created_at DESC
LIMIT $3;

-- name: GetRulesetsByTags :many
SELECT * FROM rulesets 
WHERE tags && $1::text[] AND environment = $2
ORDER BY created_at DESC
LIMIT $3;

-- name: UpsertRuleset :one
INSERT INTO rulesets (
    name, version, environment, status, ruleset_data, rule_count,
    description, tags, owner, team, metadata,
    git_commit, git_branch, git_tag, git_author, pull_request,
    compiled_at, compiler_version, schema_version, validation_hash,
    created_by, updated_by
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14, $15, $16,
    $17, $18, $19, $20,
    $21, $22
)
ON CONFLICT (name, version, environment) 
DO UPDATE SET
    status = EXCLUDED.status,
    ruleset_data = EXCLUDED.ruleset_data,
    rule_count = EXCLUDED.rule_count,
    description = EXCLUDED.description,
    tags = EXCLUDED.tags,
    metadata = EXCLUDED.metadata,
    git_commit = EXCLUDED.git_commit,
    git_branch = EXCLUDED.git_branch,
    git_tag = EXCLUDED.git_tag,
    git_author = EXCLUDED.git_author,
    pull_request = EXCLUDED.pull_request,
    compiled_at = EXCLUDED.compiled_at,
    compiler_version = EXCLUDED.compiler_version,
    schema_version = EXCLUDED.schema_version,
    validation_hash = EXCLUDED.validation_hash,
    updated_by = EXCLUDED.updated_by,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: CountRulesetsByStatus :many
SELECT status, COUNT(*) as count
FROM rulesets 
WHERE environment = $1
GROUP BY status;

-- name: GetRecentRulesets :many
SELECT * FROM rulesets 
WHERE created_at >= $1
ORDER BY created_at DESC
LIMIT $2; 
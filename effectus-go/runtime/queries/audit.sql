-- Audit log queries for sqlc code generation

-- name: CreateAuditEntry :one
INSERT INTO audit_log (
    action, resource, resource_id, version, environment,
    user_id, user_email, ip_address, user_agent, session_id,
    details, request_id, trace_id,
    result, error_message, duration_ms
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10,
    $11, $12, $13,
    $14, $15, $16
) RETURNING *;

-- name: GetAuditLogs :many
SELECT * FROM audit_log
WHERE 
    ($1::text[] IS NULL OR action = ANY($1::text[]))
    AND ($2::text[] IS NULL OR resource = ANY($2::text[]))
    AND ($3::text[] IS NULL OR user_id = ANY($3::text[]))
    AND ($4::timestamptz IS NULL OR timestamp >= $4)
    AND ($5::timestamptz IS NULL OR timestamp <= $5)
    AND ($6::text IS NULL OR result = $6)
    AND ($7::text IS NULL OR environment = $7)
ORDER BY timestamp DESC
LIMIT $8 OFFSET $9;

-- name: GetAuditLogsByResource :many
SELECT * FROM audit_log
WHERE resource = $1 AND ($2::text IS NULL OR version = $2)
ORDER BY timestamp DESC
LIMIT $3;

-- name: GetAuditLogsByUser :many
SELECT * FROM audit_log
WHERE user_id = $1 AND timestamp >= $2
ORDER BY timestamp DESC
LIMIT $3;

-- name: GetFailedOperations :many
SELECT * FROM audit_log
WHERE result = 'failure' AND timestamp >= $1
ORDER BY timestamp DESC
LIMIT $2;

-- name: GetAuditStats :one
SELECT 
    COUNT(*) as total_entries,
    COUNT(*) FILTER (WHERE result = 'success') as successful_operations,
    COUNT(*) FILTER (WHERE result = 'failure') as failed_operations,
    COUNT(DISTINCT user_id) as unique_users,
    COUNT(DISTINCT action) as unique_actions,
    AVG(duration_ms) FILTER (WHERE duration_ms IS NOT NULL) as avg_duration_ms
FROM audit_log
WHERE timestamp >= $1;

-- name: GetTopActions :many
SELECT action, COUNT(*) as count
FROM audit_log
WHERE timestamp >= $1
GROUP BY action
ORDER BY count DESC
LIMIT $2;

-- name: GetTopUsers :many
SELECT user_id, user_email, COUNT(*) as action_count
FROM audit_log
WHERE timestamp >= $1
GROUP BY user_id, user_email
ORDER BY action_count DESC
LIMIT $2;

-- name: GetSecurityEvents :many
SELECT * FROM audit_log
WHERE 
    (action LIKE '%login%' OR action LIKE '%auth%' OR action LIKE '%security%')
    AND timestamp >= $1
ORDER BY timestamp DESC
LIMIT $2;

-- name: SearchAuditLogs :many
SELECT * FROM audit_log
WHERE 
    (to_tsvector('english', action || ' ' || resource || ' ' || COALESCE(user_email, '')) @@ plainto_tsquery($1))
    AND timestamp >= $2
ORDER BY timestamp DESC
LIMIT $3;

-- name: CleanupOldAuditLogs :exec
DELETE FROM audit_log
WHERE timestamp < $1;

-- name: GetAuditLogsBySession :many
SELECT * FROM audit_log
WHERE session_id = $1
ORDER BY timestamp ASC;

-- name: GetAuditLogsByTrace :many
SELECT * FROM audit_log
WHERE trace_id = $1
ORDER BY timestamp ASC;

-- name: GetRecentErrorsCount :one
SELECT COUNT(*) as error_count
FROM audit_log
WHERE result = 'failure' AND timestamp >= $1; 
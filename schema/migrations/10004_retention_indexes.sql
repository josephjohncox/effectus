-- +goose NO TRANSACTION

-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS effectus_executions_terminal_retention
ON effectus_executions (updated_at, execution_id)
WHERE state IN ('completed', 'failed');

CREATE INDEX CONCURRENTLY IF NOT EXISTS
effectus_saga_instances_terminal_retention
ON effectus_saga_instances (updated_at, saga_id)
WHERE state IN ('completed', 'compensated', 'failed');

CREATE INDEX CONCURRENTLY IF NOT EXISTS
effectus_kafka_deliveries_poison_retention
ON effectus_kafka_deliveries (updated_at, delivery_id)
WHERE poison_acknowledged;

CREATE INDEX CONCURRENTLY IF NOT EXISTS effectus_rule_generations_retention
ON effectus_rule_generations (retired_at, generation_digest)
WHERE state = 'retired';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS effectus_rule_generations_retention;
DROP INDEX CONCURRENTLY IF EXISTS effectus_kafka_deliveries_poison_retention;
DROP INDEX CONCURRENTLY IF EXISTS effectus_saga_instances_terminal_retention;
DROP INDEX CONCURRENTLY IF EXISTS effectus_executions_terminal_retention;

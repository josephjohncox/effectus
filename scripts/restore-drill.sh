#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"

COMPOSE_FILE=${COMPOSE_FILE:-tests/fixtures/durable-stack/docker-compose.yml}
SOURCE_DATABASE=${SOURCE_DATABASE:-effectus_saga}
RESTORE_DATABASE=${RESTORE_DATABASE:-effectus_restore_drill}
OUTPUT_DIR=${RESTORE_DRILL_OUTPUT:-out/restore-drill}
SOURCE_DSN=${EFFECTUS_POSTGRES_DSN:-postgres://effectus:effectus@localhost:55433/effectus_saga?sslmode=disable}
RESTORE_DSN=${RESTORE_DSN:-postgres://effectus:effectus@localhost:55433/${RESTORE_DATABASE}?sslmode=disable}
mkdir -p "$OUTPUT_DIR"

started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
started_epoch=$(date +%s)
EFFECTUS_POSTGRES_DSN="$SOURCE_DSN" go run ./cmd/effectusd --database-migrations=apply

docker compose -f "$COMPOSE_FILE" exec -T postgres \
  pg_dump -U effectus -d "$SOURCE_DATABASE" -Fc > "$OUTPUT_DIR/postgres.dump"
backup_epoch=$(date +%s)

docker compose -f "$COMPOSE_FILE" exec -T postgres \
  dropdb -U effectus --if-exists "$RESTORE_DATABASE"
docker compose -f "$COMPOSE_FILE" exec -T postgres \
  createdb -U effectus "$RESTORE_DATABASE"
docker compose -f "$COMPOSE_FILE" exec -T postgres \
  pg_restore -U effectus -d "$RESTORE_DATABASE" --clean --if-exists < "$OUTPUT_DIR/postgres.dump"

EFFECTUS_POSTGRES_DSN="$RESTORE_DSN" go run ./cmd/effectusd --database-migrations=validate-only

integrity_sql="
SELECT CASE WHEN EXISTS (
  SELECT 1 FROM effectus_execution_plans p
  LEFT JOIN effectus_executions e ON e.execution_id=p.execution_id
  WHERE e.execution_id IS NULL
) THEN 1 ELSE 0 END;
"
orphan_count=$(docker compose -f "$COMPOSE_FILE" exec -T postgres \
  psql -U effectus -d "$RESTORE_DATABASE" -Atc "$integrity_sql")
test "$orphan_count" = "0"

blocked_sql="SELECT count(*) FROM effectus_executions WHERE state LIKE 'blocked_%';"
source_blocked=$(docker compose -f "$COMPOSE_FILE" exec -T postgres \
  psql -U effectus -d "$SOURCE_DATABASE" -Atc "$blocked_sql")
restored_blocked=$(docker compose -f "$COMPOSE_FILE" exec -T postgres \
  psql -U effectus -d "$RESTORE_DATABASE" -Atc "$blocked_sql")
test "$source_blocked" = "$restored_blocked"

# Preserve the repository-owned immutable configuration and trust-policy
# examples. Operators can add Secret exports, certificates, verifier policy,
# and projection PVC exports through RESTORE_INPUTS.
restore_inputs=${RESTORE_INPUTS:-"charts/effectusd/values.yaml charts/effectusd/README.md docs/PRODUCTION_RUNBOOK.md"}
# shellcheck disable=SC2086
tar -czf "$OUTPUT_DIR/non-postgres-dependencies.tar.gz" $restore_inputs
if [ -n "${FACTS_PROJECTION_PATH:-}" ] && [ -e "$FACTS_PROJECTION_PATH" ]; then
  cp -a "$FACTS_PROJECTION_PATH" "$OUTPUT_DIR/facts-projection"
fi
sha256sum "$OUTPUT_DIR/postgres.dump" "$OUTPUT_DIR/non-postgres-dependencies.tar.gz" > "$OUTPUT_DIR/SHA256SUMS"

kafka_result="not configured"
if [ -n "${KAFKA_BROKERS:-}" ]; then
  KAFKA_BROKERS="$KAFKA_BROKERS" go test -count=1 -tags=integration ./internal/adapters/kafka \
    -run '^TestKafkaConsumerGroupCommitAndRestart$'
  kafka_result="commit/restart reconciliation passed"
fi

finished_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
finished_epoch=$(date +%s)
rto_seconds=$((finished_epoch - started_epoch))
backup_age_seconds=$((finished_epoch - backup_epoch))
cat > "$OUTPUT_DIR/evidence.md" <<EOF
# Restore drill evidence

- Started: $started_at
- Finished: $finished_at
- Source database: $SOURCE_DATABASE
- Isolated restore database: $RESTORE_DATABASE
- Measured restore time (RTO evidence): ${rto_seconds}s
- Backup age at validation (RPO sample): ${backup_age_seconds}s
- Blocked executions preserved: $restored_blocked
- Orphan execution plans: $orphan_count
- Kafka reconciliation: $kafka_result
- Migration validation: passed
- Dependency archive checksum file: SHA256SUMS

Attach the environment's PITR target, immutable chart values, ConfigMaps, Secret
exports, certificates, verifier policy, extension digest inventory, and
projection-volume export to this evidence before production approval.
EOF

printf 'Restore drill passed; evidence: %s/evidence.md\n' "$OUTPUT_DIR"

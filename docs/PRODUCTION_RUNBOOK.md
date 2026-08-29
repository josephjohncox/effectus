# Production Runbook

This runbook covers the checked `effectusd` daemon. It assumes an immutable OCI bundle and PostgreSQL durable state.

## Supported topology

Run one `effectusd` pod for each durable database and Kafka consumer group. The Helm chart sets `replicas: 1` and `strategy.type: Recreate`.

Do not change the Deployment to a rolling strategy. Old and new versions cannot safely admit work at the same time.

A Recreate update causes downtime. Kubernetes stops the old pod before it starts the new pod. A ReadWriteOnce PVC can then move between nodes without two writers.

## Startup checklist

1. Resolve the image and bundle to immutable digests.
2. Verify the image, chart, bundle, SBOMs, provenance, and signed checksum manifest.
3. Apply database migrations with the DDL Secret.
4. Start the application with the DML Secret and migration validation mode.
5. Verify `/healthz`, `/readyz`, `/api/status`, and `/metrics`.
6. Verify the active generation digest against the approved bundle digest.
7. Verify the PostgreSQL pool limit against the database connection budget.
8. Verify certificate expiry and the external HTTP TLS route.

The daemon always installs a checked engine. It rejects rule hotload, extension reload, and schema reload before it opens a database or listener.

You can use `/api/rules/validate` to check a candidate. Activate a candidate only through an immutable deployment.

## Database migrations

Use separate PostgreSQL roles for DDL and DML.

The DDL role can change the `effectus_*` tables and `effectus_saga_goose_db_version`. Store this role in the migration Secret.

The DML role can read and change runtime rows. It can read the migration version table but cannot change the schema.

Apply migrations with a one-shot command:

```bash
EFFECTUS_POSTGRES_DSN="$DDL_DSN" effectusd --database-migrations=apply
```

The Helm migration Job uses `migrations.existingSecret`. The Deployment uses `postgres.existingSecret` and `--database-migrations=validate`.

`--database-migrations=legacy-apply` keeps the old startup behavior. Use this mode only during a controlled transition.

Use expand and contract migrations across separate releases. Deploy the compatible application before a contract migration.

A schema rollback can lose data or fail after a contract migration. Restore a backup when a down migration is not proven safe.

Normal startup performs read-only schema validation.
Startup requires every embedded migration through version 10004.
Use `--database-migrations=validate-only` to validate a database and exit.

- `GET /healthz` checks liveness.
- `GET /readyz` checks the active bundle, schema, and verb registry.
- `GET /api/status` shows the generation and runtime state. This endpoint requires a token.
- `GET /metrics` shows runtime and PostgreSQL pool metrics.

Alert on PostgreSQL waits and sustained pool saturation. Use `effectusd_database_wait_total` and `effectusd_database_in_use_connections`.

## Rollout procedure

1. Stop new external HTTP admission at the ingress.
2. Wait for the Service endpoints to remove the old pod.
3. Record the Kafka consumer-group position.
4. Start the Helm upgrade.
5. Wait for the old pod to finish its shutdown deadline.
6. Wait for the new pod to pass readiness.
7. Verify the generation digest and Kafka consumer-group position.
8. Reconcile an interrupted record with its stable delivery ID.
9. Restore external HTTP admission.

The default shutdown timeout is 30 seconds. The chart sets `terminationGracePeriodSeconds` to 45 seconds.

Keep the grace period greater than the shutdown timeout. The margin lets Kubernetes deliver SIGTERM and finish container cleanup.

The daemon stops HTTP admission and drains active HTTP handlers with the configured shutdown context. It also cancels recovery and Kafka workers.

Cancellation can interrupt an active Kafka handler or commit. The record stays uncommitted and the new consumer replays it.

If the deadline expires, the daemon reports the error and closes the HTTP server. Investigate the incomplete request before replay.

## External HTTP TLS boundary

The HTTP server does not terminate TLS. Expose bearer-token HTTP only through a trusted TLS ingress or service mesh.

Terminate TLS at the approved ingress or mesh proxy. Store and rotate certificates in that control plane.

Use a ClusterIP Service. Restrict direct Service access with the cluster network policy and namespace policy.

Trust forwarded client headers only from the approved proxy. Remove untrusted `Forwarded` and `X-Forwarded-*` headers at that boundary.

Do not expose the metrics port or health endpoints to the public network.

## PostgreSQL connection budget

The default per-pod pool has 20 open connections and 5 idle connections. The default lifetimes are 30 minutes and 5 minutes.

Set these values with `database.max_open`, `database.max_idle`, `database.max_lifetime`, and `database.max_idle_time`.

For example, reserve 20 connections for `effectusd`, 2 for migrations, and 5 for operators. Keep this total below the PostgreSQL role limit.

The supported chart topology has one pod. Do not multiply this budget for untested replicas.

## Secret and certificate rotation

Environment variables and the gRPC key pair load once at startup. A Secret update does not change a running process.

Change `rolloutNonce` after an externally managed Secret or gRPC TLS Secret changes. You can also set a reloader annotation through `podAnnotations`.

### API token rotation

1. Add the new token to the comma-separated token Secret.
2. Change `rolloutNonce` and wait for readiness.
3. Move clients to the new token.
4. Remove the old token from the Secret.
5. Change `rolloutNonce` again.
6. Verify rejected requests for the old token.

This dual-token process prevents a forced client cutover.

### PostgreSQL credential rotation

1. Create the new DML credential with the same grants.
2. Keep the old credential valid.
3. Update the DML Secret and change `rolloutNonce`.
4. Verify readiness and database pool metrics.
5. Revoke the old credential after the rollback window.

Keep the old credential available during rollback. Rotate the DDL Secret in a separate operation.

### gRPC certificate rotation

1. Issue a certificate that overlaps the current certificate validity.
2. Replace the TLS Secret.
3. Change `rolloutNonce`.
4. Verify the served chain and hostname after readiness.
5. Remove the old certificate after all clients trust the new chain.

Alert before certificate expiry. A file update alone does not reload the key pair.

## Kafka delivery ledger and poison records

`effectus_kafka_deliveries` in PostgreSQL is the authoritative Kafka attempt and poison ledger. Back up this table with the other durable tables.

Inspect poison rows with this query:

```sql
SELECT delivery_id, failures, poison_acknowledged, poison_policy,
       poison_error, topic, partition_id, offset_id, updated_at
FROM effectus_kafka_deliveries
WHERE failures > 0 OR poison_acknowledged
ORDER BY updated_at;
```

The `halt` policy preserves the uncommitted offset. Fix the cause before you restart the consumer.

The `skip` and `dlq` policies set `poison_acknowledged`. Confirm the Kafka offset and the downstream result before replay.

Never delete an unacknowledged poison row. The prune command deletes only acknowledged poison rows before the cutoff.

## Retention and pruning

Set a retention period from audit, replay, and recovery requirements. Keep blocked and active records without a time limit until an operator resolves them.

Run a dry-run first:

```bash
EFFECTUS_POSTGRES_DSN="$DML_DSN" effectusd \
  --admin-prune-before=2026-01-01T00:00:00Z \
  --admin-prune-batch-size=500 \
  --admin-prune-dry-run=true
```

The command reports row counts for each table. Review the counts and blocked-state queries.

Create a backup and complete a test restore before destructive mode. Then run:

```bash
EFFECTUS_POSTGRES_DSN="$DML_DSN" effectusd \
  --admin-prune-before=2026-01-01T00:00:00Z \
  --admin-prune-batch-size=500 \
  --admin-prune-dry-run=false \
  --admin-prune-backup-verified=true
```

The operation uses one bounded transaction. It deletes terminal rows in foreign-key order.

Terminal execution states are `completed` and `failed`. Terminal saga states are `completed`, `compensated`, and `failed`.

The operation preserves blocked, running, queued, retry, leased, and unacknowledged poison state. It removes only retired generations and unreferenced artifacts.

Repeat small batches until the report returns zero. Monitor transaction duration, locks, replica lag, and database size.

## Backup scope and PITR

Back up all tables with the `effectus_` prefix. Include these control tables:

- `effectus_saga_goose_db_version`
- `effectus_saga_instances`, `effectus_saga_steps`, `effectus_saga_outbox`, and `effectus_saga_attempts`
- `effectus_fencing_counters` and `effectus_fencing_leases`
- `effectus_execution_artifacts`, `effectus_rule_generations`, and `effectus_executions`
- `effectus_execution_plans`, `effectus_fact_applications`, and `effectus_fact_snapshots`
- `effectus_kafka_deliveries`

Also back up PostgreSQL roles, grants, extensions, and database parameters. Keep the immutable bundle and release artifacts for every retained generation.

Run these read-only checks on the source before backup and on the restored database. Compare every row count.

```sql
BEGIN TRANSACTION READ ONLY;
SELECT 'execution_artifacts' AS table_name, count(*) AS row_count FROM effectus_execution_artifacts
UNION ALL SELECT 'rule_generations', count(*) FROM effectus_rule_generations
UNION ALL SELECT 'executions', count(*) FROM effectus_executions
UNION ALL SELECT 'execution_plans', count(*) FROM effectus_execution_plans
UNION ALL SELECT 'fact_applications', count(*) FROM effectus_fact_applications
UNION ALL SELECT 'fact_snapshots', count(*) FROM effectus_fact_snapshots
UNION ALL SELECT 'saga_instances', count(*) FROM effectus_saga_instances
UNION ALL SELECT 'saga_steps', count(*) FROM effectus_saga_steps
UNION ALL SELECT 'saga_outbox', count(*) FROM effectus_saga_outbox
UNION ALL SELECT 'saga_attempts', count(*) FROM effectus_saga_attempts
UNION ALL SELECT 'fencing_counters', count(*) FROM effectus_fencing_counters
UNION ALL SELECT 'fencing_leases', count(*) FROM effectus_fencing_leases
UNION ALL SELECT 'kafka_deliveries', count(*) FROM effectus_kafka_deliveries
ORDER BY table_name;
COMMIT;
```

Run these referential-integrity checks after restore. Each query must return zero.

```sql
SELECT count(*) AS orphan_executions
FROM effectus_executions AS e
LEFT JOIN effectus_execution_artifacts AS a USING (generation_digest)
WHERE a.generation_digest IS NULL;

SELECT count(*) AS orphan_plans
FROM effectus_execution_plans AS p
LEFT JOIN effectus_executions AS e USING (execution_id)
LEFT JOIN effectus_saga_instances AS s USING (saga_id)
WHERE e.execution_id IS NULL OR s.saga_id IS NULL;

SELECT count(*) AS orphan_facts
FROM (
  SELECT execution_id FROM effectus_fact_applications
  UNION ALL
  SELECT execution_id FROM effectus_fact_snapshots
) AS f
LEFT JOIN effectus_executions AS e USING (execution_id)
WHERE e.execution_id IS NULL;

SELECT count(*) AS orphan_saga_records
FROM (
  SELECT step.saga_id
  FROM effectus_saga_steps AS step
  LEFT JOIN effectus_saga_instances AS saga USING (saga_id)
  WHERE saga.saga_id IS NULL
  UNION ALL
  SELECT outbox.saga_id
  FROM effectus_saga_outbox AS outbox
  LEFT JOIN effectus_saga_instances AS saga USING (saga_id)
  WHERE saga.saga_id IS NULL
) AS orphan;

SELECT count(*) AS orphan_attempts
FROM effectus_saga_attempts AS attempt
LEFT JOIN effectus_saga_outbox AS outbox USING (dispatch_id)
WHERE outbox.dispatch_id IS NULL;

SELECT count(*) AS orphan_fencing_leases
FROM effectus_fencing_leases AS lease
LEFT JOIN effectus_fencing_counters AS counter USING (authority, resource)
WHERE counter.authority IS NULL;
```

PITR requires continuous WAL archiving, a tested base backup, retention beyond the recovery window, and synchronized time.

The service owner defines the RPO and RTO. The database owner confirms that backup frequency and restore capacity meet them.

Record the approved RPO, RTO, backup owner, restore owner, and escalation contact in the service inventory.

## Disaster recovery

1. Isolate the failed environment from HTTP and Kafka traffic.
2. Select a restore point before the incident.
3. Restore PostgreSQL and all required roles into an isolated environment.
4. Restore the exact image, bundle, verifier policy, and extension artifacts.
5. Run `effectusd --database-migrations=validate-only` with the restored database.
6. Compare the migration version with the release requirement.
7. Run the blocked-state and Kafka ledger queries.
8. Verify artifact digests for every active or referenced generation.
9. Reconcile Kafka offsets with `effectus_kafka_deliveries` and execution admission identities.
10. Test a read-only status request and a controlled non-production execution.
11. Approve the recovery point with the service and database owners.
12. Resume one traffic source at a time.

Do not replay a Kafka record only because the consumer offset moved back. First search its stable delivery ID and admission identity.

A completed execution must not run again under a new identity. Resume accepted or running state through recovery workers.

Preserve blocked executions and their artifacts. Do not change their state with ad hoc SQL.

Run a restore drill at least each quarter. Measure the achieved RPO and RTO.
Record the drill date, restore point, backup ID, release digest, bundle digest, migration output, SQL output, Kafka reconciliation, RPO, RTO, owner, and approval.

## Blocked execution inspection

List blocked executions:

```sql
SELECT execution_id, admission_identity, state, generation_digest,
       recovery_owner, recovery_deadline, last_error, updated_at
FROM effectus_executions
WHERE state IN ('blocked_unknown', 'blocked_fence',
                'blocked_dependency', 'blocked_compensation')
ORDER BY updated_at;
```

List blocked saga and outbox state:

```sql
SELECT saga.saga_id, saga.execution_id, saga.state AS saga_state,
       outbox.dispatch_id, outbox.state AS outbox_state,
       outbox.last_outcome, outbox.last_error, outbox.updated_at
FROM effectus_saga_instances AS saga
LEFT JOIN effectus_saga_outbox AS outbox ON outbox.saga_id = saga.saga_id
WHERE saga.state LIKE 'blocked_%' OR outbox.state LIKE 'blocked_%'
ORDER BY saga.updated_at, outbox.sequence;
```

Approved actions are cause correction, dependency restoration, fence repair, or code rollback with the same generation artifact. Use the recovery worker after correction.

Do not mark blocked work as completed. Do not delete it. Escalate an unknown external outcome before a replay.

Run the repository restore harness before a release drill:

```bash
KAFKA_BROKERS=localhost:9092 just restore-drill
```

The harness creates an isolated restore database, validates migrations and relational integrity, compares blocked-state counts, archives non-PostgreSQL inputs, optionally exercises Kafka commit/restart reconciliation, and writes `out/restore-drill/evidence.md`. Set `RESTORE_INPUTS` to the environment's immutable chart values, ConfigMaps, Secret exports, certificates, verifier policy, extension inventory, and PVC export. Set `FACTS_PROJECTION_PATH` when the optional facts projection volume is enabled.

## Release failure cleanup

Registry and GitHub publication cannot form one transaction. Before release, configure every production registry repository to reject tag mutation and verify that policy with a denied overwrite test. The workflow checks each destination again immediately before it writes, promotes verified digests, and creates a signed `release-manifest.json`. The GitHub release that contains this manifest is the sole completion marker.

If promotion fails before the completion marker, treat the version as incomplete. Keep the immutable digests and do not overwrite or recreate any version tag. Remove only staging tags after you preserve logs and signatures.

Investigate any missing signature, SBOM, provenance record, chart, bundle, or archive. Start a new patch version after correction. A release is consumable only when its signed completion manifest lists all three verified digest references.

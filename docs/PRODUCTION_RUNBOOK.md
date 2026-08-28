# Production Runbook

This runbook applies to the checked `effectusd` daemon.

## Release limits

The checked engine uses an immutable generation.
Rule validation is read-only.
Rule apply, rollback, schema reload, and extension reload return an error.
Publish a new image and bundle digest to change execution behavior.

The single-replica Helm chart uses the `Recreate` strategy.
An upgrade has downtime because old and new generations must not overlap.

## Startup checklist

1. Pin the image and bundle by digest.
2. Verify the bundle signature.
3. Create a database backup.
4. Run `effectusd --migrate-only` with the migration credential.
5. Start `effectusd` with a runtime credential that has no DDL rights.
6. Set API tokens and ACL rules.
7. Set `trusted_proxy_cidrs` only for known proxy networks.
8. Set the database pool from the approved connection budget.
9. Check `/readyz` and `/metrics`.

Normal startup performs read-only schema validation.
Startup fails if migration version 10003 is not present.

The default pool allows 20 open connections and 10 idle connections.
The default connection lifetime is 30 minutes.
The default idle time is 5 minutes.

## Health and metrics

Use `GET /healthz` for liveness.
Use `GET /readyz` for admission readiness.
Use `GET /api/status` for the bundle and checked engine digests.
Use `GET /metrics` for execution, recovery, and database pool metrics.

Alert when recovery errors increase.
Alert when blocked executions increase.
Alert when database connections remain near the configured maximum.
Alert when the recovery backlog does not return to zero.

## Deployment and shutdown

1. Remove the pod from readiness.
2. Stop new HTTP admission.
3. Let active HTTP handlers finish within `shutdown-timeout`.
4. Stop Kafka polling.
5. Let the current Kafka record finish or remain uncommitted.
6. Stop the recovery worker.
7. Start the replacement pod.
8. Check the engine generation digest before you restore traffic.

Set the Kubernetes termination grace period above `shutdown-timeout`.
The default chart can cause a short outage during replacement.
Do not change the strategy to a rolling update for checked execution.

## Trusted proxies and rate limits

The daemon uses `RemoteAddr` by default.
It ignores `X-Forwarded-For` from an untrusted direct peer.
Configure trusted proxy CIDRs for the direct proxy path only.
The daemon rejects malformed forwarded chains.

The rate limiter authenticates token requests before it creates client state.
The default cache holds 10,000 clients.
The default idle time is 10 minutes.

## Database maintenance

Create a verified backup before each delete run.
First run the command in dry-run mode:

```bash
EFFECTUS_SAGA_POSTGRES_DSN="$MAINTENANCE_DSN" effectusd \
  --maintenance-prune \
  --maintenance-dry-run=true \
  --maintenance-retention=720h \
  --maintenance-batch=1000
```

Run the same command with `--maintenance-dry-run=false` after approval.
The command deletes records in foreign-key order.
It selects only old `completed` and `failed` executions.
It never selects blocked or nonterminal executions.
It deletes only acknowledged Kafka poison records.

## Blocked execution response

Inspect blocked work with this query:

```sql
SELECT execution_id, tenant_namespace, state, last_error, updated_at
FROM effectus_executions
WHERE state LIKE 'blocked_%'
ORDER BY updated_at, execution_id;
```

Inspect the related saga and dispatch rows before any manual action:

```sql
SELECT p.execution_id, s.saga_id, s.state AS saga_state,
       o.dispatch_id, o.state AS dispatch_state, o.last_error
FROM effectus_execution_plans AS p
JOIN effectus_saga_instances AS s ON s.saga_id = p.saga_id
LEFT JOIN effectus_saga_outbox AS o ON o.saga_id = s.saga_id
WHERE p.execution_id = '<execution-id>'
ORDER BY o.sequence, o.direction;
```

Do not change `blocked_unknown` to a retry state without destination evidence.
First verify the destination idempotency key or fencing token.
Use a reviewed database procedure for any manual state change.
Keep an audit record of the query, evidence, approver, and result.

## Backup and recovery scope

Back up every table with the `effectus_` prefix.
This scope includes executions, artifacts, generations, facts, sagas, dispatches, attempts, fencing, and Kafka deliveries.
Also back up `effectus_saga_goose_db_version`.

Use PostgreSQL physical backups and WAL archiving for point-in-time recovery.
Set the production RPO and RTO in the service deployment record.
Test that the backup schedule can meet both values.
Do not claim an RPO or RTO that the restore drill has not met.

## Restore drill

1. Stop all `effectusd` consumers.
2. Record Kafka group offsets for each topic and partition.
3. Restore the database to a disposable PostgreSQL instance.
4. Run read-only schema validation with the runtime credential.
5. Compare table counts with the backup manifest.
6. Run the blocked execution queries.
7. Start one daemon against the restored database and a test destination.
8. Replay one matching HTTP identity.
9. Verify that the replay uses the recorded execution ID.
10. Verify Kafka offsets before any production consumer starts.
11. Record the measured RPO, RTO, and validation results.

Use this count query after restore:

```sql
SELECT 'executions' AS kind, count(*) FROM effectus_executions
UNION ALL SELECT 'artifacts', count(*) FROM effectus_execution_artifacts
UNION ALL SELECT 'facts', count(*) FROM effectus_fact_applications
UNION ALL SELECT 'sagas', count(*) FROM effectus_saga_instances
UNION ALL SELECT 'dispatches', count(*) FROM effectus_saga_outbox
UNION ALL SELECT 'attempts', count(*) FROM effectus_saga_attempts
UNION ALL SELECT 'fencing', count(*) FROM effectus_fencing_counters
UNION ALL SELECT 'kafka_deliveries', count(*) FROM effectus_kafka_deliveries;
```

A database restore does not restore Kafka offsets.
If Kafka offsets are ahead of the restored ledger, records can be missing from the ledger.
If Kafka offsets are behind the restored ledger, records replay through stable delivery identities.
Prefer the second condition when reconciliation requires a choice.

## Migration rollback limits

Migrations 10001 through 10003 create the durable protocol schema.
A down migration deletes durable records.
Do not use a down migration as an application rollback.
Restore the pre-migration backup when a schema rollback is necessary.

## Secret and certificate rotation

Secret changes do not reload a running process.
Use pod annotations with the approved secret-reloader controller, or start a controlled Helm rollout.

Rotate API tokens in this order:

1. Add the new token while the old token remains valid.
2. Replace the pod.
3. Move clients to the new token.
4. Remove the old token.
5. Replace the pod again.

Rotate the database DSN or gRPC certificate in this order:

1. Add the new credential or certificate.
2. Replace the pod with the `Recreate` strategy.
3. Wait for HTTP and Kafka drain.
4. Verify new connections and the new certificate.
5. Revoke the old secret.

## Kafka poison recovery

The authoritative attempt and poison data is `effectus_kafka_deliveries`.
The daemon does not use a file delivery ledger.
Back up this table with the execution ledger.

For `halt`, inspect the failed delivery before you restart the consumer.
For `skip`, verify the acknowledged poison row before you advance the offset.
For `dlq`, search the DLQ by the stable source delivery ID.
A crash between DLQ publication and offset commit can create a duplicate DLQ record.

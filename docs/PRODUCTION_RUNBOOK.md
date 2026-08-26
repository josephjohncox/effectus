# Production Runbook

This runbook assumes you run `effectusd` with a config file (`--config`) and bundles pulled from OCI.

## Startup checklist

- Bundle: verify the OCI reference and expected version.
- Auth: set API tokens and ACLs; keep `/api/*` protected.
- Verbs: set `verbs.strict: true` for runtime arg/return checks.
- Facts: configure a persisted store and merge strategy per namespace.
- Saga: enable a persistent saga store (Redis/Postgres) if compensations are used.

## Health and readiness

- `GET /healthz` for liveness.
- `GET /readyz` to check that the active generation has a bundle, schema, and verb registry.
- `GET /api/status` for live counts and schema sources (token required).
- `GET /metrics` for Prometheus scraping.

Read `LIFECYCLE.md` before you use refresh or rollback operations.

## Hotload workflow

1. Validate rules:

```bash
curl -X POST http://localhost:8080/api/rules/validate \
  -H 'Authorization: Bearer $TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"path":"rules/new.eff","content":"..."}'
```

2. Canary (optional): include a `canary` payload in the hotload request to compare summaries.
3. Hotload:

```bash
curl -X POST http://localhost:8080/api/rules/hotload \
  -H 'Authorization: Bearer $TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"path":"rules/new.eff","content":"...","confirm":true}'
```

4. Verify the `generation_id` from `/api/status` and check the reload metrics.

Validation returns HTTP 422 for an invalid candidate. Activation returns HTTP 409 when another refresh changes the generation first.

## Rollback

The current rollback history is process-local. Do not use it as a durable rollback log.

Rollback recompiles the saved source against the current schema and verbs. An incompatible snapshot returns HTTP 422.

1. List snapshots:

```bash
curl -H 'Authorization: Bearer $TOKEN' http://localhost:8080/api/rules/history
```

2. Roll back by snapshot ID:

```bash
curl -X POST http://localhost:8080/api/rules/rollback \
  -H 'Authorization: Bearer $TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"id":"<snapshot-id>"}'
```

## Incident notes

- If hotload is risky, disable it and redeploy from OCI.
- For deterministic replay, use `fixed_time` with a known RFC3339 timestamp.
- Confirm logs include request IDs and watch for rate-limit rejections.

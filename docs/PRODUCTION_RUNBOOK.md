# Production Runbook

This runbook covers the checked `effectusd` daemon with an immutable OCI source
bundle and PostgreSQL durable state.

## Before rollout

1. Pin the image and source-bundle references by digest.
2. Verify the bundle with the approved verifier executable.
3. Apply migrations with a DDL-only PostgreSQL credential.
4. Deploy with a separate DML credential and `--database-migrations=validate`.
5. Set a nonempty `EFFECTUS_API_TOKEN` Secret.
6. Configure TLS for every gRPC listener and for the HTTP ingress.

The Helm chart uses one replica and a Recreate strategy. Do not change it to a
rolling deployment or add replicas for a database and Kafka consumer group.

## Health and authentication

- `GET /healthz` is an unauthenticated liveness probe.
- `GET /readyz` is an unauthenticated readiness probe after startup completes.
- `GET /v1/status` requires `Authorization: Bearer TOKEN` and returns the
  active generation identity.
- `POST /v1/dry-run` and `POST /v1/execute` also require that bearer token.

The daemon has no UI endpoint and no metrics endpoint. Do not configure probes
or monitoring checks for `/ui`, `/metrics`, or `/api/*`.

The HTTP server does not terminate TLS. Expose it only through a trusted TLS
ingress or service mesh, and restrict direct Service access with network
policy. gRPC requires TLS unless an operator explicitly sets
`--grpc-allow-insecure`; bearer authentication remains required in either mode.

## Rollout

1. Stop new admissions at the ingress.
2. Wait for Kubernetes to remove the old endpoint.
3. Start the Recreate upgrade.
4. Wait for `/readyz`.
5. Use an authenticated `/v1/status` request to verify the approved generation
   digest.
6. Restore ingress admission.

Kafka offsets commit only at the configured acknowledgement boundary. A stop
during a handler or commit leaves the record uncommitted; the replacement
consumer replays it with the same stable delivery identity. PostgreSQL keeps
Kafka handler failure counts across rebalances and process restarts.

## Secret rotation

The daemon reads `EFFECTUS_API_TOKEN` and gRPC key material at startup. To
rotate either value, update the Secret, change `rolloutNonce`, wait for
readiness, update clients, then retire the old credential. The daemon accepts
one API token at a time, so retain the old ingress path until all clients have
moved.

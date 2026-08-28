# Saga Development Stack

This Compose stack provides PostgreSQL for the production-compatible daemon and Redis only for embedded library compatibility tests.

## Start PostgreSQL for effectusd

```bash
just setup-db
```

The recipe runs `docker compose -f examples/saga_stack/docker-compose.yml up -d postgres`, waits with `pg_isready`, and exposes the database at `localhost:55433` with DSN `postgres://effectus:effectus@localhost:55433/effectus_saga?sslmode=disable`.

To run a compatibility test that explicitly needs Redis, start that service separately with `docker compose -f examples/saga_stack/docker-compose.yml up -d redis`.

## Stop

```bash
docker compose -f examples/saga_stack/docker-compose.yml down -v
```

The `-v` option deletes the development data volumes.

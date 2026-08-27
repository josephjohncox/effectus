# Saga Development Stack

This Compose stack starts PostgreSQL and Redis for saga integration tests.

## Start

```bash
docker compose -f examples/saga_stack/docker-compose.yml up -d
```

The stack exposes PostgreSQL at `localhost:55433` and Redis at `localhost:56379`.

## Stop

```bash
docker compose -f examples/saga_stack/docker-compose.yml down -v
```

The `-v` option deletes the development data volumes.

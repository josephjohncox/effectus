# Durable Test Fixture

This Compose fixture supplies PostgreSQL to daemon integration tests and Redis
to compatibility tests. It is not a user example or deployment template.

Start it with:

```bash
docker compose -f tests/fixtures/durable-stack/docker-compose.yml up -d
```

PostgreSQL listens on `127.0.0.1:55433`; Redis listens on `127.0.0.1:56379`.
Remove fixture data only when intended:

```bash
docker compose -f tests/fixtures/durable-stack/docker-compose.yml down -v
```

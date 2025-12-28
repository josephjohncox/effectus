# Saga Stack

Docker compose stack for saga integration tests (Postgres + Redis).

```bash
docker compose -f examples/saga_stack/docker-compose.yml up -d
```

Default ports:
- Postgres: `localhost:55433`
- Redis: `localhost:56379`
```

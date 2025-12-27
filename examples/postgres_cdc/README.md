# Postgres CDC Example

Streams logical changes using the `postgres_cdc` adapter.

## Prereqs
- Postgres with `wal2json` installed and `wal_level=logical`
- `POSTGRES_DSN` set (defaults to local dev stack below)

## Run
```bash
docker compose -f examples/cdc_stack/docker-compose.yml up -d
POSTGRES_DSN="postgres://effectus:effectus@localhost:5432/effectus_cdc?sslmode=disable" \
go run ./examples/postgres_cdc
```

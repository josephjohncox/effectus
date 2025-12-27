# MySQL CDC Example

Streams binlog row events using the `mysql_cdc` adapter.

## Prereqs
- MySQL with ROW binlog enabled
- `MYSQL_*` env vars set (defaults to local dev stack below)

## Run
```bash
docker compose -f examples/cdc_stack/docker-compose.yml up -d
MYSQL_HOST="localhost" \
MYSQL_PORT="3306" \
MYSQL_USER="effectus" \
MYSQL_PASSWORD="effectus" \
MYSQL_DATABASE="effectus_cdc" \
MYSQL_DSN="effectus:effectus@tcp(localhost:3306)/effectus_cdc?parseTime=true" \
go run ./examples/mysql_cdc
```

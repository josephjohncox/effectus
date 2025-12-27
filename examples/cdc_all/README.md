# CDC All-In-One Demo

Runs Postgres CDC, MySQL CDC, and AMQP adapters together.

## Run
```bash
docker compose -f examples/cdc_stack/docker-compose.yml up -d
go run ./examples/cdc_all
```

## Env Overrides
- `POSTGRES_DSN`
- `MYSQL_HOST`, `MYSQL_PORT`, `MYSQL_USER`, `MYSQL_PASSWORD`, `MYSQL_DATABASE`, `MYSQL_DSN`
- `AMQP_URL`, `AMQP_QUEUE`

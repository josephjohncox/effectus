# CDC Stack (Postgres + MySQL + RabbitMQ)

This compose stack is used by the CDC and AMQP examples.

## Start the stack
```bash
docker compose -f examples/cdc_stack/docker-compose.yml up -d
```

Or:
```bash
just cdc-up
```

## Services
- Postgres (wal2json): localhost:5432
- MySQL (ROW binlog): localhost:3306
- RabbitMQ: localhost:5672 (management: localhost:15672)

## Default credentials
- Postgres: user `effectus`, pass `effectus`, db `effectus_cdc`
- MySQL: user `effectus`, pass `effectus`, db `effectus_cdc`
- RabbitMQ: user `guest`, pass `guest`

## Notes
- Postgres is built locally to include the wal2json plugin.

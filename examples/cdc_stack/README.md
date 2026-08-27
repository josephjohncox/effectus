# CDC Development Stack

This Compose stack starts PostgreSQL, MySQL, and RabbitMQ for local CDC and AMQP examples.

## Start

From the repository root, run:

```bash
docker compose -f examples/cdc_stack/docker-compose.yml up -d
```

You can also run:

```bash
just cdc-up
```

## Services

| Service | Address | Development credentials |
| --- | --- | --- |
| PostgreSQL with `wal2json` | `localhost:5432` | `effectus` / `effectus` |
| MySQL with row binlog | `localhost:3306` | `effectus` / `effectus` |
| RabbitMQ | `localhost:5672` | `guest` / `guest` |
| RabbitMQ management | `localhost:15672` | `guest` / `guest` |

Both databases use the `effectus_cdc` database.

The PostgreSQL image builds locally and installs `wal2json`. These credentials and ports are for local development only.

## Stop

```bash
docker compose -f examples/cdc_stack/docker-compose.yml down -v
```

# CDC Test Fixture

This Compose fixture starts PostgreSQL, MySQL, and RabbitMQ for adapter
integration tests. It is not a supported example or deployment template.

```bash
docker compose -f tests/fixtures/cdc-stack/docker-compose.yml up -d
# Run tests, then remove the fixture data only when intended:
docker compose -f tests/fixtures/cdc-stack/docker-compose.yml down -v
```

The fixture binds PostgreSQL `5432`, MySQL `3306`, RabbitMQ `5672`, and its
management interface `15672` to loopback for local test use.

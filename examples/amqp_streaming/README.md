# AMQP Streaming Example

Consumes messages from a RabbitMQ queue using the `amqp` adapter.

## Run
```bash
docker compose -f examples/cdc_stack/docker-compose.yml up -d
AMQP_URL="amqp://guest:guest@localhost:5672/" \
AMQP_QUEUE="effectus.events" \
go run ./examples/amqp_streaming
```

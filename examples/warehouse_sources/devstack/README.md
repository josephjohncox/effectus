# Warehouse Devstack (Trino + Iceberg + MinIO)

This devstack boots a local Iceberg lakehouse backed by MinIO and served by Trino.
Use it to validate the `iceberg` adapter and the `trino_iceberg.yaml` config.

## Start the stack
```bash
cd examples/warehouse_sources/devstack
docker compose up -d
```

Or from the repo root:
```bash
just devstack-up
```

## Stop the stack
```bash
docker compose down
```

Or:
```bash
just devstack-down
```

## Seed a sample Iceberg table
```bash
./scripts/seed-iceberg.sh
```

Or:
```bash
just devstack-seed-iceberg
```

## Smoke test (boot + seed + query)
```bash
./scripts/smoke-test.sh
```

Or:
```bash
just devstack-smoke-test
```

## Open the Trino CLI
```bash
./scripts/trino-cli.sh
```

Or:
```bash
just devstack-trino-cli
```

Verify via Trino:
```bash
curl -s -X POST http://localhost:8080/v1/statement \
  -H "X-Trino-User: effectus" \
  -H "X-Trino-Catalog: lakehouse" \
  -H "X-Trino-Schema: sales" \
  --data "SELECT * FROM orders"
```

## Seed S3 JSON data (optional)
```bash
./scripts/seed-s3.sh
```

Or:
```bash
just devstack-seed-s3
```

## Seed S3 Parquet data (optional)
```bash
./scripts/seed-parquet.sh
```

Or:
```bash
just devstack-seed-parquet
```

Use these configs:
- `../trino_iceberg.yaml`
- `../snowflake.yaml` (for reference only)

## Notes
- MinIO API: http://localhost:9000 (user/pass: minioadmin/minioadmin)
- MinIO console: http://localhost:9001
- Trino URL: http://localhost:8080
- TRINO_DSN example: `http://effectus@localhost:8080?catalog=lakehouse&schema=sales`

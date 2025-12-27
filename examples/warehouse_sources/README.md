# Warehouse Sources (Snowflake + Trino/Iceberg)

Concrete, production-style configs for pulling facts from a warehouse (Snowflake) and a lakehouse (Iceberg via Trino).
These files are ready to drop into your config loader or adapt to your runtime.

## Files
- `snowflake.yaml` - batch snapshot from Snowflake
- `trino_iceberg.yaml` - streaming Iceberg table via Trino
- `sources.yaml` - both sources in one file
- `env.example` - required environment variables
- `devstack/` - Trino + Iceberg + MinIO local stack
- `s3_parquet_demo/` - Parquet reader example for the S3 adapter

## Quick Start
1. Copy `env.example` -> `.env` and set your DSNs.
2. Load the YAML into `[]adapters.SourceConfig`.
3. Call `adapters.CreateSource(...)` for each entry and subscribe to facts.

## Tips
- Use `schema_name` + `schema_version` when you only emit a single fact type.
- Use `mappings` if you want per-table or per-topic schemas.
- For Iceberg, `catalog.namespace.table` maps to Trino's catalog + schema + table.

## Related Docs
- `docs/FACT_SOURCES.md`
- `docs/TUTORIALS.md`

## Local Devstack
Use `devstack/` to run Trino + Iceberg + MinIO locally:

```bash
cd examples/warehouse_sources/devstack
docker compose up -d
./scripts/seed-iceberg.sh
```

Or use just targets from repo root:
```bash
just devstack-up
just devstack-seed-iceberg
```

Open the Trino CLI:
```bash
./scripts/trino-cli.sh
```

Seed S3 Parquet data:
```bash
./scripts/seed-parquet.sh
```

## Demo: S3 Parquet Reader
Use the demo to read Parquet facts from the devstack bucket:

```bash
S3_ENDPOINT="http://localhost:9000" \
S3_REGION="us-east-1" \
S3_BUCKET="exports" \
S3_PREFIX="parquet/" \
S3_ACCESS_KEY="minioadmin" \
S3_SECRET_KEY="minioadmin" \
go run ./examples/warehouse_sources/s3_parquet_demo
```

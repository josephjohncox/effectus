# Warehouse Source Examples

This directory contains example configurations for Snowflake, Trino, Iceberg, and S3.

Review credentials, limits, query cost, and polling behavior before you adapt these files.

## Files

- `snowflake.yaml` defines a Snowflake batch snapshot.
- `sql_scheduled_scrape.yaml` defines scheduled SQL polling.
- `trino_iceberg.yaml` defines an Iceberg query through Trino.
- `sources.yaml` combines warehouse sources.
- `env.example` lists environment variables.
- `devstack/` contains a local Trino, Iceberg, and MinIO stack.
- `s3_parquet_demo/` contains the Parquet reader example.

## Use a configuration

1. Copy `env.example` to `.env`.
2. Set development credentials.
3. Decode the YAML into `[]adapters.SourceConfig`.
4. Call `adapters.CreateSource` for each entry.
5. Start each source and subscribe to its facts.

Use `schema_name` and `schema_version` for one emitted fact type. Use mappings when tables or topics emit different types.

## Start the local stack

```bash
cd examples/warehouse_sources/devstack
docker compose up -d
./scripts/seed-iceberg.sh
```

From the repository root, you can run:

```bash
just devstack-up
just devstack-seed-iceberg
```

Open the Trino CLI:

```bash
./scripts/trino-cli.sh
```

Seed Parquet objects:

```bash
./scripts/seed-parquet.sh
```

## Run the S3 Parquet example

```bash
S3_ENDPOINT="http://localhost:9000" \
S3_REGION="us-east-1" \
S3_BUCKET="exports" \
S3_PREFIX="parquet/" \
S3_ACCESS_KEY="minioadmin" \
S3_SECRET_KEY="minioadmin" \
go run ./examples/warehouse_sources/s3_parquet_demo
```

The MinIO credentials are for the local stack only.

Read [Fact Sources](../../docs/FACT_SOURCES.md) for adapter behavior and limits.

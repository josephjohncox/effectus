# Warehouse Sources (Snowflake + Trino/Iceberg)

Concrete, production-style configs for pulling facts from a warehouse (Snowflake) and a lakehouse (Iceberg via Trino).
These files are ready to drop into your config loader or adapt to your runtime.

## Files
- `snowflake.yaml` - batch snapshot from Snowflake
- `trino_iceberg.yaml` - streaming Iceberg table via Trino
- `sources.yaml` - both sources in one file
- `env.example` - required environment variables

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

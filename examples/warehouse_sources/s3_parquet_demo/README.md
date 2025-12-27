# S3 Parquet Demo

Reads Parquet objects from S3 (or MinIO) through the `s3` adapter and prints the
decoded rows.

## Run against the devstack
1. Start the devstack and seed Parquet data:

```bash
cd examples/warehouse_sources/devstack
docker compose up -d
./scripts/seed-parquet.sh
```

2. Run the demo from the repo root:

```bash
S3_ENDPOINT="http://localhost:9000" \
S3_REGION="us-east-1" \
S3_BUCKET="exports" \
S3_PREFIX="parquet/" \
S3_ACCESS_KEY="minioadmin" \
S3_SECRET_KEY="minioadmin" \
go run ./examples/warehouse_sources/s3_parquet_demo
```

## Notes
- Set `S3_ENDPOINT` and `S3_FORCE_PATH_STYLE=true` for MinIO or other S3-compatible services.
- The demo reads a few facts then exits.

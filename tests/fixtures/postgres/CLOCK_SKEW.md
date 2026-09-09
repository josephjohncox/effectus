# PostgreSQL process-clock fixture

This fixture tests recovery with a database wall clock that differs from the Go caller by +24 or −24 hours.
It does not change the host clock, kernel clock, or an existing database.
Use a new container and an empty data directory for each offset. Do not mount an existing database directory.

## Requirements and limits

- Docker with Linux containers, Python 3, a POSIX shell, and the repository's Go toolchain.
- Network access for the image build. The image uses the same digest-pinned PostgreSQL 16 Alpine base as the remediation fixture.
- The Dockerfile pins libfaketime 0.9.13 to commit `86b37fde2fed7336ea2d0c17928e3015a55d9b4a` and verifies its archive SHA256.
- Builder packages come from the base image's Alpine repositories. The build is not claimed to be bit-for-bit reproducible. Record the resulting image ID and architecture.
- The measured setup used Linux/arm64. Other architectures require their own execution result.

The entrypoint runs `initdb` as `postgres` with the real clock.
Only the final PostgreSQL process tree receives `LD_PRELOAD` and `FAKETIME`.
`FAKETIME_DONT_FAKE_MONOTONIC=1` configures libfaketime to leave monotonic time unchanged.
The test verifies the SQL wall-clock offset directly. It does not directly instrument PostgreSQL's monotonic clock or simulate NTP corrections during a running lease.

TCP connections require SCRAM authentication. The published port binds only to host loopback.
Unix sockets use trust inside this task-owned container. This is a test fixture, not a production authentication configuration.
Do not enable shell tracing or print the password or DSN.

## Check startup guards without Docker

This test uses temporary files and fake child commands. It does not start a real database.
It checks HUP/INT/TERM exit and password-file cleanup, invalid offsets, and preservation of an existing database marker.

```bash
python3 -B -m unittest discover -s tests/fixtures/postgres \
  -p test_clock_entrypoint.py -v
```

## Create one isolated fixture

Run from the repository root in one shell. Choose `+24h` first.
For the second case, repeat these steps with `-24h` and a new run ID.

```bash
run_id=$(python3 -c 'import secrets; print(secrets.token_hex(8))')
clock_image="effectus-clock-test:$run_id"
clock_name="effectus-clock-test-$run_id"
export POSTGRES_PASSWORD
POSTGRES_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(24))')
export EFFECTUS_CLOCK_OFFSET=+24h
case "$EFFECTUS_CLOCK_OFFSET" in
  +24h) export EFFECTUS_CLOCK_OFFSET_SECONDS=86400 ;;
  -24h) export EFFECTUS_CLOCK_OFFSET_SECONDS=-86400 ;;
  *) exit 1 ;;
esac

docker build --label "effectus.clock-run=$run_id" \
  --tag "$clock_image" \
  --file tests/fixtures/postgres/clock-skew.Dockerfile \
  tests/fixtures/postgres
clock_image_id=$(docker image inspect --format '{{.Id}}' "$clock_image")
clock_id=$(docker create --name "$clock_name" \
  --label "effectus.clock-run=$run_id" \
  --publish 127.0.0.1::5432 --cap-drop ALL \
  --security-opt no-new-privileges \
  --env POSTGRES_PASSWORD --env EFFECTUS_CLOCK_OFFSET \
  "$clock_image_id")
docker start "$clock_id"
```

Keep the exact container ID, image ID, and run label for review and later cleanup.
The example permits 60 readiness attempts, with a one-second probe timeout and one-second delay after failure.
Docker command latency is additional. A failed or stopped container is a failed setup, not permission to use another database.

```bash
ready=false
for attempt in $(seq 1 60); do
  if docker exec "$clock_id" pg_isready -h 127.0.0.1 \
      -U postgres -d postgres -t 1 >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
[ "$ready" = true ] || { echo 'Clock fixture did not become ready' >&2; exit 1; }
export CLOCK_FIXTURE_ID="$clock_id" CLOCK_FIXTURE_RUN="$run_id"
port=$(python3 - <<'PY'
import json, os, subprocess
item = json.loads(subprocess.check_output(
    ["docker", "inspect", os.environ["CLOCK_FIXTURE_ID"]], text=True))[0]
assert item["Id"] == os.environ["CLOCK_FIXTURE_ID"]
assert item["Config"]["Labels"]["effectus.clock-run"] == os.environ["CLOCK_FIXTURE_RUN"]
assert item["State"]["Running"]
bindings = item["NetworkSettings"]["Ports"]["5432/tcp"]
assert len(bindings) == 1 and bindings[0]["HostIp"] == "127.0.0.1"
print(int(bindings[0]["HostPort"]))
PY
)
export EFFECTUS_CLOCK_POSTGRES_DSN="postgres://postgres:$POSTGRES_PASSWORD@127.0.0.1:$port/postgres?sslmode=disable"
```

## Run the required clock test

```bash
go test -race -count=5 -tags=integration,clockskew -timeout=45s -v \
  ./runtime -run '^TestPostgresRecoveryWithIndependentDatabaseClock$'
```

Both tags are required. Ordinary unit and `integration` runs do not select this test.
Once selected, a missing DSN, missing offset, or mismatched SQL clock fails rather than skips.
The SQL clock check precedes migrations and admission.
The test creates and retains its own unique execution identity in this isolated database.
Its worker claims only that identity through PostgreSQL APIs, without changing returned or persisted timestamps.

The test holds an executor across four 400 ms lease windows, verifies renewal and competing-owner exclusion, then completes and replays without another invocation.
This is not a destination-deduplication, clock-adjustment, throughput, or deployment-capacity test.

Retain fixtures until review is complete.
When cleanup is authorized, verify the stored run label and exact IDs before stopping and removing only those containers and their anonymous data volumes.
Do not prune containers, images, networks, or volumes. Do not target the normal PostgreSQL fixture.

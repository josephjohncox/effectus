#!/bin/sh
set -eu
umask 077

: "${PGDATA:?PGDATA is required}"
: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}"
case "${EFFECTUS_CLOCK_OFFSET:-}" in
+24h | -24h) ;;
*)
  echo 'EFFECTUS_CLOCK_OFFSET must be +24h or -24h' >&2
  exit 64
  ;;
esac
if [ -e "$PGDATA/PG_VERSION" ]; then
  echo 'The clock-skew fixture requires an empty, task-owned data directory' >&2
  exit 64
fi

# Initialize with the real clock as the unprivileged database user.
password_file=$(mktemp)
trap 'rm -f "$password_file"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
printf '%s\n' "$POSTGRES_PASSWORD" >"$password_file"
initdb -D "$PGDATA" -U postgres --pwfile="$password_file" \
  --auth-local=trust --auth-host=scram-sha-256
rm -f "$password_file"
trap - EXIT HUP INT TERM
printf '\nhost all all all scram-sha-256\n' >>"$PGDATA/pg_hba.conf"

# Only this PostgreSQL process tree receives the shifted wall clock.
# CLOCK_MONOTONIC and the host clock remain unchanged.
export LD_PRELOAD=/usr/local/lib/faketime/libfaketimeMT.so.1
export FAKETIME="$EFFECTUS_CLOCK_OFFSET"
export FAKETIME_DONT_FAKE_MONOTONIC=1
exec postgres -D "$PGDATA" -c listen_addresses='*'

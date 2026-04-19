#!/bin/sh
set -eu

echo "[demo] waiting for postgres..."
until pg_isready -h db -p 5432 -U escalated >/dev/null 2>&1; do sleep 1; done

echo "[demo] TODO: run migrations (go run .../cmd/migrate) + seed"
echo "[demo] ready"

exec "$@"

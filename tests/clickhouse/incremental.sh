#!/bin/bash
# End-to-end test of the ClickHouse incremental backup (base_backup) against the local stack
# (RustFS + ClickHouse from docker-compose.yml). Run from the repository root:
#   npm run test:clickhouse:incremental
#
# Scenario:
#   1. PERIODICITY=weekly  -> full backup (the base)
#   2. insert more rows
#   3. PERIODICITY=daily   -> incremental against the newest weekly archive
#   4. the incremental archive is smaller than the base and its .backup metadata carries the
#      base reference but NOT the storage secret
#   5. RESTORE from the incremental alone (base resolved by the server) matches the live data
#   6. base periodicity without archives -> falls back to a full backup with a warning
set -euo pipefail
cd "$(dirname "$0")/../.."

ENV_FILE=tests/clickhouse/.env
[ -f "$ENV_FILE" ] || { echo "Missing $ENV_FILE (cp tests/clickhouse/.env.example $ENV_FILE)"; exit 1; }
COMPOSE=(docker compose -f tests/clickhouse/docker-compose.yml --env-file "$ENV_FILE")
set -a
# shellcheck source=/dev/null
. "$ENV_FILE"
set +a
: "${AWS_S3_ENDPOINT_URL:=http://s3:9000}"
LOG_DIR=$(mktemp -d)

step() { echo; echo "==================== $* ===================="; }
run_job() { "${COMPOSE[@]}" run --rm -T "$@" dumpscript; }
ch() { "${COMPOSE[@]}" exec -T clickhouse clickhouse-client --user admin --password admin --query "$1"; }
object_path() { sed -n 's/^Object verified: s3:\/\/[^/]*\/\(.*\) (\([0-9]*\) bytes)$/\1 \2/p' "$1" | head -n 1; }

step "stack up"
"${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
"${COMPOSE[@]}" build dumpscript
"${COMPOSE[@]}" up -d s3 clickhouse
"${COMPOSE[@]}" run --rm -T --entrypoint bash dumpscript -c \
  'rclone mkdir ":s3:$S3_BUCKET" --config="" --s3-provider=Other --s3-endpoint="$AWS_S3_ENDPOINT_URL" --s3-force-path-style=true --s3-access-key-id="$AWS_ACCESS_KEY_ID" --s3-secret-access-key="$AWS_SECRET_ACCESS_KEY"'

step "1. weekly = full (base)"
run_job -e PERIODICITY=weekly -e CLICKHOUSE_BASE_BACKUP_PERIODICITY=weekly | tee "$LOG_DIR/full.log"
grep -q "Backup mode: full" "$LOG_DIR/full.log"
read -r FULL_PATH FULL_SIZE <<< "$(object_path "$LOG_DIR/full.log")"
[ -n "$FULL_PATH" ] && echo "base: $FULL_PATH ($FULL_SIZE bytes)"

step "2. insert rows after the base"
ch "INSERT INTO demo.events SELECT toDate('2026-06-01') + intDiv(number, 1000), 1000000 + number, concat('inc-', toString(number)) FROM numbers(20000)"
EXPECTED=$(ch "SELECT count(), sum(event_id) FROM demo.events")
echo "live demo.events: $EXPECTED"

step "3. daily = incremental against the newest weekly"
run_job -e PERIODICITY=daily -e CLICKHOUSE_BASE_BACKUP_PERIODICITY=weekly | tee "$LOG_DIR/inc.log"
grep -q "Backup mode: incremental" "$LOG_DIR/inc.log"
grep -q "Base backup: s3://${S3_BUCKET}/${FULL_PATH}" "$LOG_DIR/inc.log"
grep -q "use_same_s3_credentials_for_base_backup = 1" "$LOG_DIR/inc.log"
if grep -q "$AWS_SECRET_ACCESS_KEY" "$LOG_DIR/inc.log"; then echo "FAIL: storage secret leaked into the job log"; exit 1; fi
read -r INC_PATH INC_SIZE <<< "$(object_path "$LOG_DIR/inc.log")"
echo "incremental: $INC_PATH ($INC_SIZE bytes)"
[ "$INC_SIZE" -lt "$FULL_SIZE" ] || { echo "FAIL: incremental ($INC_SIZE) is not smaller than the base ($FULL_SIZE)"; exit 1; }

step "4. .backup metadata of the incremental: base reference present, secret absent"
"${COMPOSE[@]}" run --rm -T --entrypoint bash -e INC_PATH="$INC_PATH" dumpscript -c '
  set -e
  apk add --no-cache zstd tar >/dev/null 2>&1
  rclone copyto ":s3:$S3_BUCKET/$INC_PATH" /tmp/inc.tar.zst --config="" --s3-provider=Other --s3-endpoint="$AWS_S3_ENDPOINT_URL" --s3-force-path-style=true --s3-access-key-id="$AWS_ACCESS_KEY_ID" --s3-secret-access-key="$AWS_SECRET_ACCESS_KEY"
  zstd -dc /tmp/inc.tar.zst | tar -xO .backup' > "$LOG_DIR/inc.backup"
grep -q "<base_backup>" "$LOG_DIR/inc.backup" || { echo "FAIL: .backup has no <base_backup> element"; cat "$LOG_DIR/inc.backup"; exit 1; }
if grep -q "$AWS_SECRET_ACCESS_KEY" "$LOG_DIR/inc.backup"; then echo "FAIL: storage secret written into the .backup metadata"; exit 1; fi
echo "base_backup in metadata: $(sed -n 's/.*<base_backup>\(.*\)<\/base_backup>.*/\1/p' "$LOG_DIR/inc.backup")"

step "5. RESTORE from the incremental only"
ch "DROP DATABASE IF EXISTS demo_restored SYNC"
ch "RESTORE DATABASE demo AS demo_restored FROM S3('${AWS_S3_ENDPOINT_URL}/${S3_BUCKET}/${INC_PATH}', '${AWS_ACCESS_KEY_ID}', '${AWS_SECRET_ACCESS_KEY}') SETTINGS use_same_s3_credentials_for_base_backup = 1" >/dev/null
RESTORED=$(ch "SELECT count(), sum(event_id) FROM demo_restored.events")
echo "restored demo_restored.events: $RESTORED"
[ "$RESTORED" = "$EXPECTED" ] || { echo "FAIL: restored data differs from live data"; exit 1; }

step "6. base periodicity without archives -> full with warning"
run_job -e PERIODICITY=daily -e CLICKHOUSE_BASE_BACKUP_PERIODICITY=monthly | tee "$LOG_DIR/fallback.log"
grep -q "No base backup found" "$LOG_DIR/fallback.log"
grep -q "Backup mode: full" "$LOG_DIR/fallback.log"

echo
echo "ALL INCREMENTAL CHECKS PASSED (logs in $LOG_DIR)"

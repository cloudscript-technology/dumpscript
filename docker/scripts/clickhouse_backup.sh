#!/bin/bash
set -e
set -o pipefail

# ClickHouse native backup (server-side)
#
# Runs `BACKUP ... TO S3(...)` / `BACKUP ... TO AzureBlobStorage(...)` ON THE SERVER,
# driven over the HTTP interface with curl. The data goes straight from the ClickHouse
# server to the bucket/container: no local file, no client binary, no /dumpscript volume.
#
# Called by dump_db_to_s3.sh when DB_TYPE=clickhouse. Uses the same environment as the
# other database types (DB_*, STORAGE_BACKEND, AWS_*/S3_*, AZURE_STORAGE_*, PERIODICITY,
# DUMP_OPTIONS, SLACK_*), plus:
#
#   DB_PORT                             HTTP port (default: 8123, or 8443 when CLICKHOUSE_SECURE=true)
#   CLICKHOUSE_SECURE                   "true" to use https:// (default: false)
#   CLICKHOUSE_CA_CERT                  path to a CA bundle for https (optional)
#   CLICKHOUSE_ARCHIVE_FORMAT           tar | tar.gz | tar.zst (default: tar.zst)
#   CLICKHOUSE_EXCLUDE_DATABASES        comma-separated list skipped on full-instance backups
#                                       (default: system,information_schema,INFORMATION_SCHEMA)
#   CLICKHOUSE_BACKUP_ACCESS_ENTITIES   "true" to include users/roles/grants/quotas/functions/
#                                       named collections on full-instance backups (default: true)
#   CLICKHOUSE_BACKUP_TIMEOUT           seconds to wait for the backup (default: 21600 = 6h)
#   CLICKHOUSE_BACKUP_POLL_INTERVAL     seconds between system.backups polls (default: 15)
#   CLICKHOUSE_USE_SERVER_CREDENTIALS   "true" to emit S3('<url>') without keys and let the server
#                                       authenticate with its own configuration (default: false)
#   DUMP_OPTIONS                        appended to the BACKUP `SETTINGS` clause
#                                       (e.g. "allow_s3_native_copy=0, deduplicate_files=1")
#
# Requirements on the server side:
#   - ClickHouse >= 24.3 (tar archives as backup destination)
#   - user with `BACKUP ON *.*` (+ `SHOW`) and the source grant for the destination
#     (`S3 ON *.*` or `READ, WRITE ON S3`; `AZURE ON *.*` for Azure)
#   - network egress from the ClickHouse pods to the storage endpoint

# ---------------------------------------------------------------------------
# Notifications (same contract as dump_db_to_s3.sh)
# ---------------------------------------------------------------------------
notify_failure() {
    local error_msg="$1"
    local context="$2"
    if [ -f "/usr/local/bin/notify_slack.sh" ]; then
        /usr/local/bin/notify_slack.sh failure "$error_msg" "$context" || true
        export NOTIFICATION_SENT=true
        touch "${NOTIFY_MARKER:-/tmp/dumpscript.notified}" 2>/dev/null || true
    fi
}

notify_success() {
    local remote_path="$1"
    local size="$2"
    if [ -f "/usr/local/bin/notify_slack.sh" ]; then
        /usr/local/bin/notify_slack.sh success "$remote_path" "$size" || true
    fi
}

fail() {
    local error_msg="$1"
    local context="$2"
    echo "Error: $error_msg"
    notify_failure "$error_msg" "$context"
    exit 1
}

# ---------------------------------------------------------------------------
# Storage utilities (bucket/prefix/listing via rclone)
# ---------------------------------------------------------------------------
if [ -f "/usr/local/bin/storage_utils.sh" ]; then
    . /usr/local/bin/storage_utils.sh
elif [ -f "$(dirname "$0")/storage_utils.sh" ]; then
    . "$(dirname "$0")/storage_utils.sh"
else
    fail "Storage utilities not found" "Missing storage_utils.sh script"
fi

# ---------------------------------------------------------------------------
# Input validation
# ---------------------------------------------------------------------------
[ "$DB_TYPE" = "clickhouse" ] || fail "clickhouse_backup.sh called with DB_TYPE=$DB_TYPE" "Invalid database type configuration"
[ -n "$DB_HOST" ] || fail "DB_HOST must be specified" "Configuration validation failed"
[ -n "$DB_USER" ] || fail "DB_USER must be specified" "Configuration validation failed"
[ -n "$PERIODICITY" ] || fail "PERIODICITY must be specified (e.g., daily, weekly, monthly, yearly)" "Configuration validation failed"

if ! storage_validate_config; then
    fail "Storage configuration is invalid" "Check STORAGE_BACKEND and the backend variables"
fi

# Identifiers: refuse anything that is not a plain database name (also blocks SQL injection via env)
_ident_re='^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$'
if [ -n "${DB_NAME:-}" ] && ! [[ "$DB_NAME" =~ $_ident_re ]]; then
    fail "DB_NAME contains unsupported characters (allowed: letters, digits, '_', '.', '-')" "Configuration validation failed"
fi
case "${DB_USER}${DB_PASSWORD:-}" in *$'\n'*|*$'\r'*) fail "DB_USER/DB_PASSWORD must not contain line breaks" "Configuration validation failed" ;; esac

CLICKHOUSE_SECURE="${CLICKHOUSE_SECURE:-false}"
CLICKHOUSE_ARCHIVE_FORMAT="${CLICKHOUSE_ARCHIVE_FORMAT:-tar.zst}"
CLICKHOUSE_EXCLUDE_DATABASES="${CLICKHOUSE_EXCLUDE_DATABASES:-system,information_schema,INFORMATION_SCHEMA}"
CLICKHOUSE_BACKUP_ACCESS_ENTITIES="${CLICKHOUSE_BACKUP_ACCESS_ENTITIES:-true}"
CLICKHOUSE_BACKUP_TIMEOUT="${CLICKHOUSE_BACKUP_TIMEOUT:-21600}"
CLICKHOUSE_BACKUP_POLL_INTERVAL="${CLICKHOUSE_BACKUP_POLL_INTERVAL:-15}"
CLICKHOUSE_USE_SERVER_CREDENTIALS="${CLICKHOUSE_USE_SERVER_CREDENTIALS:-false}"

case "$CLICKHOUSE_ARCHIVE_FORMAT" in
    tar|tar.gz|tar.zst) ;;
    *) fail "CLICKHOUSE_ARCHIVE_FORMAT must be 'tar', 'tar.gz' or 'tar.zst', received: $CLICKHOUSE_ARCHIVE_FORMAT" "Invalid archive format (zip is not supported for S3/Azure backups)" ;;
esac

if [ "$CLICKHOUSE_SECURE" = "true" ]; then
    CH_SCHEME="https"
    CH_PORT="${DB_PORT:-8443}"
else
    CH_SCHEME="http"
    CH_PORT="${DB_PORT:-8123}"
fi
CH_URL="${CH_SCHEME}://${DB_HOST}:${CH_PORT}/"

# ---------------------------------------------------------------------------
# HTTP helper: credentials go in headers via a private curl config file, never in
# the URL, the command line or the logs.
# ---------------------------------------------------------------------------
CURL_CFG=$(mktemp)
CURL_OUT=$(mktemp)
chmod 600 "$CURL_CFG" "$CURL_OUT"
cleanup_tmp() { rm -f "$CURL_CFG" "$CURL_OUT"; }
trap cleanup_tmp EXIT

# curl config syntax: values are double-quoted, so escape backslash and double quote
curl_cfg_escape() { local v="$1" bs='\' dq='"'; v="${v//"$bs"/$bs$bs}"; v="${v//"$dq"/$bs$dq}"; printf '%s' "$v"; }
{
    printf 'header = "X-ClickHouse-User: %s"\n' "$(curl_cfg_escape "$DB_USER")"
    printf 'header = "X-ClickHouse-Key: %s"\n' "$(curl_cfg_escape "${DB_PASSWORD:-}")"
    printf 'header = "X-ClickHouse-Format: TSVRaw"\n'
    printf 'silent\nshow-error\n'
    printf 'connect-timeout = 15\n'
    printf 'max-time = 600\n'
    if [ "$CLICKHOUSE_SECURE" = "true" ] && [ -n "${CLICKHOUSE_CA_CERT:-}" ]; then
        printf 'cacert = "%s"\n' "$CLICKHOUSE_CA_CERT"
    fi
} > "$CURL_CFG"

# Replace every known secret value by *** before anything reaches the log (server errors may echo the
# rejected statement, which carries the storage credentials)
mask_secrets() {
    local out="$1" secret
    for secret in "${AWS_SECRET_ACCESS_KEY:-}" "${AWS_ACCESS_KEY_ID:-}" "${AWS_SESSION_TOKEN:-}" \
                  "${AZURE_STORAGE_KEY:-}" "${AZURE_STORAGE_SAS_TOKEN:-}" "${DB_PASSWORD:-}"; do
        [ -n "$secret" ] && out="${out//"$secret"/***}"
    done
    printf '%s' "$out" | sed -E "s/(S3\('[^']*',)[^)]*\)/\1 '***', '***')/g; s/(AccountKey|SharedAccessSignature)=[^;']*/\1=***/g"
}

# ch_query <sql>  -> prints the response body; returns 1 on HTTP/transport error
ch_query() {
    local sql="$1"
    local http_code
    : > "$CURL_OUT"
    http_code=$(curl -K "$CURL_CFG" -X POST --data-binary "$sql" -o "$CURL_OUT" -w '%{http_code}' "$CH_URL" 2>>"$CURL_OUT") || true
    if [ "$http_code" != "200" ]; then
        echo "[clickhouse] HTTP $http_code from server:" >&2
        mask_secrets "$(head -c 4000 "$CURL_OUT")" >&2
        echo >&2
        return 1
    fi
    cat "$CURL_OUT"
}

# Escape a value for use inside a single-quoted SQL string literal
sql_str() {
    local v="$1"
    local bs='\' q="'"
    v="${v//"$bs"/$bs$bs}"
    v="${v//"$q"/$bs$q}"
    printf "'%s'" "$v"
}

# Quote an identifier with backticks
sql_ident() {
    local v="$1"
    local bs='\' bt='`'
    v="${v//"$bs"/$bs$bs}"
    v="${v//"$bt"/$bs$bt}"
    printf '`%s`' "$v"
}

# ---------------------------------------------------------------------------
# Pre-flight: connectivity, credentials and server version
# ---------------------------------------------------------------------------
echo "=== ClickHouse native backup ==="
echo "[DEBUG] Server: $CH_URL (user: $DB_USER)"
echo "[DEBUG] DB_NAME: ${DB_NAME:-<all databases>}"
echo "[DEBUG] STORAGE_BACKEND: $(storage_get_backend)"
echo "[DEBUG] Container/Bucket: $(storage_get_container)"
echo "[DEBUG] Prefix: $(storage_get_prefix)"
echo "[DEBUG] PERIODICITY: $PERIODICITY"
echo "[DEBUG] Archive format: $CLICKHOUSE_ARCHIVE_FORMAT"

if ! CH_VERSION=$(ch_query "SELECT version()"); then
    fail "Unable to reach ClickHouse at $CH_URL" "Check DB_HOST/DB_PORT, credentials and network policies (HTTP interface required)"
fi
CH_VERSION=$(echo "$CH_VERSION" | tr -d '[:space:]')
echo "ClickHouse server version: $CH_VERSION"

CH_MAJOR=$(echo "$CH_VERSION" | cut -d. -f1)
CH_MINOR=$(echo "$CH_VERSION" | cut -d. -f2)
if [ "${CH_MAJOR:-0}" -lt 24 ] || { [ "${CH_MAJOR:-0}" -eq 24 ] && [ "${CH_MINOR:-0}" -lt 3 ]; }; then
    fail "ClickHouse $CH_VERSION is too old: archive backups (tar) require >= 24.3" "Upgrade the server or use another backup strategy"
fi

# ---------------------------------------------------------------------------
# Destination
# ---------------------------------------------------------------------------
YEAR=$(date +%Y)
MONTH=$(date +%m)
DAY=$(date +%d)
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="dump_${TIMESTAMP}.${CLICKHOUSE_ARCHIVE_FORMAT}"
STORAGE_PREFIX=$(storage_get_prefix)
REMOTE_DIR="${STORAGE_PREFIX}/${PERIODICITY}/${YEAR}/${MONTH}/${DAY}"
REMOTE_PATH="${REMOTE_DIR}/${BACKUP_FILE}"
DISPLAY_PATH=$(storage_display_path "$REMOTE_PATH")
BACKUP_ID="dumpscript-${TIMESTAMP}-$$"

VERIFY_WITH_RCLONE=true
case "$(storage_get_backend)" in
    s3)
        [ -n "$S3_BUCKET" ] || fail "S3_BUCKET must be specified" "Configuration validation failed"
        if [ -n "${AWS_S3_ENDPOINT_URL:-}" ]; then
            S3_URL="${AWS_S3_ENDPOINT_URL%/}/${S3_BUCKET}/${REMOTE_PATH}"
        else
            S3_URL="https://s3.${AWS_REGION:-us-east-1}.amazonaws.com/${S3_BUCKET}/${REMOTE_PATH}"
        fi
        if [ "$CLICKHOUSE_USE_SERVER_CREDENTIALS" = "true" ]; then
            DESTINATION="S3($(sql_str "$S3_URL"))"
            DESTINATION_MASKED="S3('$S3_URL')"
            [ -n "${AWS_ACCESS_KEY_ID:-}" ] || VERIFY_WITH_RCLONE=false
        else
            if [ -n "${AWS_SESSION_TOKEN:-}" ]; then
                fail "Temporary AWS credentials (AWS_SESSION_TOKEN / IRSA / AWS_ROLE_ARN) are not supported by ClickHouse S3 backups" \
                     "Use a static access key for the clickhouse type, or set CLICKHOUSE_USE_SERVER_CREDENTIALS=true and configure the credentials on the ClickHouse server"
            fi
            [ -n "${AWS_ACCESS_KEY_ID:-}" ] && [ -n "${AWS_SECRET_ACCESS_KEY:-}" ] \
                || fail "AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are required (the ClickHouse server uploads with them)" \
                        "Provide static S3/GCS HMAC credentials or set CLICKHOUSE_USE_SERVER_CREDENTIALS=true"
            DESTINATION="S3($(sql_str "$S3_URL"), $(sql_str "$AWS_ACCESS_KEY_ID"), $(sql_str "$AWS_SECRET_ACCESS_KEY"))"
            DESTINATION_MASKED="S3('$S3_URL', '***', '***')"
        fi
        ;;
    azure)
        AZURE_ENDPOINT_SUFFIX="${AZURE_STORAGE_ENDPOINT_SUFFIX:-core.windows.net}"
        if [ -n "${AZURE_STORAGE_SAS_TOKEN:-}" ]; then
            AZURE_CONN="BlobEndpoint=https://${AZURE_STORAGE_ACCOUNT}.blob.${AZURE_ENDPOINT_SUFFIX};SharedAccessSignature=${AZURE_STORAGE_SAS_TOKEN#\?}"
        else
            AZURE_CONN="DefaultEndpointsProtocol=https;AccountName=${AZURE_STORAGE_ACCOUNT};AccountKey=${AZURE_STORAGE_KEY};EndpointSuffix=${AZURE_ENDPOINT_SUFFIX}"
        fi
        DESTINATION="AzureBlobStorage($(sql_str "$AZURE_CONN"), $(sql_str "$AZURE_STORAGE_CONTAINER"), $(sql_str "$REMOTE_PATH"))"
        DESTINATION_MASKED="AzureBlobStorage('***', '$AZURE_STORAGE_CONTAINER', '$REMOTE_PATH')"
        ;;
    *)
        fail "Unknown STORAGE_BACKEND: $(storage_get_backend)" "Must be 's3' or 'azure'"
        ;;
esac

# ---------------------------------------------------------------------------
# Source
# ---------------------------------------------------------------------------
if [ -n "${DB_NAME:-}" ]; then
    SOURCE="DATABASE $(sql_ident "$DB_NAME")"
else
    EXCEPT_LIST=""
    IFS=',' read -r -a _excluded <<< "$CLICKHOUSE_EXCLUDE_DATABASES"
    for db in "${_excluded[@]}"; do
        db=$(echo "$db" | tr -d '[:space:]')
        [ -z "$db" ] && continue
        [[ "$db" =~ $_ident_re ]] || fail "CLICKHOUSE_EXCLUDE_DATABASES entry has unsupported characters: $db" "Configuration validation failed"
        EXCEPT_LIST="${EXCEPT_LIST:+$EXCEPT_LIST, }$(sql_ident "$db")"
    done
    SOURCE="ALL"
    [ -n "$EXCEPT_LIST" ] && SOURCE="ALL EXCEPT DATABASES ${EXCEPT_LIST}"
    if [ "$CLICKHOUSE_BACKUP_ACCESS_ENTITIES" = "true" ]; then
        # Official pattern: access entities are only included when their system tables are listed explicitly
        SOURCE="TABLE system.users, TABLE system.roles, TABLE system.settings_profiles, TABLE system.row_policies, TABLE system.quotas, TABLE system.functions, TABLE system.named_collections, ${SOURCE}"
    fi
fi

SETTINGS="id = $(sql_str "$BACKUP_ID")"
if [ -n "${DUMP_OPTIONS:-}" ]; then
    # Only `name = value` pairs are accepted here (no arbitrary SQL)
    _kv="[a-z_0-9]+[[:space:]]*=[[:space:]]*'?[A-Za-z0-9_.-]+'?"
    [[ "$DUMP_OPTIONS" =~ ^[[:space:]]*${_kv}([[:space:]]*,[[:space:]]*${_kv})*[[:space:]]*$ ]] \
        || fail "DUMP_OPTIONS for clickhouse must be a comma-separated list of setting=value pairs, received: $(mask_secrets "$DUMP_OPTIONS")" "Configuration validation failed"
    SETTINGS="${SETTINGS}, ${DUMP_OPTIONS}"
fi

BACKUP_SQL="BACKUP ${SOURCE} TO ${DESTINATION} SETTINGS ${SETTINGS} ASYNC"
echo "[DEBUG] Query: BACKUP ${SOURCE} TO ${DESTINATION_MASKED} SETTINGS ${SETTINGS} ASYNC"
echo "Destination path: $DISPLAY_PATH"

# ---------------------------------------------------------------------------
# Submit (ASYNC) and poll system.backups
# ---------------------------------------------------------------------------
echo "Starting ClickHouse backup (id: $BACKUP_ID)..."
if ! SUBMIT_OUT=$(ch_query "$BACKUP_SQL"); then
    fail "ClickHouse rejected the BACKUP statement" "Check the server logs above: grants (BACKUP, S3/AZURE), destination URL/credentials and network egress from the ClickHouse pods to the storage endpoint"
fi
echo "[DEBUG] Submit response: $(echo "$SUBMIT_OUT" | tr '\t' ' ')"

# TSV (escaped) so an empty/multi-line error never shifts the columns; parsed with awk -F'\t'
POLL_SQL="SELECT status, if(error = '', '-', replaceRegexpAll(error, '[\\r\\n\\t]+', ' ')), num_files, total_size, compressed_size FROM system.backups WHERE id = $(sql_str "$BACKUP_ID") FORMAT TSV"
START_TS=$(date +%s)
MISSES=0
STATUS=""
BACKUP_ERROR=""
NUM_FILES=0
TOTAL_SIZE=0
COMPRESSED_SIZE=0

while true; do
    ELAPSED=$(( $(date +%s) - START_TS ))
    if [ "$ELAPSED" -ge "$CLICKHOUSE_BACKUP_TIMEOUT" ]; then
        fail "ClickHouse backup $BACKUP_ID did not finish within ${CLICKHOUSE_BACKUP_TIMEOUT}s (last status: ${STATUS:-unknown})" \
             "The backup keeps running on the server (ASYNC); check system.backups / system.backup_log and raise CLICKHOUSE_BACKUP_TIMEOUT if needed"
    fi

    if ! ROW=$(ch_query "$POLL_SQL"); then
        MISSES=$((MISSES + 1))
        echo "[WARN] Poll failed ($MISSES/5), retrying..."
    elif [ -z "$ROW" ]; then
        MISSES=$((MISSES + 1))
        echo "[WARN] Backup $BACKUP_ID not found in system.backups ($MISSES/5) - server restarted?"
    else
        MISSES=0
        STATUS=$(echo "$ROW" | awk -F'\t' 'NR==1 {print $1}')
        BACKUP_ERROR=$(echo "$ROW" | awk -F'\t' 'NR==1 {print $2}')
        NUM_FILES=$(echo "$ROW" | awk -F'\t' 'NR==1 {print $3}')
        TOTAL_SIZE=$(echo "$ROW" | awk -F'\t' 'NR==1 {print $4}')
        COMPRESSED_SIZE=$(echo "$ROW" | awk -F'\t' 'NR==1 {print $5}')
        [ "$BACKUP_ERROR" = "-" ] && BACKUP_ERROR=""
        echo "[$(date '+%H:%M:%S')] status=$STATUS files=${NUM_FILES:-0} total=${TOTAL_SIZE:-0}B compressed=${COMPRESSED_SIZE:-0}B elapsed=${ELAPSED}s"
        case "$STATUS" in
            BACKUP_CREATED)
                break
                ;;
            BACKUP_FAILED|BACKUP_CANCELLED)
                fail "ClickHouse backup $BACKUP_ID ended with $STATUS: ${BACKUP_ERROR:-no error message}" \
                     "See system.backup_log on the server; common causes: missing S3/AZURE grant, wrong credentials, no egress to the storage endpoint, disk full on the server"
                ;;
        esac
    fi

    if [ "$MISSES" -ge 5 ]; then
        fail "Lost track of ClickHouse backup $BACKUP_ID (no row in system.backups / poll errors)" \
             "The server may have restarted; check system.backup_log and the object at $DISPLAY_PATH before retrying"
    fi
    sleep "$CLICKHOUSE_BACKUP_POLL_INTERVAL"
done

echo "Backup created on the server: $NUM_FILES files, $TOTAL_SIZE bytes (compressed: $COMPRESSED_SIZE bytes)"

# ---------------------------------------------------------------------------
# Verify the object exists in the storage (from the job's point of view)
# ---------------------------------------------------------------------------
OBJECT_SIZE=""
if [ "$VERIFY_WITH_RCLONE" = "true" ]; then
    echo "Verifying object in $(storage_get_backend) storage..."
    LIST_OUT=$(storage_list "${REMOTE_DIR}/" 2>/dev/null || true)
    # storage_list lines: "YYYY-MM-DD HH:MM:SS  SIZE  PATH" -> path is the last field, size the one before
    OBJECT_SIZE=$(echo "$LIST_OUT" | awk -v f="$REMOTE_PATH" '$NF == f {print $(NF-1)}' | head -n 1)
    if [ -z "$OBJECT_SIZE" ]; then
        fail "Backup reported as created but object not found at $DISPLAY_PATH" \
             "Check that the job credentials can list the bucket and that the server wrote to the expected path"
    fi
    if [ "$OBJECT_SIZE" -le 0 ] 2>/dev/null; then
        fail "Backup object is empty: $DISPLAY_PATH" "ClickHouse wrote an empty archive"
    fi
    echo "Object verified: $DISPLAY_PATH ($OBJECT_SIZE bytes)"
else
    echo "[WARN] Skipping storage verification: no credentials in the job (CLICKHOUSE_USE_SERVER_CREDENTIALS=true)"
fi

DUMP_SIZE="${OBJECT_SIZE:-${COMPRESSED_SIZE:-0}}"
echo "Dump completed successfully: $DISPLAY_PATH"
notify_success "$DISPLAY_PATH" "$DUMP_SIZE"

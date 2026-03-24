#!/bin/bash
set -e
set -o pipefail

# Wait for all variables to be set in the environment
# DB_TYPE (mysql, mariadb, postgresql or mongodb), DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME
# STORAGE_BACKEND ("s3" or "azure", default: "s3")
# S3 backend: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_ROLE_ARN, AWS_REGION, S3_BUCKET, S3_PREFIX
# Azure backend: AZURE_STORAGE_ACCOUNT, AZURE_STORAGE_KEY or AZURE_STORAGE_SAS_TOKEN, AZURE_STORAGE_CONTAINER, AZURE_STORAGE_PREFIX
# PERIODICITY, DUMP_OPTIONS (specific options for mysqldump, mariadb-dump or pg_dump)
# SLACK_WEBHOOK_URL (optional) - Slack webhook URL for notifications
# SLACK_CHANNEL (optional) - Specific channel to send messages
# SLACK_USERNAME (optional) - Username that will appear as sender
# SLACK_NOTIFY_SUCCESS (optional) - If "true", also sends success notifications

# Function to notify failures in Slack
notify_failure() {
    local error_msg="$1"
    local context="$2"
    if [ -f "/usr/local/bin/notify_slack.sh" ]; then
        /usr/local/bin/notify_slack.sh failure "$error_msg" "$context" || true
        export NOTIFICATION_SENT=true
    fi
}

# Function to notify success in Slack
notify_success() {
    local s3_path="$1"
    local dump_size="$2"
    if [ -f "/usr/local/bin/notify_slack.sh" ]; then
        /usr/local/bin/notify_slack.sh success "$s3_path" "$dump_size" || true
    fi
}

# Source storage utilities
if [ -f "/usr/local/bin/storage_utils.sh" ]; then
    . /usr/local/bin/storage_utils.sh
elif [ -f "$(dirname "$0")/storage_utils.sh" ]; then
    . "$(dirname "$0")/storage_utils.sh"
else
    echo "Error: Storage utilities not found."
    notify_failure "Storage utilities not found" "Missing storage_utils.sh script"
    exit 1
fi

# Source AWS utilities for role assumption functionality (S3 backend only)
if [ "$(storage_get_backend)" = "s3" ]; then
    if [ -f "/usr/local/bin/aws_role_utils.sh" ]; then
        . /usr/local/bin/aws_role_utils.sh
    elif [ -f "$(dirname "$0")/aws_role_utils.sh" ]; then
        . "$(dirname "$0")/aws_role_utils.sh"
    else
        echo "Warning: AWS role utilities not found. Role assumption may not work."
    fi
fi

if [ -z "$DB_TYPE" ]; then
  error_msg="DB_TYPE must be specified (mysql, mariadb, postgresql or mongodb)"
  echo "Error: $error_msg"
  notify_failure "$error_msg" "Configuration validation failed"
  exit 1
fi

if [ -z "$PERIODICITY" ]; then
  error_msg="PERIODICITY must be specified (e.g., daily, weekly, monthly, yearly)"
  echo "Error: $error_msg"
  notify_failure "$error_msg" "Configuration validation failed"
  exit 1
fi

# Assume AWS role if AWS_ROLE_ARN is defined (initial authentication, S3 backend only)
if [ "$(storage_get_backend)" = "s3" ]; then
  if command -v assume_aws_role >/dev/null 2>&1; then
    if ! assume_aws_role; then
      echo "Warning: Failed to assume AWS role, continuing with existing credentials"
    fi
  else
    echo "Warning: AWS role utilities not available; proceeding without assuming role"
  fi
fi

# Create data structure for S3 path
CURRENT_DATE=$(date +%Y-%m-%d)
YEAR=$(date +%Y)
MONTH=$(date +%m)
DAY=$(date +%d)

# Define dump filename based on DB_TYPE
case "$DB_TYPE" in
  "mysql"|"mariadb"|"postgresql")
    DUMP_EXT="sql"
    ;;
  "mongodb")
    DUMP_EXT="archive"
    ;;
  *)
    error_msg="DB_TYPE must be 'mysql', 'mariadb', 'postgresql' or 'mongodb', received: $DB_TYPE"
    echo "Error: $error_msg"
    notify_failure "$error_msg" "Invalid database type configuration"
    exit 1
    ;;
esac

DUMP_FILE="dump_$(date +%Y%m%d_%H%M%S).${DUMP_EXT}"
DUMP_FILE_GZ="$DUMP_FILE.gz"

echo "[DEBUG] Dump file name: $DUMP_FILE"
echo "[DEBUG] Dump file with compression: $DUMP_FILE_GZ"
echo "[DEBUG] Full path for dump file: $(pwd)/$DUMP_FILE_GZ"
echo "[DEBUG] Absolute path for dump file: /dumpscript/$DUMP_FILE_GZ"

echo "Starting database dump..."
echo "[DEBUG] Current working directory: $(pwd)"
echo "[DEBUG] Workdir /dumpscript exists: $([ -d /dumpscript ] && echo 'YES' || echo 'NO')"
echo "[DEBUG] Workdir /dumpscript permissions: $(ls -ld /dumpscript 2>/dev/null || echo 'Directory not accessible')"
echo "[DEBUG] Available space in /dumpscript: $(df -h /dumpscript 2>/dev/null || echo 'Unable to check space')"
echo "[DEBUG] DB_TYPE: $DB_TYPE"
echo "[DEBUG] DB_HOST: $DB_HOST"
echo "[DEBUG] DB_USER: $DB_USER"
echo "[DEBUG] DB_NAME: $DB_NAME"
echo "[DEBUG] STORAGE_BACKEND: $(storage_get_backend)"
echo "[DEBUG] Container/Bucket: $(storage_get_container)"
echo "[DEBUG] Prefix: $(storage_get_prefix)"
echo "[DEBUG] PERIODICITY: $PERIODICITY"

case "$DB_TYPE" in
  "mysql")
    export MYSQL_PWD="$DB_PASSWORD"
    DUMP_CMD="mysqldump"
    if [ "${MYSQL_VERSION:-}" = "5.7" ]; then
      if command -v mysqldump >/dev/null 2>&1; then
        DUMP_CMD="mysqldump"
      elif command -v mariadb-dump >/dev/null 2>&1; then
        DUMP_CMD="mariadb-dump"
      else
        error_msg="No mysqldump or mariadb-dump available for MySQL 5.7"
        echo "Error: $error_msg"
        notify_failure "$error_msg" "MySQL 5.7 client not found - ensure mysql-client or mariadb-client is installed"
        exit 1
      fi
    fi
    if [ -n "$DB_NAME" ]; then
      echo "[DEBUG] Command: $DUMP_CMD $DUMP_OPTIONS -h $DB_HOST -P ${DB_PORT:-3306} -u $DB_USER $DB_NAME | gzip > $DUMP_FILE_GZ"
      echo "Executing $DUMP_CMD..."
      if ! $DUMP_CMD $DUMP_OPTIONS -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" | gzip > "$DUMP_FILE_GZ"; then
        error_msg="$DUMP_CMD execution failed"
        echo "Error: $error_msg"
        notify_failure "$error_msg" "MySQL dump process failed - check database connectivity and credentials"
        rm -f "$DUMP_FILE_GZ"
        exit 1
      fi
    else
      echo "[DEBUG] Command: $DUMP_CMD $DUMP_OPTIONS --all-databases -h $DB_HOST -P ${DB_PORT:-3306} -u $DB_USER | gzip > $DUMP_FILE_GZ"
      echo "Executing $DUMP_CMD (all databases)..."
      if ! $DUMP_CMD $DUMP_OPTIONS --all-databases -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" | gzip > "$DUMP_FILE_GZ"; then
        error_msg="$DUMP_CMD execution failed (all databases)"
        echo "Error: $error_msg"
        notify_failure "$error_msg" "MySQL full instance dump failed - check permissions and connectivity"
        rm -f "$DUMP_FILE_GZ"
        exit 1
      fi
    fi
    ;;
  "mariadb")
    export MYSQL_PWD="$DB_PASSWORD"
    if [ -n "$DB_NAME" ]; then
      echo "[DEBUG] Command: mariadb-dump $DUMP_OPTIONS -h $DB_HOST -P ${DB_PORT:-3306} -u $DB_USER $DB_NAME | gzip > $DUMP_FILE_GZ"
      echo "Executing mariadb-dump..."
      if ! mariadb-dump $DUMP_OPTIONS -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" | gzip > "$DUMP_FILE_GZ"; then
        error_msg="mariadb-dump execution failed"
        echo "Error: $error_msg"
        notify_failure "$error_msg" "MariaDB dump process failed - check database connectivity and credentials"
        rm -f "$DUMP_FILE_GZ"
        exit 1
      fi
    else
      echo "[DEBUG] Command: mariadb-dump $DUMP_OPTIONS --all-databases -h $DB_HOST -P ${DB_PORT:-3306} -u $DB_USER | gzip > $DUMP_FILE_GZ"
      echo "Executing mariadb-dump (all databases)..."
      if ! mariadb-dump $DUMP_OPTIONS --all-databases -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" | gzip > "$DUMP_FILE_GZ"; then
        error_msg="mariadb-dump execution failed (all databases)"
        echo "Error: $error_msg"
        notify_failure "$error_msg" "MariaDB full instance dump failed - check permissions and connectivity"
        rm -f "$DUMP_FILE_GZ"
        exit 1
      fi
    fi
    ;;
  "postgresql")
    export PGPASSWORD="$DB_PASSWORD"
    if [ -n "$DB_NAME" ]; then
      echo "[DEBUG] Command: pg_dump $DUMP_OPTIONS -h $DB_HOST -p ${DB_PORT:-5432} -U $DB_USER $DB_NAME | gzip > $DUMP_FILE_GZ"
      echo "Executing pg_dump (single database)..."
      if ! pg_dump $DUMP_OPTIONS -h "$DB_HOST" -p "${DB_PORT:-5432}" -U "$DB_USER" "$DB_NAME" | gzip > "$DUMP_FILE_GZ"; then
        error_msg="pg_dump execution failed"
        echo "Error: $error_msg"
        notify_failure "$error_msg" "PostgreSQL dump process failed - check database connectivity and credentials"
        rm -f "$DUMP_FILE_GZ"
        exit 1
      fi
    else
      echo "[DEBUG] Command: pg_dumpall $DUMP_OPTIONS -h $DB_HOST -p ${DB_PORT:-5432} -U $DB_USER | gzip > $DUMP_FILE_GZ"
      echo "Executing pg_dumpall (all databases)..."
      if ! pg_dumpall $DUMP_OPTIONS -h "$DB_HOST" -p "${DB_PORT:-5432}" -U "$DB_USER" | gzip > "$DUMP_FILE_GZ"; then
        error_msg="pg_dumpall execution failed"
        echo "Error: $error_msg"
        notify_failure "$error_msg" "PostgreSQL full instance dump failed - check permissions and connectivity"
        rm -f "$DUMP_FILE_GZ"
        exit 1
      fi
    fi
    ;;
  "mongodb")
    # mongodump outputs to stdout when using --archive; --gzip compresses the output
    if [ -n "$DB_NAME" ]; then
      echo "[DEBUG] Command: mongodump $DUMP_OPTIONS --host $DB_HOST --port ${DB_PORT:-27017} --username $DB_USER --password ****** --db $DB_NAME --archive --gzip > $DUMP_FILE_GZ"
      echo "Executing mongodump..."
      if ! mongodump $DUMP_OPTIONS --host "$DB_HOST" --port "${DB_PORT:-27017}" --username "$DB_USER" --password "$DB_PASSWORD" --db "$DB_NAME" --archive --gzip > "$DUMP_FILE_GZ"; then
        error_msg="mongodump execution failed"
        echo "Error: $error_msg"
        notify_failure "$error_msg" "MongoDB dump process failed - check database connectivity and credentials"
        rm -f "$DUMP_FILE_GZ"
        exit 1
      fi
    else
      echo "[DEBUG] Command: mongodump $DUMP_OPTIONS --host $DB_HOST --port ${DB_PORT:-27017} --username $DB_USER --password ****** --archive --gzip > $DUMP_FILE_GZ"
      echo "Executing mongodump (all databases)..."
      if ! mongodump $DUMP_OPTIONS --host "$DB_HOST" --port "${DB_PORT:-27017}" --username "$DB_USER" --password "$DB_PASSWORD" --archive --gzip > "$DUMP_FILE_GZ"; then
        error_msg="mongodump execution failed (all databases)"
        echo "Error: $error_msg"
        notify_failure "$error_msg" "MongoDB full instance dump failed - check permissions and connectivity"
        rm -f "$DUMP_FILE_GZ"
        exit 1
      fi
    fi
    ;;
  *)
    error_msg="DB_TYPE must be 'mysql', 'mariadb', 'postgresql' or 'mongodb', received: $DB_TYPE"
    echo "Error: $error_msg"
    notify_failure "$error_msg" "Invalid database type configuration"
    exit 1
    ;;
esac

# Check if the file was created and is not empty
echo "[DEBUG] Checking if dump file was created..."
echo "[DEBUG] Looking for file: $DUMP_FILE_GZ"
echo "[DEBUG] File exists check: $([ -f "$DUMP_FILE_GZ" ] && echo 'YES' || echo 'NO')"
echo "[DEBUG] Current directory contents:"
ls -la . 2>/dev/null || echo "Unable to list directory contents"
echo "[DEBUG] Files matching dump pattern:"
ls -la dump_* 2>/dev/null || echo "No files matching dump pattern found"

if [ ! -f "$DUMP_FILE_GZ" ]; then
  error_msg="Dump file was not created"
  echo "Error: $error_msg"
  echo "[DEBUG] Expected file: $DUMP_FILE_GZ"
  echo "[DEBUG] Current working directory: $(pwd)"
  echo "[DEBUG] Directory contents:"
  ls -la . 2>/dev/null || echo "Unable to list directory contents"
  notify_failure "$error_msg" "File system issue - dump file creation failed"
  exit 1
fi

# Check if the file has content
if [ ! -s "$DUMP_FILE_GZ" ]; then
  error_msg="Dump file is empty"
  echo "Error: $error_msg"
  notify_failure "$error_msg" "Database dump resulted in empty file - check database contents and permissions"
  rm -f "$DUMP_FILE_GZ"
  exit 1
fi

# Check if the compressed file is valid
if ! gzip -t "$DUMP_FILE_GZ"; then
  error_msg="Dump file is corrupted"
  echo "Error: $error_msg"
  notify_failure "$error_msg" "File compression failed - dump file is corrupted"
  rm -f "$DUMP_FILE_GZ"
  exit 1
fi

DUMP_SIZE=$(stat -c%s "$DUMP_FILE_GZ")
echo "Dump file created successfully. Size: $DUMP_SIZE bytes"
echo "[DEBUG] File location details:"
echo "[DEBUG] - File name: $DUMP_FILE_GZ"
echo "[DEBUG] - Full path: $(pwd)/$DUMP_FILE_GZ"
echo "[DEBUG] - Absolute path: /dumpscript/$DUMP_FILE_GZ"
echo "[DEBUG] - File size: $DUMP_SIZE bytes"
echo "[DEBUG] - File permissions: $(ls -l "$DUMP_FILE_GZ" 2>/dev/null || echo 'Unable to get file info')"
echo "[DEBUG] - File owner: $(stat -c '%U:%G' "$DUMP_FILE_GZ" 2>/dev/null || echo 'Unable to get owner info')"
echo "[DEBUG] - Directory contents after creation:"
ls -la . 2>/dev/null || echo "Unable to list directory contents"

# Construct the remote path: prefix/periodicity/year/month/day/file
STORAGE_PREFIX=$(storage_get_prefix)
REMOTE_PATH="${STORAGE_PREFIX}/${PERIODICITY}/${YEAR}/${MONTH}/${DAY}/${DUMP_FILE_GZ}"
DISPLAY_PATH=$(storage_display_path "$REMOTE_PATH")
echo "[DEBUG] REMOTE_PATH: $REMOTE_PATH"
echo "[DEBUG] DISPLAY_PATH: $DISPLAY_PATH"

# Upload using storage abstraction with automatic credential refresh
echo "Uploading to $(storage_get_backend) storage..."
echo "Destination path: $DISPLAY_PATH"

# Determine credential refresh function (S3 backend only)
REFRESH_FUNC=""
if [ "$(storage_get_backend)" = "s3" ] && command -v assume_aws_role >/dev/null 2>&1; then
  REFRESH_FUNC="assume_aws_role"
fi

if ! storage_upload "$DUMP_FILE_GZ" "$REMOTE_PATH" "$REFRESH_FUNC"; then
  error_msg="Failed to upload after multiple attempts"
  echo "Error: $error_msg"
  notify_failure "$error_msg" "Storage upload failed even after multiple retries - check storage permissions, network connectivity, and credentials"
  rm -f "$DUMP_FILE_GZ"
  exit 1
fi

echo "Upload completed successfully"

rm "$DUMP_FILE_GZ"

echo "Dump completed successfully: $DISPLAY_PATH"

# Send success notification if configured
notify_success "$DISPLAY_PATH" "$DUMP_SIZE"

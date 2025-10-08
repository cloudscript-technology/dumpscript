#!/bin/sh
set -e
set -o pipefail

# Wait for all variables to be set in the environment
# DB_TYPE (mysql, postgresql or mongodb), DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME
# AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_ROLE_ARN, AWS_REGION, S3_BUCKET, S3_PREFIX, PERIODICITY
# DUMP_OPTIONS (specific options for mysqldump or pg_dump)
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

# Source AWS utilities for role assumption functionality
if [ -f "/usr/local/bin/aws_role_utils.sh" ]; then
    . /usr/local/bin/aws_role_utils.sh
elif [ -f "$(dirname "$0")/aws_role_utils.sh" ]; then
    . "$(dirname "$0")/aws_role_utils.sh"
else
    echo "Warning: AWS role utilities not found. Role assumption may not work."
fi

if [ -z "$DB_TYPE" ]; then
  error_msg="DB_TYPE must be specified (mysql, postgresql or mongodb)"
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

# Assume AWS role if AWS_ROLE_ARN is defined (initial authentication)
if command -v assume_aws_role >/dev/null 2>&1; then
  if ! assume_aws_role; then
    echo "Warning: Failed to assume AWS role, continuing with existing credentials"
  fi
else
  echo "Warning: AWS role utilities not available; proceeding without assuming role"
fi

# Create data structure for S3 path
CURRENT_DATE=$(date +%Y-%m-%d)
YEAR=$(date +%Y)
MONTH=$(date +%m)
DAY=$(date +%d)

# Define dump filename based on DB_TYPE
case "$DB_TYPE" in
  "mysql"|"postgresql")
    DUMP_EXT="sql"
    ;;
  "mongodb")
    DUMP_EXT="archive"
    ;;
  *)
    error_msg="DB_TYPE must be 'mysql', 'postgresql' or 'mongodb', received: $DB_TYPE"
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
echo "[DEBUG] S3_BUCKET: $S3_BUCKET"
echo "[DEBUG] S3_PREFIX: $S3_PREFIX"
echo "[DEBUG] PERIODICITY: $PERIODICITY"

case "$DB_TYPE" in
  "mysql")
    export MYSQL_PWD="$DB_PASSWORD"
    echo "Executing mysqldump..."
    if ! mysqldump $DUMP_OPTIONS -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" | gzip > "$DUMP_FILE_GZ"; then
      error_msg="mysqldump execution failed"
      echo "Error: $error_msg"
      notify_failure "$error_msg" "MySQL dump process failed - check database connectivity and credentials"
      rm -f "$DUMP_FILE_GZ"
      exit 1
    fi
    ;;
  "postgresql")
    export PGPASSWORD="$DB_PASSWORD"
    echo "Executing pg_dump..."
    if ! pg_dump $DUMP_OPTIONS -h "$DB_HOST" -p "${DB_PORT:-5432}" -U "$DB_USER" "$DB_NAME" | gzip > "$DUMP_FILE_GZ"; then
      error_msg="pg_dump execution failed"
      echo "Error: $error_msg"
      notify_failure "$error_msg" "PostgreSQL dump process failed - check database connectivity and credentials"
      rm -f "$DUMP_FILE_GZ"
      exit 1
    fi
    ;;
  "mongodb")
    echo "Executing mongodump..."
    # mongodump outputs to stdout when using --archive; --gzip compresses the output
    if ! mongodump $DUMP_OPTIONS --host "$DB_HOST" --port "${DB_PORT:-27017}" --username "$DB_USER" --password "$DB_PASSWORD" --archive --gzip > "$DUMP_FILE_GZ"; then
      error_msg="mongodump execution failed"
      echo "Error: $error_msg"
      notify_failure "$error_msg" "MongoDB dump process failed - check database connectivity and credentials"
      rm -f "$DUMP_FILE_GZ"
      exit 1
    fi
    ;;
  *)
    error_msg="DB_TYPE must be 'mysql', 'postgresql' or 'mongodb', received: $DB_TYPE"
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

# Construct the S3 path with data structure: bucket/prefix/periodicity/year/month/day/file
S3_PATH="s3://$S3_BUCKET/$S3_PREFIX/$PERIODICITY/$YEAR/$MONTH/$DAY/$DUMP_FILE_GZ"
echo "[DEBUG] S3_PATH: $S3_PATH"

# Upload to S3 using robust upload system with automatic credential refresh
echo "Uploading to S3 with automatic credential refresh..."
echo "Destination path: $S3_PATH"

# Verificar se o script de upload robusto existe
if [ ! -f "/usr/local/bin/s3_upload_with_refresh.sh" ]; then
  error_msg="S3 upload script not found"
  echo "Error: $error_msg"
  notify_failure "$error_msg" "Missing s3_upload_with_refresh.sh script - check container build"
  rm -f "$DUMP_FILE_GZ"
  exit 1
fi

# Use robust upload system with automatic refresh
if ! /usr/local/bin/s3_upload_with_refresh.sh "$DUMP_FILE_GZ" "$S3_PATH" "assume_aws_role"; then
  error_msg="Failed to upload to S3 after multiple attempts with credential refresh"
  echo "Error: $error_msg"
  notify_failure "$error_msg" "S3 upload failed even after multiple retries and credential refresh - check bucket permissions, network connectivity, and AWS credentials"
  rm -f "$DUMP_FILE_GZ"
  exit 1
fi

echo "S3 upload completed successfully with robust retry mechanism"

rm "$DUMP_FILE_GZ"

echo "Dump completed successfully: $S3_PATH"

# Send success notification if configured
notify_success "$S3_PATH" "$DUMP_SIZE"

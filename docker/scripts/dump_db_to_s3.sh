#!/bin/sh
set -e
set -o pipefail

# Wait for all variables to be set in the environment
# DB_TYPE (mysql or postgresql), DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME
# AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_ROLE_ARN, AWS_REGION, S3_BUCKET, S3_PREFIX, PERIODICITY
# DUMP_OPTIONS (specific options for mysqldump or pg_dump)
# SLACK_WEBHOOK_URL (optional) - URL do webhook do Slack para notificações
# SLACK_CHANNEL (optional) - Canal específico para enviar mensagens
# SLACK_USERNAME (optional) - Nome do usuário que aparecerá como remetente
# SLACK_NOTIFY_SUCCESS (optional) - Se "true", envia notificações de sucesso também

# Função para notificar falhas no Slack
notify_failure() {
    local error_msg="$1"
    local context="$2"
    if [ -f "/usr/local/bin/notify_slack.sh" ]; then
        /usr/local/bin/notify_slack.sh failure "$error_msg" "$context" || true
        export NOTIFICATION_SENT=true
    fi
}

# Função para notificar sucesso no Slack
notify_success() {
    local s3_path="$1"
    local dump_size="$2"
    if [ -f "/usr/local/bin/notify_slack.sh" ]; then
        /usr/local/bin/notify_slack.sh success "$s3_path" "$dump_size" || true
    fi
}

# Função para assumir role AWS (reutilizável)
assume_aws_role() {
    if [ -z "$AWS_ROLE_ARN" ]; then
        echo "No AWS_ROLE_ARN defined, skipping role assumption"
        return 0
    fi

    echo "Assuming AWS role: $AWS_ROLE_ARN"
    
    # Check if the service account token is available
    if [ -f "/var/run/secrets/eks.amazonaws.com/serviceaccount/token" ]; then
        echo "Service account token found"
        
        # Read the token from the file
        WEB_IDENTITY_TOKEN=$(cat /var/run/secrets/eks.amazonaws.com/serviceaccount/token)
        if [ -z "$WEB_IDENTITY_TOKEN" ]; then
            error_msg="Service account token is empty"
            echo "Error: $error_msg"
            notify_failure "$error_msg" "AWS authentication failed - IRSA token issue"
            return 1
        fi
        
        export AWS_ROLE_SESSION_NAME="dumpscript-$(date +%s)"
        
        # Assume the role using the service account token
        echo "Assuming role using IRSA..."
        echo "Role ARN: $AWS_ROLE_ARN"
        echo "Role Session Name: $AWS_ROLE_SESSION_NAME"
        
        TEMP_ROLE=$(aws sts assume-role-with-web-identity \
          --role-arn "$AWS_ROLE_ARN" \
          --role-session-name "$AWS_ROLE_SESSION_NAME" \
          --web-identity-token "$WEB_IDENTITY_TOKEN" \
          --query 'Credentials.[AccessKeyId,SecretAccessKey,SessionToken]' \
          --output text)
        
        if [ $? -ne 0 ]; then
            echo "Error assuming role. Attempting to use default credentials."
            return 1
        else
            export AWS_ACCESS_KEY_ID=$(echo $TEMP_ROLE | cut -d' ' -f1)
            export AWS_SECRET_ACCESS_KEY=$(echo $TEMP_ROLE | cut -d' ' -f2)
            export AWS_SESSION_TOKEN=$(echo $TEMP_ROLE | cut -d' ' -f3)
            
            echo "Role assumed successfully!"
            echo "AWS_ACCESS_KEY_ID: ${AWS_ACCESS_KEY_ID:0:10}..."
            return 0
        fi
    else
        echo "Warning: Service account token not found at /var/run/secrets/eks.amazonaws.com/serviceaccount/token"
        echo "Listing available files:"
        find /var/run/secrets -name "*token*" -type f 2>/dev/null || echo "No token found"
        echo "Attempting to use default credentials."
        return 1
    fi
}

if [ -z "$DB_TYPE" ]; then
  error_msg="DB_TYPE must be specified (mysql or postgresql)"
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
assume_aws_role

# Create data structure for S3 path
CURRENT_DATE=$(date +%Y-%m-%d)
YEAR=$(date +%Y)
MONTH=$(date +%m)
DAY=$(date +%d)

DUMP_FILE="dump_$(date +%Y%m%d_%H%M%S).sql"
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
  *)
    error_msg="DB_TYPE must be 'mysql' or 'postgresql', received: $DB_TYPE"
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

# Refresh AWS credentials before S3 upload (in case the dump took a long time and tokens expired)
echo "Refreshing AWS credentials before S3 upload..."
if ! assume_aws_role; then
    echo "Warning: Failed to refresh AWS credentials, attempting S3 upload with existing credentials"
fi

# Upload to S3
echo "Uploading to S3..."
echo "Destination path: $S3_PATH"
if ! aws s3 cp "$DUMP_FILE_GZ" "$S3_PATH"; then
  error_msg="Failed to upload to S3"
  echo "Error: $error_msg"
  
  # If S3 upload fails, try refreshing credentials once more and retry
  echo "S3 upload failed, attempting to refresh credentials and retry..."
  if assume_aws_role; then
    echo "Credentials refreshed, retrying S3 upload..."
    if ! aws s3 cp "$DUMP_FILE_GZ" "$S3_PATH"; then
      error_msg="Failed to upload to S3 after credential refresh"
      echo "Error: $error_msg"
      notify_failure "$error_msg" "S3 upload failed even after credential refresh - check bucket permissions and network connectivity"
      rm -f "$DUMP_FILE_GZ"
      exit 1
    fi
  else
    notify_failure "$error_msg" "S3 upload failed and credential refresh also failed - check AWS credentials, bucket permissions, and network connectivity"
    rm -f "$DUMP_FILE_GZ"
    exit 1
  fi
fi

rm "$DUMP_FILE_GZ"

echo "Dump completed successfully: $S3_PATH"

# Enviar notificação de sucesso se configurado
notify_success "$S3_PATH" "$DUMP_SIZE" 
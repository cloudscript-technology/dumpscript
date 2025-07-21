#!/bin/sh
set -e
set -o pipefail

# Wait for all variables to be set in the environment
# DB_TYPE (mysql or postgresql), DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME
# AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_ROLE_ARN, AWS_REGION, S3_BUCKET, S3_PREFIX, PERIODICITY
# DUMP_OPTIONS (specific options for mysqldump or pg_dump)

if [ -z "$DB_TYPE" ]; then
  echo "Error: DB_TYPE must be specified (mysql or postgresql)"
  exit 1
fi

if [ -z "$PERIODICITY" ]; then
  echo "Error: PERIODICITY must be specified (e.g., daily, weekly, monthly, yearly)"
  exit 1
fi

# Assume AWS role if AWS_ROLE_ARN is defined
if [ -n "$AWS_ROLE_ARN" ]; then
  echo "Assuming AWS role: $AWS_ROLE_ARN"
  
  # Check if the service account token is available
  if [ -f "/var/run/secrets/eks.amazonaws.com/serviceaccount/token" ]; then
    echo "Service account token found"
    
    # Read the token from the file
    WEB_IDENTITY_TOKEN=$(cat /var/run/secrets/eks.amazonaws.com/serviceaccount/token)
    if [ -z "$WEB_IDENTITY_TOKEN" ]; then
      echo "Error: Service account token is empty"
      exit 1
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
    else
      export AWS_ACCESS_KEY_ID=$(echo $TEMP_ROLE | cut -d' ' -f1)
      export AWS_SECRET_ACCESS_KEY=$(echo $TEMP_ROLE | cut -d' ' -f2)
      export AWS_SESSION_TOKEN=$(echo $TEMP_ROLE | cut -d' ' -f3)
      
      echo "Role assumed successfully!"
      echo "AWS_ACCESS_KEY_ID: ${AWS_ACCESS_KEY_ID:0:10}..."
    fi
  else
    echo "Warning: Service account token not found at /var/run/secrets/eks.amazonaws.com/serviceaccount/token"
    echo "Listing available files:"
    find /var/run/secrets -name "*token*" -type f 2>/dev/null || echo "No token found"
    echo "Attempting to use default credentials."
  fi
fi

# Create data structure for S3 path
CURRENT_DATE=$(date +%Y-%m-%d)
YEAR=$(date +%Y)
MONTH=$(date +%m)
DAY=$(date +%d)

DUMP_FILE="dump_$(date +%Y%m%d_%H%M%S).sql"
DUMP_FILE_GZ="$DUMP_FILE.gz"

echo "Starting database dump..."
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
      echo "Error: mysqldump execution failed"
      rm -f "$DUMP_FILE_GZ"
      exit 1
    fi
    ;;
  "postgresql")
    export PGPASSWORD="$DB_PASSWORD"
    echo "Executing pg_dump..."
    if ! pg_dump $DUMP_OPTIONS -h "$DB_HOST" -p "${DB_PORT:-5432}" -U "$DB_USER" "$DB_NAME" | gzip > "$DUMP_FILE_GZ"; then
      echo "Error: pg_dump execution failed"
      rm -f "$DUMP_FILE_GZ"
      exit 1
    fi
    ;;
  *)
    echo "Error: DB_TYPE must be 'mysql' or 'postgresql', received: $DB_TYPE"
    exit 1
    ;;
esac

# Check if the file was created and is not empty
if [ ! -f "$DUMP_FILE_GZ" ]; then
  echo "Error: Dump file was not created"
  exit 1
fi

# Check if the file has content
if [ ! -s "$DUMP_FILE_GZ" ]; then
  echo "Error: Dump file is empty"
  rm -f "$DUMP_FILE_GZ"
  exit 1
fi

# Check if the compressed file is valid
if ! gzip -t "$DUMP_FILE_GZ"; then
  echo "Error: Dump file is corrupted"
  rm -f "$DUMP_FILE_GZ"
  exit 1
fi

DUMP_SIZE=$(stat -c%s "$DUMP_FILE_GZ")
echo "Dump file created successfully. Size: $DUMP_SIZE bytes"

# Construct the S3 path with data structure: bucket/prefix/periodicity/year/month/day/file
S3_PATH="s3://$S3_BUCKET/$S3_PREFIX/$PERIODICITY/$YEAR/$MONTH/$DAY/$DUMP_FILE_GZ"
echo "[DEBUG] S3_PATH: $S3_PATH"

# Upload to S3
echo "Uploading to S3..."
echo "Destination path: $S3_PATH"
if ! aws s3 cp "$DUMP_FILE_GZ" "$S3_PATH"; then
  echo "Error: Failed to upload to S3"
  rm -f "$DUMP_FILE_GZ"
  exit 1
fi

rm "$DUMP_FILE_GZ"

echo "Dump completed successfully: $S3_PATH" 
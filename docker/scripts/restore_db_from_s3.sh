#!/bin/sh
set -e

# Wait for all variables to be set in the environment
# DB_TYPE (mysql or postgresql), DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME
# AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_ROLE_ARN, AWS_REGION, S3_BUCKET, S3_KEY, CREATE_DB

if [ -z "$DB_TYPE" ]; then
  echo "Error: DB_TYPE must be specified (mysql or postgresql)"
  exit 1
fi

export AWS_REGION

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
    
    export AWS_ROLE_SESSION_NAME="dumpscript-restore-$(date +%s)"
    
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
      echo "Error assuming role. Trying to use default credentials."
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
    echo "Trying to use default credentials."
  fi
fi

echo "[DEBUG] DB_TYPE: $DB_TYPE"
echo "[DEBUG] DB_HOST: $DB_HOST"
echo "[DEBUG] DB_USER: $DB_USER"
echo "[DEBUG] DB_NAME: $DB_NAME"
echo "[DEBUG] S3_BUCKET: $S3_BUCKET"
echo "[DEBUG] S3_PREFIX: $S3_PREFIX"
echo "[DEBUG] S3_KEY: $S3_KEY"

aws s3 cp "s3://$S3_BUCKET/$S3_KEY" dump_restore.sql.gz
gunzip -f dump_restore.sql.gz

case "$DB_TYPE" in
  "mysql")
    export MYSQL_PWD="$DB_PASSWORD"
    
    if [ "$CREATE_DB" = "1" ]; then
      echo "Creating MySQL database $DB_NAME..."
      mysql -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" -e "CREATE DATABASE IF NOT EXISTS \`$DB_NAME\`;"
    fi
    
    mysql -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" < dump_restore.sql
    ;;
    
  "postgresql")
    export PGPASSWORD="$DB_PASSWORD"
    
    if [ "$CREATE_DB" = "1" ]; then
      echo "Creating PostgreSQL database $DB_NAME..."
      psql -h "$DB_HOST" -p "${DB_PORT:-5432}" -U "$DB_USER" -d postgres -c "CREATE DATABASE \"$DB_NAME\";" || echo "Database already exists."
    fi
    
    psql -h "$DB_HOST" -p "${DB_PORT:-5432}" -U "$DB_USER" "$DB_NAME" < dump_restore.sql
    ;;
    
  *)
    echo "Error: DB_TYPE must be 'mysql' or 'postgresql', received: $DB_TYPE"
    exit 1
    ;;
esac

rm dump_restore.sql

echo "Restore completed for database $DB_TYPE: $DB_NAME" 
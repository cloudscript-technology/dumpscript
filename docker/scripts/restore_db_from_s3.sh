#!/bin/sh
set -e

# Wait for all variables to be set in the environment
# DB_TYPE (mysql, mariadb, postgresql or mongodb), DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME
# AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_ROLE_ARN, AWS_REGION, S3_BUCKET, S3_KEY, CREATE_DB

if [ -z "$DB_TYPE" ]; then
  echo "Error: DB_TYPE must be specified (mysql, mariadb, postgresql or mongodb)"
  exit 1
fi

export AWS_REGION

# Source AWS utilities for role assumption functionality
if [ -f "/usr/local/bin/aws_role_utils.sh" ]; then
    . /usr/local/bin/aws_role_utils.sh
elif [ -f "$(dirname "$0")/aws_role_utils.sh" ]; then
    . "$(dirname "$0")/aws_role_utils.sh"
else
    echo "Warning: AWS role utilities not found. Role assumption may not work."
fi

# Assume AWS role if AWS_ROLE_ARN is defined (initial authentication)
if command -v assume_aws_role >/dev/null 2>&1; then
    if ! assume_aws_role; then
        echo "Warning: Failed to assume AWS role, continuing with existing credentials"
    fi
else
    echo "Warning: assume_aws_role function not available. Proceeding with default credentials."
fi

echo "[DEBUG] DB_TYPE: $DB_TYPE"
echo "[DEBUG] DB_HOST: $DB_HOST"
echo "[DEBUG] DB_USER: $DB_USER"
echo "[DEBUG] DB_NAME: $DB_NAME"
echo "[DEBUG] S3_BUCKET: $S3_BUCKET"
echo "[DEBUG] S3_PREFIX: $S3_PREFIX"
echo "[DEBUG] S3_KEY: $S3_KEY"

case "$DB_TYPE" in
  "mysql"|"mariadb"|"postgresql")
    RESTORE_FILE_GZ="dump_restore.sql.gz"
    aws s3 cp "s3://$S3_BUCKET/$S3_KEY" "$RESTORE_FILE_GZ"
    gunzip -f "$RESTORE_FILE_GZ"
    ;;
  "mongodb")
    RESTORE_FILE_GZ="dump_restore.archive.gz"
    aws s3 cp "s3://$S3_BUCKET/$S3_KEY" "$RESTORE_FILE_GZ"
    ;;
  *)
    echo "Error: DB_TYPE must be 'mysql', 'postgresql' or 'mongodb', received: $DB_TYPE"
    exit 1
    ;;
esac

case "$DB_TYPE" in
  "mysql")
    export MYSQL_PWD="$DB_PASSWORD"
    # Choose client command (mysql preferred, fallback to mariadb)
    MYSQL_CLIENT_CMD="mysql"
    if ! command -v mysql >/dev/null 2>&1 && command -v mariadb >/dev/null 2>&1; then
      MYSQL_CLIENT_CMD="mariadb"
    elif ! command -v mysql >/dev/null 2>&1 && ! command -v mariadb >/dev/null 2>&1; then
      echo "Error: No MySQL/MariaDB client found (mysql or mariadb)"
      exit 1
    fi

    if [ -n "$DB_NAME" ]; then
      if [ "$CREATE_DB" = "1" ]; then
        echo "Creating MySQL database $DB_NAME..."
        $MYSQL_CLIENT_CMD -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" -e "CREATE DATABASE IF NOT EXISTS \`$DB_NAME\`;"
      fi
      $MYSQL_CLIENT_CMD -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" < dump_restore.sql
    else
      echo "Restoring full MySQL instance (no DB_NAME provided)..."
      $MYSQL_CLIENT_CMD -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" < dump_restore.sql
    fi
    ;;
  "mariadb")
    export MYSQL_PWD="$DB_PASSWORD"
    # Choose client command (mariadb preferred, fallback to mysql)
    MARIADB_CLIENT_CMD="mariadb"
    if ! command -v mariadb >/dev/null 2>&1 && command -v mysql >/dev/null 2>&1; then
      MARIADB_CLIENT_CMD="mysql"
    elif ! command -v mariadb >/dev/null 2>&1 && ! command -v mysql >/dev/null 2>&1; then
      echo "Error: No MariaDB/MySQL client found (mariadb or mysql)"
      exit 1
    fi

    if [ -n "$DB_NAME" ]; then
      if [ "$CREATE_DB" = "1" ]; then
        echo "Creating MariaDB database $DB_NAME..."
        $MARIADB_CLIENT_CMD -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" -e "CREATE DATABASE IF NOT EXISTS \`$DB_NAME\`;"
      fi
      $MARIADB_CLIENT_CMD -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" < dump_restore.sql
    else
      echo "Restoring full MariaDB instance (no DB_NAME provided)..."
      $MARIADB_CLIENT_CMD -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" < dump_restore.sql
    fi
    ;;
    
  "postgresql")
    export PGPASSWORD="$DB_PASSWORD"
    if [ -n "$DB_NAME" ]; then
      if [ "$CREATE_DB" = "1" ]; then
        echo "Creating PostgreSQL database $DB_NAME..."
        psql -h "$DB_HOST" -p "${DB_PORT:-5432}" -U "$DB_USER" -d postgres -c "CREATE DATABASE \"$DB_NAME\";" || echo "Database already exists."
      fi
      psql -h "$DB_HOST" -p "${DB_PORT:-5432}" -U "$DB_USER" "$DB_NAME" < dump_restore.sql
    else
      echo "Restoring full PostgreSQL instance (pg_dumpall)..."
      psql -h "$DB_HOST" -p "${DB_PORT:-5432}" -U "$DB_USER" -d postgres < dump_restore.sql
    fi
    ;;
  "mongodb")
    echo "Restoring MongoDB archive..."
    # mongorestore can read gzipped archive when --gzip is provided
    if [ -n "$DB_NAME" ]; then
      if ! mongorestore --host "$DB_HOST" --port "${DB_PORT:-27017}" --username "$DB_USER" --password "$DB_PASSWORD" --db "$DB_NAME" --archive --gzip < "$RESTORE_FILE_GZ"; then
        echo "Error: mongorestore failed"
        exit 1
      fi
    else
      if ! mongorestore --host "$DB_HOST" --port "${DB_PORT:-27017}" --username "$DB_USER" --password "$DB_PASSWORD" --archive --gzip < "$RESTORE_FILE_GZ"; then
        echo "Error: mongorestore failed (full instance)"
        exit 1
      fi
    fi
    ;;
  
  *)
    echo "Error: DB_TYPE must be 'mysql', 'mariadb', 'postgresql' or 'mongodb', received: $DB_TYPE"
    exit 1
    ;;
esac

if [ "$DB_TYPE" = "mysql" ] || [ "$DB_TYPE" = "mariadb" ] || [ "$DB_TYPE" = "postgresql" ]; then
  rm -f dump_restore.sql
else
  rm -f "$RESTORE_FILE_GZ"
fi

echo "Restore completed for database $DB_TYPE: $DB_NAME"

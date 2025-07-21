#!/bin/bash
set -e

# Entrypoint for the dump container
# Installs necessary clients dynamically and executes the dump

echo "=== DumpScript Container Starting ==="
echo "DB_TYPE: $DB_TYPE"
echo "[DEBUG] DB_TYPE: $DB_TYPE"
if [ "$DB_TYPE" = "postgresql" ]; then
  echo "[DEBUG] POSTGRES_VERSION: ${POSTGRES_VERSION:-16}"
elif [ "$DB_TYPE" = "mysql" ]; then
  echo "[DEBUG] MYSQL_VERSION: ${MYSQL_VERSION:-10.11}"
fi

# Validate required variables
if [ -z "$DB_TYPE" ]; then
    echo "Error: DB_TYPE must be specified (postgresql or mysql)"
    exit 1
fi

# Remove old backups that are outside the retention period
echo "Removing old backups..."
/usr/local/bin/remove_old_backups.sh

# Install database clients
echo "Installing database clients..."
/usr/local/bin/install_db_clients.sh

# Check if clients were installed correctly
case "$DB_TYPE" in
    "postgresql")
        if ! command -v pg_dump &> /dev/null; then
            echo "Error: pg_dump not found after installation"
            exit 1
        fi
        echo "PostgreSQL client version: $(pg_dump --version)"
        ;;
    "mysql")
        if ! command -v mysqldump &> /dev/null; then
            echo "Error: mysqldump not found after installation"
            exit 1
        fi
        echo "MySQL client version: $(mysqldump --version)"
        ;;
esac

echo "Database clients installed successfully!"
echo "=== Starting Database Dump ==="

# Execute the dump script
exec /usr/local/bin/dump_db_to_s3.sh 
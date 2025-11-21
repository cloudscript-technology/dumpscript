#!/bin/bash
set -e

# Entrypoint for the restore container
# Installs necessary clients dynamically and executes the restore

echo "=== DumpScript Restore Container Starting ==="
echo "DB_TYPE: $DB_TYPE"
echo "POSTGRES_VERSION: ${POSTGRES_VERSION:-16}"
echo "MYSQL_VERSION: ${MYSQL_VERSION:-8.0}"
echo "MongoDB tools: will be installed when DB_TYPE=mongodb"
echo "[DEBUG] DB_TYPE: $DB_TYPE"
if [ "$DB_TYPE" = "postgresql" ]; then
  echo "[DEBUG] POSTGRES_VERSION: ${POSTGRES_VERSION:-16}"
elif [ "$DB_TYPE" = "mysql" ]; then
  echo "[DEBUG] MYSQL_VERSION: ${MYSQL_VERSION:-8.0}"
elif [ "$DB_TYPE" = "mariadb" ]; then
  echo "[DEBUG] MARIADB_VERSION: ${MARIADB_VERSION:-11.4}"
elif [ "$DB_TYPE" = "mongodb" ]; then
  echo "[DEBUG] MongoDB tools will be installed"
fi

case "$DB_TYPE" in
    "postgresql")
        echo "POSTGRES_VERSION: ${POSTGRES_VERSION:-16}"
        ;;
    "mysql")
        echo "MYSQL_VERSION: ${MYSQL_VERSION:-8.0}"
        ;;
    "mariadb")
        echo "MARIADB_VERSION: ${MARIADB_VERSION:-11.4}"
        ;;
    "mongodb")
        echo "MongoDB tools will be installed"
        ;;
esac

# Validate required variables
if [ -z "$DB_TYPE" ]; then
    echo "Error: DB_TYPE must be specified (postgresql, mysql, mariadb or mongodb)"
    exit 1
fi

# Install database clients
echo "Installing database clients..."
/usr/local/bin/install_db_clients.sh

# Verify if clients were installed correctly
case "$DB_TYPE" in
    "postgresql")
        if ! command -v psql &> /dev/null; then
            echo "Error: psql not found after installation"
            exit 1
        fi
        echo "PostgreSQL client version: $(psql --version)"
        ;;
    "mysql")
        if ! command -v mysql &> /dev/null; then
            echo "Error: mysql not found after installation"
            exit 1
        fi
        echo "MySQL client version: $(mysql --version)"
        ;;
    "mariadb")
        if ! command -v mysql &> /dev/null; then
            echo "Error: mysql not found after installation"
            exit 1
        fi
        echo "MariaDB client version: $(mysql --version)"
        ;;
    "mongodb")
        if ! command -v mongorestore &> /dev/null; then
            echo "Error: mongorestore not found after installation"
            exit 1
        fi
        echo "MongoDB tools version: $(mongorestore --version | head -n 1)"
        ;;
esac

echo "Database clients installed successfully!"
echo "=== Starting Database Restore ==="

# Execute the restore script
exec /usr/local/bin/restore_db_from_s3.sh

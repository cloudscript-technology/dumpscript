#!/bin/bash
set -e

# Entrypoint for the dump container
# Installs necessary clients dynamically and executes the dump

# Função para notificar falhas no Slack
notify_failure() {
    local error_msg="$1"
    local context="$2"
    if [ -f "/usr/local/bin/notify_slack.sh" ]; then
        /usr/local/bin/notify_slack.sh failure "$error_msg" "$context" || true
    fi
}

# Trap para capturar falhas não tratadas
cleanup_on_failure() {
    local exit_code=$?
    if [ $exit_code -ne 0 ]; then
        notify_failure "Dump process failed with exit code $exit_code" "Unexpected error during dump execution"
    fi
    exit $exit_code
}
trap cleanup_on_failure EXIT

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
    error_msg="DB_TYPE must be specified (postgresql or mysql)"
    echo "Error: $error_msg"
    notify_failure "$error_msg" "Configuration validation failed in entrypoint"
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
            error_msg="pg_dump not found after installation"
            echo "Error: $error_msg"
            notify_failure "$error_msg" "PostgreSQL client installation failed"
            exit 1
        fi
        echo "PostgreSQL client version: $(pg_dump --version)"
        ;;
    "mysql")
        if ! command -v mysqldump &> /dev/null; then
            error_msg="mysqldump not found after installation"
            echo "Error: $error_msg"
            notify_failure "$error_msg" "MySQL client installation failed"
            exit 1
        fi
        echo "MySQL client version: $(mysqldump --version)"
        ;;
esac

echo "Database clients installed successfully!"
echo "=== Starting Database Dump ==="

# Execute the dump script
exec /usr/local/bin/dump_db_to_s3.sh 
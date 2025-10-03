#!/bin/bash
set -e

# Script to install database clients dynamically
# Based on environment variables:
# - POSTGRES_VERSION: PostgreSQL client version (13, 14, 15, 16, 17)
# - MYSQL_VERSION: MySQL/MariaDB client version (10.11, 11.4)
# - MongoDB tools: installed without version pinning (mongodump, mongorestore)

echo "Installing database clients based on environment variables..."
echo "[DEBUG] DB_TYPE: $DB_TYPE"
case "$DB_TYPE" in
    "postgresql")
        echo "POSTGRES_VERSION: ${POSTGRES_VERSION:-16}"
        ;;
    "mysql")
        echo "MYSQL_VERSION: ${MYSQL_VERSION:-10.11}"
        ;;
    "mongodb")
        echo "MongoDB tools will be installed (mongodump/mongorestore)"
        ;;
esac

# Function to install PostgreSQL client
install_postgresql_client() {
    local version=$1
    echo "Installing PostgreSQL client version $version..."
    
    case "$version" in
        "13"|"14"|"15"|"16"|"17")
            apk add --no-cache postgresql${version}-client
            echo "PostgreSQL client $version installed successfully"
            ;;
        *)
            echo "Error: Unsupported PostgreSQL version: $version"
            echo "Supported versions: 13, 14, 15, 16, 17"
            exit 1
            ;;
    esac
}

# Function to install MySQL/MariaDB client
install_mysql_client() {
    local version=$1
    echo "Installing MySQL/MariaDB client version $version..."
    
    case "$version" in
        "10.11"|"11.4")
            apk add --no-cache mariadb-client~=${version}
            echo "MariaDB client $version installed successfully"
            ;;
        "8.0")
            apk add --no-cache mysql-client
            echo "MySQL client installed successfully"
            ;;
        *)
            echo "Error: Unsupported MySQL/MariaDB version: $version"
            echo "Supported versions: 10.11, 11.4, 8.0"
            exit 1
            ;;
    esac
}

# Function to install MongoDB database tools
install_mongodb_tools() {
    echo "Installing MongoDB database tools (mongodump, mongorestore)..."
    # Alpine provides mongodb-tools package containing mongodump/mongorestore
    apk add --no-cache mongodb-tools
    echo "MongoDB tools installed successfully"
}

# Install clients based on database type
case "$DB_TYPE" in
    "postgresql")
        POSTGRES_VERSION=${POSTGRES_VERSION:-16}
        install_postgresql_client "$POSTGRES_VERSION"
        ;;
    "mysql")
        MYSQL_VERSION=${MYSQL_VERSION:-10.11}
        install_mysql_client "$MYSQL_VERSION"
        ;;
    "mongodb")
        install_mongodb_tools
        ;;
    *)
        echo "Error: DB_TYPE must be 'postgresql', 'mysql' or 'mongodb', received: $DB_TYPE"
        exit 1
        ;;
esac

echo "Database client installation completed!"
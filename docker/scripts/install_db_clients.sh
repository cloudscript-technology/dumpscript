#!/bin/bash
set -e

# Script para instalar clientes de banco de dados dinamicamente
# Baseado nas variáveis de ambiente:
# - POSTGRES_VERSION: versão do cliente PostgreSQL (13, 14, 15, 16, 17)
# - MYSQL_VERSION: versão do cliente MySQL/MariaDB (10.11, 11.4)

echo "Installing database clients based on environment variables..."

# Função para instalar cliente PostgreSQL
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

# Função para instalar cliente MySQL/MariaDB
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

# Instalar clientes baseado no tipo de banco
case "$DB_TYPE" in
    "postgresql")
        POSTGRES_VERSION=${POSTGRES_VERSION:-16}
        install_postgresql_client "$POSTGRES_VERSION"
        ;;
    "mysql")
        MYSQL_VERSION=${MYSQL_VERSION:-10.11}
        install_mysql_client "$MYSQL_VERSION"
        ;;
    *)
        echo "Error: DB_TYPE must be 'postgresql' or 'mysql', received: $DB_TYPE"
        exit 1
        ;;
esac

echo "Database client installation completed!" 
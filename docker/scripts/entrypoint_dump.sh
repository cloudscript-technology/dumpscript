#!/bin/bash
set -e

# Entrypoint para o container de dump
# Instala os clientes necessários dinamicamente e executa o dump

echo "=== DumpScript Container Starting ==="
echo "DB_TYPE: $DB_TYPE"
echo "POSTGRES_VERSION: ${POSTGRES_VERSION:-16}"
echo "MYSQL_VERSION: ${MYSQL_VERSION:-10.11}"

# Validar variáveis obrigatórias
if [ -z "$DB_TYPE" ]; then
    echo "Error: DB_TYPE must be specified (postgresql or mysql)"
    exit 1
fi

# Instalar clientes de banco de dados
echo "Installing database clients..."
/usr/local/bin/install_db_clients.sh

# Verificar se os clientes foram instalados corretamente
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

# Executar o script de dump
exec /usr/local/bin/dump_db_to_s3.sh 
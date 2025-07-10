#!/bin/bash
set -e

# Entrypoint para o container de restore
# Instala os clientes necessários dinamicamente e executa o restore

echo "=== DumpScript Restore Container Starting ==="
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
esac

echo "Database clients installed successfully!"
echo "=== Starting Database Restore ==="

# Executar o script de restore
exec /usr/local/bin/restore_db_from_s3.sh 
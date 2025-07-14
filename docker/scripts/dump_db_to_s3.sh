#!/bin/sh
set -e
set -o pipefail

# Espera que todas as variáveis estejam setadas no ambiente
# DB_TYPE (mysql ou postgresql), DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME
# AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_ROLE_ARN, AWS_REGION, S3_BUCKET, S3_PREFIX
# DUMP_OPTIONS (opções específicas para mysqldump ou pg_dump)

if [ -z "$DB_TYPE" ]; then
  echo "Erro: DB_TYPE deve ser especificado (mysql ou postgresql)"
  exit 1
fi

# Assumir role da AWS se AWS_ROLE_ARN estiver definido
if [ -n "$AWS_ROLE_ARN" ]; then
  echo "Assumindo role da AWS: $AWS_ROLE_ARN"
  
  # Verificar se o token do service account está disponível
  if [ -f "/var/run/secrets/eks.amazonaws.com/serviceaccount/token" ]; then
    echo "Token do service account encontrado"
    
    # Ler o token do arquivo
    WEB_IDENTITY_TOKEN=$(cat /var/run/secrets/eks.amazonaws.com/serviceaccount/token)
    if [ -z "$WEB_IDENTITY_TOKEN" ]; then
      echo "Erro: Token do service account está vazio"
      exit 1
    fi
    
    export AWS_ROLE_SESSION_NAME="dumpscript-$(date +%s)"
    
    # Assumir a role usando o token do service account
    echo "Assumindo role usando IRSA..."
    echo "Role ARN: $AWS_ROLE_ARN"
    echo "Role Session Name: $AWS_ROLE_SESSION_NAME"
    
    TEMP_ROLE=$(aws sts assume-role-with-web-identity \
      --role-arn "$AWS_ROLE_ARN" \
      --role-session-name "$AWS_ROLE_SESSION_NAME" \
      --web-identity-token "$WEB_IDENTITY_TOKEN" \
      --query 'Credentials.[AccessKeyId,SecretAccessKey,SessionToken]' \
      --output text)
    
    if [ $? -ne 0 ]; then
      echo "Erro ao assumir role. Tentando usar credenciais padrão."
    else
      export AWS_ACCESS_KEY_ID=$(echo $TEMP_ROLE | cut -d' ' -f1)
      export AWS_SECRET_ACCESS_KEY=$(echo $TEMP_ROLE | cut -d' ' -f2)
      export AWS_SESSION_TOKEN=$(echo $TEMP_ROLE | cut -d' ' -f3)
      
      echo "Role assumida com sucesso!"
      echo "AWS_ACCESS_KEY_ID: ${AWS_ACCESS_KEY_ID:0:10}..."
    fi
  else
    echo "Aviso: Token do service account não encontrado em /var/run/secrets/eks.amazonaws.com/serviceaccount/token"
    echo "Listando arquivos disponíveis:"
    find /var/run/secrets -name "*token*" -type f 2>/dev/null || echo "Nenhum token encontrado"
    echo "Tentando usar credenciais padrão."
  fi
fi

# Criar estrutura de data para o path S3
CURRENT_DATE=$(date +%Y-%m-%d)
YEAR=$(date +%Y)
MONTH=$(date +%m)
DAY=$(date +%d)

DUMP_FILE="dump_$(date +%Y%m%d_%H%M%S).sql"
DUMP_FILE_GZ="$DUMP_FILE.gz"

echo "Iniciando dump do banco de dados..."

case "$DB_TYPE" in
  "mysql")
    export MYSQL_PWD="$DB_PASSWORD"
    echo "Executando mysqldump..."
    if ! mysqldump $DUMP_OPTIONS -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" | gzip > "$DUMP_FILE_GZ"; then
      echo "Erro: Falha ao executar mysqldump"
      rm -f "$DUMP_FILE_GZ"
      exit 1
    fi
    ;;
  "postgresql")
    export PGPASSWORD="$DB_PASSWORD"
    echo "Executando pg_dump..."
    if ! pg_dump $DUMP_OPTIONS -h "$DB_HOST" -p "${DB_PORT:-5432}" -U "$DB_USER" "$DB_NAME" | gzip > "$DUMP_FILE_GZ"; then
      echo "Erro: Falha ao executar pg_dump"
      rm -f "$DUMP_FILE_GZ"
      exit 1
    fi
    ;;
  *)
    echo "Erro: DB_TYPE deve ser 'mysql' ou 'postgresql', recebido: $DB_TYPE"
    exit 1
    ;;
esac

# Verificar se o arquivo foi criado e não está vazio
if [ ! -f "$DUMP_FILE_GZ" ]; then
  echo "Erro: Arquivo de dump não foi criado"
  exit 1
fi

# Verificar se o arquivo tem tamanho maior que 0
if [ ! -s "$DUMP_FILE_GZ" ]; then
  echo "Erro: Arquivo de dump está vazio"
  rm -f "$DUMP_FILE_GZ"
  exit 1
fi

# Verificar se o arquivo comprimido tem conteúdo válido
if ! gzip -t "$DUMP_FILE_GZ"; then
  echo "Erro: Arquivo de dump está corrompido"
  rm -f "$DUMP_FILE_GZ"
  exit 1
fi

DUMP_SIZE=$(stat -c%s "$DUMP_FILE_GZ")
echo "Arquivo de dump criado com sucesso. Tamanho: $DUMP_SIZE bytes"

# Construir o path S3 com estrutura de data: bucket/prefix/ano/mes/dia/arquivo
S3_PATH="s3://$S3_BUCKET/$S3_PREFIX/$YEAR/$MONTH/$DAY/$DUMP_FILE_GZ"

# Fazer upload para S3
echo "Fazendo upload para S3..."
echo "Path de destino: $S3_PATH"
if ! aws s3 cp "$DUMP_FILE_GZ" "$S3_PATH"; then
  echo "Erro: Falha ao fazer upload para S3"
  rm -f "$DUMP_FILE_GZ"
  exit 1
fi

rm "$DUMP_FILE_GZ"

echo "Dump concluído com sucesso: $S3_PATH" 
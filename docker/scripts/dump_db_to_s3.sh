#!/bin/sh
set -e

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

DUMP_FILE="dump_$(date +%Y%m%d_%H%M%S).sql"
DUMP_FILE_GZ="$DUMP_FILE.gz"

case "$DB_TYPE" in
  "mysql")
    export MYSQL_PWD="$DB_PASSWORD"
    mysqldump $DUMP_OPTIONS -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" | gzip > "$DUMP_FILE_GZ"
    ;;
  "postgresql")
    export PGPASSWORD="$DB_PASSWORD"
    pg_dump $DUMP_OPTIONS -h "$DB_HOST" -p "${DB_PORT:-5432}" -U "$DB_USER" "$DB_NAME" | gzip > "$DUMP_FILE_GZ"
    ;;
  *)
    echo "Erro: DB_TYPE deve ser 'mysql' ou 'postgresql', recebido: $DB_TYPE"
    exit 1
    ;;
esac

aws s3 cp "$DUMP_FILE_GZ" "s3://$S3_BUCKET/$S3_PREFIX/$DUMP_FILE_GZ"

rm "$DUMP_FILE_GZ"

echo "Dump concluído: s3://$S3_BUCKET/$S3_PREFIX/$DUMP_FILE_GZ" 
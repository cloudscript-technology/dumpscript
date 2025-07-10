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

# AWS Auth
export AWS_REGION
if [ -n "$AWS_ROLE_ARN" ]; then
  CREDS=$(aws sts assume-role --role-arn "$AWS_ROLE_ARN" --role-session-name "db-dump-session")
  export AWS_ACCESS_KEY_ID=$(echo $CREDS | jq -r .Credentials.AccessKeyId)
  export AWS_SECRET_ACCESS_KEY=$(echo $CREDS | jq -r .Credentials.SecretAccessKey)
  export AWS_SESSION_TOKEN=$(echo $CREDS | jq -r .Credentials.SessionToken)
fi

aws s3 cp "$DUMP_FILE_GZ" "s3://$S3_BUCKET/$S3_PREFIX/$DUMP_FILE_GZ"

rm "$DUMP_FILE_GZ"

echo "Dump concluído: s3://$S3_BUCKET/$S3_PREFIX/$DUMP_FILE_GZ" 
# Resolvendo Incompatibilidade de Versões

Este exemplo mostra como resolver o erro de incompatibilidade de versão do PostgreSQL que você enfrentou:

```
pg_dump: error: aborting because of server version mismatch
pg_dump: detail: server version: 16.2; pg_dump version: 15.13
```

## Problema

O erro acontece quando a versão do cliente `pg_dump` é diferente da versão do servidor PostgreSQL. Neste caso:
- Servidor PostgreSQL: **16.2**
- Cliente pg_dump: **15.13**

## Solução

### 1. Identificar a Versão do Servidor

Primeiro, identifique a versão do seu servidor PostgreSQL:

```sql
SELECT version();
-- ou
SHOW server_version;
```

### 2. Configurar o Helm Chart

Configure o Helm chart para usar a versão correta do cliente:

```yaml
# values.yaml
databases:
  - type: postgresql
    version: "16"  # Usar versão 16 para corresponder ao servidor 16.2
    connectionInfo:
      host: "your-postgres-server.com"
      username: "backup_user"
      password: "your-password"
      database: "your_database"
      port: 5432
    aws:
      region: "us-east-1"
      bucket: "your-backup-bucket"
      bucketPrefix: "postgresql-dumps"
      roleArn: "arn:aws:iam::123456789012:role/BackupRole"
    schedule: "0 2 * * *"
    extraArgs: "--no-owner --no-acl"
```

### 3. Executar o Backup

```bash
helm upgrade --install dumpscript-backup ./helm-charts/dumpscript -f values.yaml
```

### 4. Verificar a Execução

O container agora irá:

1. **Instalar o cliente correto em runtime**:
   ```
   === DumpScript Container Starting ===
   DB_TYPE: postgresql
   POSTGRES_VERSION: 16
   Installing database clients...
   Installing PostgreSQL client version 16...
   PostgreSQL client 16 installed successfully
   PostgreSQL client version: pg_dump (PostgreSQL) 16.x
   ```

2. **Executar o dump sem erros**:
   ```
   === Starting Database Dump ===
   Dump concluído: s3://your-bucket/postgresql-dumps/dump_20240115_020000.sql.gz
   ```

## Versões Suportadas

### PostgreSQL
- `13` - Para servidores PostgreSQL 13.x
- `14` - Para servidores PostgreSQL 14.x  
- `15` - Para servidores PostgreSQL 15.x
- `16` - Para servidores PostgreSQL 16.x
- `17` - Para servidores PostgreSQL 17.x

### MySQL/MariaDB
- `8.0` - Para servidores MySQL 8.0
- `10.11` - Para servidores MariaDB 10.11
- `11.4` - Para servidores MariaDB 11.4

## Dicas Importantes

1. **Sempre use a versão major correspondente**: Se o servidor é 16.2, use `version: "16"`
2. **Teste antes de ir para produção**: Execute um dump manual primeiro
3. **Monitore os logs**: Verifique se a instalação foi bem-sucedida nos logs do container
4. **Use variáveis de ambiente**: Para testes rápidos, você pode usar variáveis de ambiente diretamente

## Exemplo de Teste Manual

```bash
# Testar com PostgreSQL 16
docker run --rm \
  -e DB_TYPE=postgresql \
  -e POSTGRES_VERSION=16 \
  -e DB_HOST=your-server.com \
  -e DB_USER=your-user \
  -e DB_PASSWORD=your-password \
  -e DB_NAME=your-database \
  -e AWS_REGION=us-east-1 \
  -e S3_BUCKET=your-bucket \
  -e S3_PREFIX=test-dumps \
  ghcr.io/cloudscript-technology/dumpscript:latest
```

## Logs de Sucesso

Quando tudo funciona corretamente, você verá logs similares a:

```
=== DumpScript Container Starting ===
DB_TYPE: postgresql
POSTGRES_VERSION: 16
Installing database clients...
Installing PostgreSQL client version 16...
PostgreSQL client 16 installed successfully
Database clients installed successfully!
=== Starting Database Dump ===
Dump concluído: s3://your-bucket/test-dumps/dump_20240115_142234.sql.gz
``` 
# Solving Version Incompatibility

This example shows how to solve the PostgreSQL version incompatibility error you encountered:

```
pg_dump: error: aborting because of server version mismatch
pg_dump: detail: server version: 16.2; pg_dump version: 15.13
```

## Problem

This error occurs when the `pg_dump` client version is different from the PostgreSQL server version. In this case:
- PostgreSQL server: **16.2**
- pg_dump client: **15.13**

## Solution

### 1. Identify the Server Version

First, identify your PostgreSQL server version:

```sql
SELECT version();
-- or
SHOW server_version;
```

### 2. Configure the Helm Chart

Configure the Helm chart to use the correct client version:

```yaml
# values.yaml
databases:
  - type: postgresql
    version: "16"  # Use version 16 to match server 16.2
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
    schedule: "0 2 * * *"
    extraArgs: "--no-owner --no-acl"
```

### 3. Run the Backup

```bash
helm upgrade --install dumpscript-backup ./helm-charts/dumpscript -f values.yaml
```

### 4. Check the Execution

The container will now:

1. **Install the correct client at runtime**:
   ```
   === DumpScript Container Starting ===
   DB_TYPE: postgresql
   POSTGRES_VERSION: 16
   Installing database clients...
   Installing PostgreSQL client version 16...
   PostgreSQL client 16 installed successfully
   PostgreSQL client version: pg_dump (PostgreSQL) 16.x
   ```

2. **Run the dump without errors**:
   ```
   === Starting Database Dump ===
   Dump completed: s3://your-bucket/postgresql-dumps/daily/2024/01/15/dump_20240115_020000.sql.gz
   ```

## Supported Versions

### PostgreSQL
- `13` - For PostgreSQL 13.x servers
- `14` - For PostgreSQL 14.x servers
- `15` - For PostgreSQL 15.x servers
- `16` - For PostgreSQL 16.x servers
- `17` - For PostgreSQL 17.x servers

### MySQL/MariaDB
- `8.0` - For MySQL 8.0 servers
- `10.11` - For MariaDB 10.11 servers
- `11.4` - For MariaDB 11.4 servers

## Important Tips

1. **Always use the matching major version**: If your server is 16.2, use `version: "16"`
2. **Test before production**: Run a manual dump first
3. **Monitor the logs**: Check if the installation succeeded in the container logs
4. **Use environment variables**: For quick tests, you can use environment variables directly

## Example: Manual Test

```bash
# Test with PostgreSQL 16
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
  -e PERIODICITY=daily \
  -e RETENTION_DAYS=7 \
  ghcr.io/cloudscript-technology/dumpscript:latest
```

## Success Logs

When everything works correctly, you will see logs similar to:

```
=== DumpScript Container Starting ===
DB_TYPE: postgresql
POSTGRES_VERSION: 16
Installing database clients...
Installing PostgreSQL client version 16...
PostgreSQL client 16 installed successfully
Database clients installed successfully!
=== Starting Database Dump ===
Dump completed: s3://your-bucket/test-dumps/daily/2024/01/15/dump_20240115_142234.sql.gz
``` 
# DumpScript

Database dump and restore tool with configurable client versions.

## Features

- Support for PostgreSQL and MySQL/MariaDB databases
- **Runtime configurable database client versions** - No need to rebuild images
- Automatic S3 upload of database dumps
- AWS IAM role support for secure access
- Kubernetes CronJob deployment via Helm chart
- Containerized execution with Alpine Linux

## Database Client Versions

### PostgreSQL
Supported versions: `13`, `14`, `15`, `16`, `17`

The client version should match your PostgreSQL server version to avoid compatibility issues like:
```
pg_dump: error: aborting because of server version mismatch
pg_dump: detail: server version: 16.2; pg_dump version: 15.13
```

### MySQL/MariaDB
Supported versions: 
- `8.0` - MySQL 8.0
- `10.11` - MariaDB 10.11 (default)
- `11.4` - MariaDB 11.4

## Usage

### Environment Variables

#### Required
- `DB_TYPE` - Database type (`postgresql` or `mysql`)
- `DB_HOST` - Database host
- `DB_USER` - Database username
- `DB_PASSWORD` - Database password
- `DB_NAME` - Database name
- `AWS_REGION` - AWS region for S3
- `S3_BUCKET` - S3 bucket name
- `S3_PREFIX` - S3 prefix for dumps
- `PERIODICITY` - Backup periodicity (`daily`, `weekly`, `monthly`, `yearly`)
- `RETENTION_DAYS` - Retention days

#### Optional
- `POSTGRES_VERSION` - PostgreSQL client version (default: `16`)
- `MYSQL_VERSION` - MySQL/MariaDB client version (default: `10.11`)
- `DB_PORT` - Database port (default: 5432 for PostgreSQL, 3306 for MySQL)
- `AWS_ROLE_ARN` - AWS IAM role ARN for authentication
- `DUMP_OPTIONS` - Additional options for `pg_dump` or `mysqldump`

### Docker Example

```bash
# PostgreSQL 16 daily dump
 docker run --rm \
  -e DB_TYPE=postgresql \
  -e POSTGRES_VERSION=16 \
  -e DB_HOST=localhost \
  -e DB_USER=user \
  -e DB_PASSWORD=password \
  -e DB_NAME=mydb \
  -e AWS_REGION=us-east-1 \
  -e S3_BUCKET=my-backups \
  -e S3_PREFIX=postgresql-dumps \
  -e PERIODICITY=daily \
  -e RETENTION_DAYS=7 \
  ghcr.io/cloudscript-technology/dumpscript:latest

# MySQL 8.0 weekly dump
 docker run --rm \
  -e DB_TYPE=mysql \
  -e MYSQL_VERSION=8.0 \
  -e DB_HOST=localhost \
  -e DB_USER=user \
  -e DB_PASSWORD=password \
  -e DB_NAME=mydb \
  -e AWS_REGION=us-east-1 \
  -e S3_BUCKET=my-backups \
  -e S3_PREFIX=mysql-dumps \
  -e PERIODICITY=weekly \
  -e RETENTION_DAYS=7 \
  ghcr.io/cloudscript-technology/dumpscript:latest
```

### Helm Chart Example

```yaml
databases:
  - type: postgresql
    version: "16"  # Matches PostgreSQL server version
    connectionInfo:
      host: "postgres.example.com"
      username: "backup_user"
      password: "secure_password"
      database: "production_db"
      port: 5432
    aws:
      region: "us-east-1"
      bucket: "my-db-backups"
      bucketPrefix: "postgresql/production"
    schedule: "0 2 * * *"  # Daily at 2 AM
    extraArgs: "--no-owner --no-acl"
    
  - type: mysql
    version: "8.0"  # Matches MySQL server version
    connectionInfo:
      host: "mysql.example.com"
      username: "backup_user"
      password: "secure_password"  
      database: "app_db"
      port: 3306
    aws:
      region: "us-east-1"
      bucket: "my-db-backups"
      bucketPrefix: "mysql/app"
    schedule: "0 3 * * *"  # Daily at 3 AM
    extraArgs: "--single-transaction --routines"
```

## How It Works

1. **Runtime Installation**: When the container starts, it reads the `POSTGRES_VERSION` or `MYSQL_VERSION` environment variable
2. **Dynamic Client Installation**: The appropriate database client is installed using Alpine's package manager
3. **Version Verification**: The installation is verified and client version is logged
4. **Database Operations**: The original dump/restore scripts are executed with the correct client version
5. **S3 Path Structure**: The dump is uploaded to S3 at the path: `s3://$S3_BUCKET/$S3_PREFIX/$PERIODICITY/$YEAR/$MONTH/$DAY/$DUMP_FILE_GZ` (e.g., `daily`, `weekly`, `monthly`, `yearly`)

## Building

```bash
# Build dump image
docker build -t dumpscript:latest -f docker/Dockerfile.dump .

# Build restore image  
docker build -t dumpscript-restore:latest -f docker/Dockerfile.restore .
```

## Files

- `docker/Dockerfile.dump` - Dump container image
- `docker/Dockerfile.restore` - Restore container image
- `scripts/dump_db_to_s3.sh` - Database dump script
- `scripts/restore_db_from_s3.sh` - Database restore script
- `scripts/install_db_clients.sh` - Dynamic client installation script
- `scripts/entrypoint_dump.sh` - Dump container entrypoint
- `scripts/entrypoint_restore.sh` - Restore container entrypoint 

## AWS IAM Policy Requirements

To allow DumpScript to upload, list, and delete backups in your S3 bucket, the IAM role used by the container must have the following permissions:

- `s3:GetObject` – Read backup files from S3 (for restore)
- `s3:PutObject` – Upload new backup files to S3
- `s3:DeleteObject` – Remove old backups from S3 (for retention policy)
- `s3:ListBucket` – List objects in the S3 bucket (for cleanup and restore)
- `s3:ListObjects`, `s3:ListObjectsV2` – List objects within a bucket and support recursive listing (required for AWS CLI and scripts that use `aws s3 ls --recursive`)

Below is an example of a minimal IAM policy for S3 access:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject",
        "s3:ListObjects",
        "s3:ListObjectsV2"
      ],
      "Resource": "arn:aws:s3:::your-bucket-name/*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "s3:ListBucket"
      ],
      "Resource": "arn:aws:s3:::your-bucket-name"
    }
  ]
}
```

Replace `your-bucket-name` with the actual name of your S3 bucket. Granting only these permissions ensures the tool can perform all backup, restore, and cleanup operations securely. 
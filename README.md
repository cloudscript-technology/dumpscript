# DumpScript

Database dump and restore tool with configurable client versions.

[![Artifact Hub](https://img.shields.io/badge/Artifact-Hub-417598?style=for-the-badge&logo=artifacthub&logoColor=white)](https://artifacthub.io/packages/helm/cloudscript/dumpscript)
[![Helm Chart](https://img.shields.io/badge/Helm-Chart-0F1689?style=for-the-badge&logo=helm&logoColor=white)](https://github.com/cloudscript-technology/helm-charts/tree/main/dumpscript)
[![Slack Bot](https://img.shields.io/badge/Slack-Bot-4A154B?style=for-the-badge&logo=slack&logoColor=white)](https://slack.com/marketplace/A096PJ2QBD5-dumpscript-bot)
[![Website](https://img.shields.io/badge/Website-Cloudscript-2E8B57?style=for-the-badge&logo=globe&logoColor=white)](https://cloudscript.com.br)

## Features

- Support for PostgreSQL and MySQL/MariaDB databases
- **Runtime configurable database client versions** - No need to rebuild images
- **Multiple backup schedules** - Support for daily, weekly, monthly, and yearly backups per database
- **Slack notifications** - Optional notifications for backup status
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

#### Simple Configuration

```yaml
databases:
  - type: postgresql
    version: "17"  # Matches PostgreSQL server version
    periodicity:
      - type: daily
        retentionDays: 7
        schedule: "0 2 * * *"  # Daily at 2:00 AM
      - type: weekly
        retentionDays: 30
        schedule: "0 3 * * 0"  # Weekly on Sunday at 3:00 AM
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
    extraArgs: "--no-owner --no-acl"
    
  - type: mysql
    version: "10.11"  # Matches MariaDB server version
    periodicity:
      - type: daily
        retentionDays: 14
        schedule: "0 1 * * *"  # Daily at 1:00 AM
      - type: monthly
        retentionDays: 365
        schedule: "0 4 1 * *"  # Monthly on 1st at 4:00 AM
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
    extraArgs: "--single-transaction --routines"
```

#### Advanced Configuration with Slack Notifications

```yaml
databases:
  - type: postgresql
    version: "16"
    periodicity:
      - type: daily
        retentionDays: 7
        schedule: "0 2 * * *"
      - type: weekly
        retentionDays: 30
        schedule: "0 3 * * 0"
      - type: monthly
        retentionDays: 365
        schedule: "0 4 1 * *"
    connectionInfo:
      secretName: "postgres-credentials"
    aws:
      secretName: "aws-credentials"
      region: "us-east-1"
      bucket: "secure-backups"
      bucketPrefix: "production/postgres/"
    extraArgs: "--verbose --single-transaction"

notifications:
  slack:
    enabled: true
    webhookUrl: "https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK"
    channel: "#backups"
    username: "Dumpscript Bot"
    notifyOnSuccess: false

serviceAccount:
  create: true
  annotations:
    eks.amazonaws.com/role-arn: "arn:aws:iam::123456789012:role/DatabaseBackupRole"
```

#### Multiple Databases with Different Versions

```yaml
databases:
  # Production PostgreSQL
  - type: postgresql
    version: "17"
    periodicity:
      - type: daily
        retentionDays: 30
        schedule: "0 1 * * *"
      - type: monthly
        retentionDays: 365
        schedule: "0 4 1 * *"
    connectionInfo:
      secretName: "prod-postgres-creds"
    aws:
      secretName: "prod-aws-creds"
      region: "us-west-2"
      bucket: "secure-backups"
      bucketPrefix: "production/postgres/"
    extraArgs: "--verbose --single-transaction"

  # Development MySQL
  - type: mysql
    version: "8.0"
    periodicity:
      - type: daily
        retentionDays: 14
        schedule: "0 2 * * *"
      - type: weekly
        retentionDays: 60
        schedule: "0 3 * * 0"
    connectionInfo:
      secretName: "dev-mysql-creds"
    aws:
      secretName: "dev-aws-creds"
      region: "us-west-2"
      bucket: "secure-backups"
      bucketPrefix: "develop/mysql/"
    extraArgs: "--opt --single-transaction"

  # Test PostgreSQL
  - type: postgresql
    version: "15"
    periodicity:
      - type: weekly
        retentionDays: 90
        schedule: "0 0 * * 0"
    connectionInfo:
      secretName: "test-postgres-creds"
    aws:
      secretName: "test-aws-creds"
      region: "us-west-2"
      bucket: "secure-backups"
      bucketPrefix: "test/postgres/"
    extraArgs: "--clean --if-exists"
```

## Multiple Backup Schedules

Each database can have multiple backup schedules with different retention policies:

- **Daily backups**: Short-term retention (7-30 days)
- **Weekly backups**: Medium-term retention (30-90 days)
- **Monthly backups**: Long-term retention (90-365 days)
- **Yearly backups**: Long-term archival (365+ days)

This allows for flexible backup strategies like:
- Daily backups for quick recovery
- Weekly backups for medium-term retention
- Monthly backups for compliance
- Yearly backups for long-term archival

## Slack Notifications

Enable Slack notifications to monitor backup status:

```yaml
notifications:
  slack:
    enabled: true
    webhookUrl: "https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK"
    channel: "#backups"  # Optional
    username: "Dumpscript Bot"  # Optional
    notifyOnSuccess: false  # Only notify on failures by default
```

## How It Works

1. **Runtime Installation**: When the container starts, it reads the `POSTGRES_VERSION` or `MYSQL_VERSION` environment variable
2. **Dynamic Client Installation**: The appropriate database client is installed using Alpine's package manager
3. **Version Verification**: The installation is verified and client version is logged
4. **Database Operations**: The original dump/restore scripts are executed with the correct client version
5. **Multiple Schedules**: Each database can have multiple backup schedules with different retention policies
6. **S3 Path Structure**: The dump is uploaded to S3 at the path: `s3://$S3_BUCKET/$S3_PREFIX/$PERIODICITY/$YEAR/$MONTH/$DAY/$DUMP_FILE_GZ` (e.g., `daily`, `weekly`, `monthly`, `yearly`)
7. **Notifications**: Optional Slack notifications for backup status

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
- `scripts/notify_slack.sh` - Slack notification script

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
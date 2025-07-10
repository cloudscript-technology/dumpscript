# DumpScript

Unified system for backup and restore of MySQL and PostgreSQL databases to Amazon S3.

## Architecture

- **Dockerfile.dump**: Container for database backup (dump)
- **Dockerfile.restore**: Container for database restore
- Support for MySQL and PostgreSQL
- Automatic upload/download to/from Amazon S3
- Configuration via environment variables

## Prerequisites

- Docker installed
- AWS credentials configured
- Access to databases (MySQL or PostgreSQL)
- S3 bucket created

## Build Images

```bash
# Backup image
docker build -f Dockerfile.dump -t backup-db .

# Restore image
docker build -f Dockerfile.restore -t restore-db .
```

## Backup (Dump)

### Required Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `DB_TYPE` | Database type: `mysql` or `postgresql` | `postgresql` |
| `DB_HOST` | Database host | `host.docker.internal` |
| `DB_USER` | Database user | `postgres` |
| `DB_PASSWORD` | Database password | `mypassword` |
| `DB_NAME` | Database name | `mydb` |
| `AWS_ACCESS_KEY_ID` | AWS Access Key | `AKIAIOSFODNN7EXAMPLE` |
| `AWS_SECRET_ACCESS_KEY` | AWS Secret Key | `wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY` |
| `AWS_REGION` | AWS Region | `us-east-1` |
| `S3_BUCKET` | S3 bucket name | `my-backup-bucket` |
| `S3_PREFIX` | S3 prefix/folder | `backups/production` |

### Optional Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `DB_PORT` | Database port | MySQL: `3306`, PostgreSQL: `5432` |
| `AWS_SESSION_TOKEN` | AWS session token (for temporary roles) | - |
| `AWS_ROLE_ARN` | Role ARN for assume role | - |
| `DUMP_OPTIONS` | Specific options for mysqldump/pg_dump | - |

### Backup Examples

#### PostgreSQL
```bash
docker run --rm \
  -e DB_TYPE=postgresql \
  -e DB_HOST=host.docker.internal \
  -e DB_PORT=5432 \
  -e DB_USER=postgres \
  -e DB_PASSWORD=mypassword \
  -e DB_NAME=mydb \
  -e AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE \
  -e AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  -e AWS_SESSION_TOKEN=IQoJb3JpASDFFGVjELb//////////EXAMPLETOKEN \
  -e AWS_REGION=us-east-1 \
  -e S3_BUCKET=my-backup-bucket \
  -e S3_PREFIX=backups/postgresql \
  -e DUMP_OPTIONS="--format plain --encoding UTF8 --no-owner --no-privileges" \
  backup-db
```

#### MySQL
```bash
docker run --rm \
  -e DB_TYPE=mysql \
  -e DB_HOST=host.docker.internal \
  -e DB_PORT=3306 \
  -e DB_USER=root \
  -e DB_PASSWORD=mypassword \
  -e DB_NAME=mydb \
  -e AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE \
  -e AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  -e AWS_SESSION_TOKEN=IQoJb3JpASDFFGVjELb//////////EXAMPLETOKEN \
  -e AWS_REGION=us-east-1 \
  -e S3_BUCKET=my-backup-bucket \
  -e S3_PREFIX=backups/mysql \
  -e DUMP_OPTIONS="--single-transaction --routines --triggers --set-gtid-purged=OFF" \
  backup-db
```

## Restore

### Required Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `DB_TYPE` | Database type: `mysql` or `postgresql` | `postgresql` |
| `DB_HOST` | Database host | `host.docker.internal` |
| `DB_USER` | Database user | `postgres` |
| `DB_PASSWORD` | Database password | `mypassword` |
| `DB_NAME` | Database name | `mydb` |
| `AWS_ACCESS_KEY_ID` | AWS Access Key | `AKIAIOSFODNN7EXAMPLE` |
| `AWS_SECRET_ACCESS_KEY` | AWS Secret Key | `wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY` |
| `AWS_REGION` | AWS Region | `us-east-1` |
| `S3_BUCKET` | S3 bucket name | `my-backup-bucket` |
| `S3_KEY` | Full S3 file path | `backups/production/dump_20250710_143004.sql.gz` |

### Optional Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `DB_PORT` | Database port | MySQL: `3306`, PostgreSQL: `5432` |
| `CREATE_DB` | Create database before restore (`1` for yes) | - |
| `AWS_SESSION_TOKEN` | AWS session token | - |
| `AWS_ROLE_ARN` | Role ARN for assume role | - |

### Restore Examples

#### PostgreSQL
```bash
docker run --rm \
  -e DB_TYPE=postgresql \
  -e DB_HOST=host.docker.internal \
  -e DB_PORT=5432 \
  -e DB_USER=postgres \
  -e DB_PASSWORD=mypassword \
  -e DB_NAME=mydb_restore \
  -e AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE \
  -e AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  -e AWS_SESSION_TOKEN=IQoJb3JpASDFFGVjELb//////////EXAMPLETOKEN \
  -e AWS_REGION=us-east-1 \
  -e S3_BUCKET=my-backup-bucket \
  -e S3_KEY=backups/postgresql/dump_20250710_143004.sql.gz \
  -e CREATE_DB=1 \
  restore-db
```

#### MySQL
```bash
docker run --rm \
  -e DB_TYPE=mysql \
  -e DB_HOST=host.docker.internal \
  -e DB_PORT=3306 \
  -e DB_USER=root \
  -e DB_PASSWORD=mypassword \
  -e DB_NAME=mydb_restore \
  -e AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE \
  -e AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  -e AWS_SESSION_TOKEN=IQoJb3JpASDFFGVjELb//////////EXAMPLETOKEN \
  -e AWS_REGION=us-east-1 \
  -e S3_BUCKET=my-backup-bucket \
  -e S3_KEY=backups/mysql/dump_20250710_143004.sql.gz \
  -e CREATE_DB=1 \
  restore-db
```

## Recommended DUMP_OPTIONS

### PostgreSQL (pg_dump)
```bash
# Complete dump with UTF8 encoding
--format plain --encoding UTF8 --no-owner --no-privileges

# Schema only
--schema-only --format plain --encoding UTF8

# Data only
--data-only --format plain --encoding UTF8

# With compression (don't use with script's gzip)
--format custom --compress 9
```

### MySQL (mysqldump)
```bash
# Complete dump with transaction
--single-transaction --routines --triggers --set-gtid-purged=OFF

# Schema only
--no-data --routines --triggers

# Data only
--no-create-info --skip-triggers

# With specific charset
--default-character-set=utf8mb4
```

## File Format

Backup files are saved to S3 in the format:
```
s3://YOUR_BUCKET/YOUR_PREFIX/dump_YYYYMMDD_HHMMSS.sql.gz
```

Example:
```
s3://my-backup-bucket/backups/postgresql/dump_20250710_143004.sql.gz
```

## AWS Permissions

The AWS user/role needs the following permissions:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::my-backup-bucket",
        "arn:aws:s3:::my-backup-bucket/*"
      ]
    }
  ]
}
```

## Troubleshooting

### S3 403 Error
- Check if AWS credentials are correct
- Verify user has permissions on the bucket
- Check if bucket exists and is in the correct region

### Database Connection Error
- Check if the host is accessible from the container
- For local databases, use `host.docker.internal` (Mac/Windows) or `172.17.0.1` (Linux)
- Verify database credentials are correct

### File Not Found in S3
- Check if the full path (`S3_KEY`) is correct
- List bucket objects: `aws s3 ls s3://my-bucket/my-prefix/`

## Automation

For use in CI/CD pipelines or cron jobs, you can create shell scripts or use tools like Kubernetes CronJob:

```bash
#!/bin/bash
# backup-daily.sh

docker run --rm \
  -e DB_TYPE=postgresql \
  -e DB_HOST=my-rds.amazonaws.com \
  -e DB_USER=$DB_USER \
  -e DB_PASSWORD=$DB_PASSWORD \
  -e DB_NAME=production \
  -e AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  -e AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY \
  -e AWS_REGION=us-east-1 \
  -e S3_BUCKET=production-backups \
  -e S3_PREFIX=daily \
  backup-db
``` 
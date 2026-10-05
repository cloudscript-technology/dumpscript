# DumpScript

Database dump and restore tool with configurable client versions.

[![Artifact Hub](https://img.shields.io/badge/Artifact-Hub-417598?style=for-the-badge&logo=artifacthub&logoColor=white)](https://artifacthub.io/packages/helm/cloudscript/dumpscript)
[![Helm Chart](https://img.shields.io/badge/Helm-Chart-0F1689?style=for-the-badge&logo=helm&logoColor=white)](https://github.com/cloudscript-technology/helm-charts/tree/main/dumpscript)
[![Slack Bot](https://img.shields.io/badge/Slack-Bot-4A154B?style=for-the-badge&logo=slack&logoColor=white)](https://slack.com/marketplace/A096PJ2QBD5-dumpscript-bot)
[![Website](https://img.shields.io/badge/Website-Cloudscript-2E8B57?style=for-the-badge&logo=globe&logoColor=white)](https://cloudscript.com.br)

## Features

- Support for PostgreSQL, MySQL/MariaDB, MongoDB and ClickHouse databases
- **Multiple storage backends** - S3-compatible storage (AWS, MinIO) and Azure Blob Storage
- **Runtime configurable database client versions** - No need to rebuild images
- **ClickHouse native backups** - server-side `BACKUP ... TO S3()/AzureBlobStorage()` as a single archive, no local disk; full + incremental (`base_backup`) per periodicity
- **Multiple backup schedules** - Support for daily, weekly, monthly, and yearly backups per database
- **Slack notifications** - Optional notifications for backup status
- Automatic upload of database dumps to configured storage backend
- AWS IAM role support for secure access
- Kubernetes CronJob deployment via Helm chart
- Containerized execution with Alpine Linux

## Storage Backends

DumpScript supports two storage backends. The backend is selected via the `STORAGE_BACKEND` environment variable (defaults to `s3` for backward compatibility).

### S3-Compatible Storage (default)

Works with AWS S3, MinIO, and any S3-compatible object storage. This is the default backend — existing deployments require **zero changes**.

### Azure Blob Storage

Works with Azure Blob Storage accounts. Supports authentication via storage account key or SAS token.

### Storage Path Structure

Both backends use the same path structure for organizing backups:

```
<prefix>/<periodicity>/<year>/<month>/<day>/<dump_file>
```

Example: `postgresql-dumps/daily/2025/03/24/dump_20250324_120000.sql.gz`

## Database Client Versions

### PostgreSQL
Supported versions: `13`, `14`, `15`, `16`, `17`, `18`

**Version Availability by Alpine Base Image:**
- **Alpine 3.20**: PostgreSQL versions `14`, `15`, `16`
- **Alpine 3.21**: PostgreSQL versions `15`, `16`, `17`
- **Alpine edge** (pinned): PostgreSQL versions `16`, `17`, `18`

The client version should match your PostgreSQL server version to avoid compatibility issues like:
```
pg_dump: error: aborting because of server version mismatch
pg_dump: detail: server version: 16.2; pg_dump version: 15.13
```

### MySQL/MariaDB
Supported versions:
- `5.7` - MySQL 5.7 (uses `mysqldump`; may use compatible MariaDB client if 5.7 client unavailable on base image)
- `8.0` - MySQL 8.0 (uses `mysqldump`)
- `10.11` - MariaDB 10.11 (default, uses `mariadb-dump`)
- `11.4` - MariaDB 11.4 (uses `mariadb-dump`)

### MongoDB Tools
MongoDB backups use `mongodump`/`mongorestore` from MongoDB Database Tools.
Tools are installed at runtime (no version pinning).

### ClickHouse
ClickHouse backups do not use a client binary. DumpScript sends a native
`BACKUP ... TO S3(...)` (or `AzureBlobStorage(...)`) statement to the server over the
HTTP interface (`curl`, port `8123`/`8443`) and polls `system.backups` until it finishes.
The **server** uploads the archive straight to the bucket: nothing is written to the job's
disk, so no `/dumpscript` volume is needed. Requires ClickHouse **>= 24.3**.
See [ClickHouse](#clickhouse-1) below for grants, network and restore.

## Usage

### Environment Variables

#### Required

| Variable | Description |
|----------|-------------|
| `DB_TYPE` | Database type (`postgresql`, `mysql`, `mariadb`, `mongodb` or `clickhouse`) |
| `DB_HOST` | Database host |
| `DB_USER` | Database username |
| `DB_PASSWORD` | Database password |
| `PERIODICITY` | Backup periodicity (`daily`, `weekly`, `monthly`, `yearly`) |
| `RETENTION_DAYS` | Number of days to retain backups (cleanup runs **after** a successful dump) |

#### Storage Backend

| Variable | Default | Description |
|----------|---------|-------------|
| `STORAGE_BACKEND` | `s3` | Storage backend: `s3` or `azure` |

#### S3 Backend Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `AWS_REGION` | Yes | AWS region |
| `S3_BUCKET` | Yes | S3 bucket name |
| `S3_PREFIX` | Yes | S3 key prefix for dumps |
| `AWS_ACCESS_KEY_ID` | Yes* | AWS access key (*or use IRSA; `clickhouse` needs a static key, see below) |
| `AWS_SECRET_ACCESS_KEY` | Yes* | AWS secret key (*or use IRSA) |
| `AWS_ROLE_ARN` | No | AWS IAM role ARN for IRSA authentication |
| `AWS_S3_ENDPOINT_URL` | No | Custom S3 endpoint (for MinIO or S3-compatible storage) |

#### Azure Backend Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `AZURE_STORAGE_ACCOUNT` | Yes | Azure storage account name |
| `AZURE_STORAGE_KEY` | Yes* | Storage account access key (*or use SAS token) |
| `AZURE_STORAGE_SAS_TOKEN` | No | SAS token (alternative to storage key) |
| `AZURE_STORAGE_CONTAINER` | Yes | Azure Blob container name |
| `AZURE_STORAGE_PREFIX` | Yes | Blob key prefix for dumps |

#### Upload Tuning (optional, applies to all backends)

| Variable | Default | Description |
|----------|---------|-------------|
| `STORAGE_UPLOAD_CUTOFF` | `200M` | File size threshold for multipart/chunked upload |
| `STORAGE_CHUNK_SIZE` | `100M` | Chunk size for multipart/chunked upload |
| `STORAGE_UPLOAD_CONCURRENCY` | `4` | Number of parallel upload threads |

#### Database Options (optional)

| Variable | Description |
|----------|-------------|
| `POSTGRES_VERSION` | PostgreSQL client version (default: `16`) |
| `MYSQL_VERSION` | MySQL client version (`5.7` or `8.0`) — dumps with `mysqldump` |
| `MARIADB_VERSION` | MariaDB client version (default: `11.4`) — dumps with `mariadb-dump` |
| `DB_PORT` | Database port (default: 5432 for PostgreSQL, 3306 for MySQL, 27017 for MongoDB, 8123 for ClickHouse HTTP) |
| `DB_NAME` | Database name (if omitted, dumps all databases in the instance). Allowed characters: letters, digits, `_`, `.`, `-` — anything else is rejected (prevents SQL/connection-string/option injection) |
| `RETENTION_MIN_KEEP` | Never delete the N most recent backups of the periodicity, whatever `RETENTION_DAYS` says (default: `1`) |
| `DUMP_OPTIONS` | Additional options for the dump command (e.g., `--authenticationDatabase=admin`). For `clickhouse` it is appended to the `BACKUP ... SETTINGS` clause (e.g. `allow_s3_native_copy=0, deduplicate_files=1`) |

#### ClickHouse Options (optional, `DB_TYPE=clickhouse`)

| Variable | Default | Description |
|----------|---------|-------------|
| `CLICKHOUSE_SECURE` | `false` | Use `https://` for the HTTP interface (default port becomes `8443`) |
| `CLICKHOUSE_CA_CERT` | — | CA bundle path for `https://` |
| `CLICKHOUSE_ARCHIVE_FORMAT` | `tar.zst` | Archive written by the server: `tar`, `tar.gz` or `tar.zst` (`zip` is not supported for S3/Azure) |
| `CLICKHOUSE_EXCLUDE_DATABASES` | `system,information_schema,INFORMATION_SCHEMA` | Databases skipped on a full-instance backup (`DB_NAME` empty) |
| `CLICKHOUSE_BACKUP_ACCESS_ENTITIES` | `true` | Include users, roles, grants, quotas, settings profiles, row policies, functions and named collections on a full-instance backup |
| `CLICKHOUSE_BACKUP_TIMEOUT` | `21600` | Seconds to wait for the server-side backup (6h) |
| `CLICKHOUSE_BACKUP_POLL_INTERVAL` | `15` | Seconds between `system.backups` polls |
| `CLICKHOUSE_CLUSTER` | _(empty)_ | Cluster name (`system.clusters`) to poll `system.backups` on all replicas via `clusterAllReplicas()`. Set it when `DB_HOST` is a Service balancing several replicas: `system.backups` is local to each server, so without it the poll can miss the replica running the backup ("Lost track") even though the backup succeeds |
| `CLICKHOUSE_USE_SERVER_CREDENTIALS` | `false` | Emit `S3('<url>')` without keys; the server authenticates with its own S3 configuration |
| `CLICKHOUSE_BASE_BACKUP_PERIODICITY` | — | Enables **incremental backups**: runs of this periodicity (e.g. `weekly`) are full; every other periodicity (e.g. `daily`) is incremental against the newest archive of it (`SETTINGS base_backup = ...`). No base found = full backup with a warning. See [ClickHouse incremental backups](#clickhouse-incremental-backups) |

### Docker Examples

#### S3 Backend

```bash
# PostgreSQL 16 daily dump to S3
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

# MySQL 8.0 weekly dump to S3
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

# MariaDB 11.4 daily dump to S3 (mariadb-dump)
docker run --rm \
  -e DB_TYPE=mariadb \
  -e MARIADB_VERSION=11.4 \
  -e DB_HOST=localhost \
  -e DB_USER=user \
  -e DB_PASSWORD=password \
  -e DB_NAME=mydb \
  -e AWS_REGION=us-east-1 \
  -e S3_BUCKET=my-backups \
  -e S3_PREFIX=mariadb-dumps \
  -e PERIODICITY=daily \
  -e RETENTION_DAYS=7 \
  ghcr.io/cloudscript-technology/dumpscript:latest

# MySQL 5.7 daily dump to S3
docker run --rm \
  -e DB_TYPE=mysql \
  -e MYSQL_VERSION=5.7 \
  -e DB_HOST=localhost \
  -e DB_USER=user \
  -e DB_PASSWORD=password \
  -e DB_NAME=mydb \
  -e AWS_REGION=us-east-1 \
  -e S3_BUCKET=my-backups \
  -e S3_PREFIX=mysql57-dumps \
  -e PERIODICITY=daily \
  -e RETENTION_DAYS=7 \
  ghcr.io/cloudscript-technology/dumpscript:latest

# MongoDB daily dump to S3
docker run --rm \
  -e DB_TYPE=mongodb \
  -e DB_HOST=localhost \
  -e DB_USER=user \
  -e DB_PASSWORD=password \
  -e DB_NAME=mydb \
  -e DB_PORT=27017 \
  -e DUMP_OPTIONS="--authenticationDatabase=admin" \
  -e AWS_REGION=us-east-1 \
  -e S3_BUCKET=my-backups \
  -e S3_PREFIX=mongodb-dumps \
  -e PERIODICITY=daily \
  -e RETENTION_DAYS=7 \
  ghcr.io/cloudscript-technology/dumpscript:latest

# ClickHouse daily backup to GCS (S3-compatible API, HMAC key) - runs on the server
docker run --rm \
  -e DB_TYPE=clickhouse \
  -e DB_HOST=clickhouse.databases.svc.cluster.local \
  -e DB_PORT=8123 \
  -e DB_USER=backup \
  -e DB_PASSWORD=password \
  -e DB_NAME=analytics \
  -e AWS_ACCESS_KEY_ID=GOOG1E... \
  -e AWS_SECRET_ACCESS_KEY=... \
  -e AWS_REGION=us-central1 \
  -e AWS_S3_ENDPOINT_URL=https://storage.googleapis.com \
  -e S3_BUCKET=my-backups \
  -e S3_PREFIX=clickhouse/analytics \
  -e PERIODICITY=daily \
  -e RETENTION_DAYS=7 \
  ghcr.io/cloudscript-technology/dumpscript:latest

# S3-compatible storage (MinIO)
docker run --rm \
  -e DB_TYPE=postgresql \
  -e POSTGRES_VERSION=16 \
  -e DB_HOST=localhost \
  -e DB_USER=user \
  -e DB_PASSWORD=password \
  -e DB_NAME=mydb \
  -e AWS_ACCESS_KEY_ID=minioadmin \
  -e AWS_SECRET_ACCESS_KEY=minioadmin \
  -e AWS_REGION=us-east-1 \
  -e AWS_S3_ENDPOINT_URL=http://minio:9000 \
  -e S3_BUCKET=my-backups \
  -e S3_PREFIX=postgresql-dumps \
  -e PERIODICITY=daily \
  -e RETENTION_DAYS=7 \
  ghcr.io/cloudscript-technology/dumpscript:latest
```

#### Azure Blob Storage Backend

```bash
# PostgreSQL 16 daily dump to Azure Blob Storage
docker run --rm \
  -e DB_TYPE=postgresql \
  -e POSTGRES_VERSION=16 \
  -e DB_HOST=localhost \
  -e DB_USER=user \
  -e DB_PASSWORD=password \
  -e DB_NAME=mydb \
  -e STORAGE_BACKEND=azure \
  -e AZURE_STORAGE_ACCOUNT=mystorageaccount \
  -e AZURE_STORAGE_KEY=mybase64encodedkey... \
  -e AZURE_STORAGE_CONTAINER=db-backups \
  -e AZURE_STORAGE_PREFIX=postgresql-dumps \
  -e PERIODICITY=daily \
  -e RETENTION_DAYS=7 \
  ghcr.io/cloudscript-technology/dumpscript:latest

# MySQL 8.0 daily dump to Azure Blob Storage (with SAS token)
docker run --rm \
  -e DB_TYPE=mysql \
  -e MYSQL_VERSION=8.0 \
  -e DB_HOST=localhost \
  -e DB_USER=user \
  -e DB_PASSWORD=password \
  -e DB_NAME=mydb \
  -e STORAGE_BACKEND=azure \
  -e AZURE_STORAGE_ACCOUNT=mystorageaccount \
  -e AZURE_STORAGE_SAS_TOKEN="sv=2021-06-08&ss=b&srt=sco&sp=rwdlac&se=2026-01-01T00:00:00Z&sig=..." \
  -e AZURE_STORAGE_CONTAINER=db-backups \
  -e AZURE_STORAGE_PREFIX=mysql-dumps \
  -e PERIODICITY=daily \
  -e RETENTION_DAYS=7 \
  ghcr.io/cloudscript-technology/dumpscript:latest

# MongoDB daily dump to Azure Blob Storage
docker run --rm \
  -e DB_TYPE=mongodb \
  -e DB_HOST=localhost \
  -e DB_USER=user \
  -e DB_PASSWORD=password \
  -e DB_NAME=mydb \
  -e DB_PORT=27017 \
  -e DUMP_OPTIONS="--authenticationDatabase=admin" \
  -e STORAGE_BACKEND=azure \
  -e AZURE_STORAGE_ACCOUNT=mystorageaccount \
  -e AZURE_STORAGE_KEY=mybase64encodedkey... \
  -e AZURE_STORAGE_CONTAINER=db-backups \
  -e AZURE_STORAGE_PREFIX=mongodb-dumps \
  -e PERIODICITY=daily \
  -e RETENTION_DAYS=7 \
  ghcr.io/cloudscript-technology/dumpscript:latest
```

### Helm Chart Examples

#### S3 Backend (default)

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

  - type: mariadb
    version: "11.4"  # Matches MariaDB server version
    periodicity:
      - type: daily
        retentionDays: 14
        schedule: "0 1 * * *"  # Daily at 1:00 AM
      - type: monthly
        retentionDays: 365
        schedule: "0 4 1 * *"  # Monthly on 1st at 4:00 AM
    connectionInfo:
      host: "mariadb.example.com"
      username: "backup_user"
      password: "secure_password"
      database: "app_db"
      port: 3306
    aws:
      region: "us-east-1"
      bucket: "my-db-backups"
      bucketPrefix: "mariadb/app"
    extraArgs: "--single-transaction --routines"
```

#### Azure Blob Storage Backend

```yaml
databases:
  - type: postgresql
    version: "17"
    periodicity:
      - type: daily
        retentionDays: 7
        schedule: "0 2 * * *"
      - type: weekly
        retentionDays: 30
        schedule: "0 3 * * 0"
    connectionInfo:
      host: "postgres.example.com"
      username: "backup_user"
      password: "secure_password"
      database: "production_db"
      port: 5432
    storage:
      backend: "azure"
      azure:
        storageAccount: "mystorageaccount"
        storageKey: "mybase64encodedkey..."
        container: "db-backups"
        prefix: "postgresql/production"
    extraArgs: "--no-owner --no-acl"

  - type: mysql
    version: "8.0"
    periodicity:
      - type: daily
        retentionDays: 14
        schedule: "0 1 * * *"
    connectionInfo:
      host: "mysql.example.com"
      username: "backup_user"
      password: "secure_password"
      database: "app_db"
      port: 3306
    storage:
      backend: "azure"
      azure:
        storageAccount: "mystorageaccount"
        storageKey: "mybase64encodedkey..."
        container: "db-backups"
        prefix: "mysql/app"
    extraArgs: "--single-transaction --routines"
```

#### MongoDB Configuration

```yaml
databases:
  - type: mongodb
    periodicity:
      - type: daily
        retentionDays: 7
        schedule: "0 2 * * *"  # Daily at 2:00 AM
    connectionInfo:
      host: "mongo.example.com"
      username: "backup_user"
      password: "secure_password"
      database: "app_db"
      port: 27017
    aws:
      region: "us-east-1"
      bucket: "my-db-backups"
      bucketPrefix: "mongodb/app"
    extraArgs: "--authenticationDatabase=admin"  # Adjust if auth DB differs
```

MongoDB backup notes:
- Uses `mongodump` to create a compressed archive (`dump_restore.archive.gz`).
- Set `extraArgs` for cluster URIs (e.g., `--uri="mongodb+srv://..."`).
- For SCRAM auth, ensure `--authenticationDatabase` matches your setup (often `admin`).
- Grant the backup user `read` on target DB; cluster-wide backups may require broader roles.

#### ClickHouse Configuration

```yaml
databases:
  - type: clickhouse
    periodicity:
      - type: daily
        retentionDays: 15
        schedule: "0 4 * * *"
    connectionInfo:
      # Secret keys: host, username, password, database, port
      # database="" = full instance (databases + users/roles/grants/named collections)
      secretName: dumpscript-clickhouse-credentials
    objectStorage:
      backend: s3
      region: us-central1
      bucket: my-db-backups
      bucketPrefix: clickhouse/all
      secretName: dumpscript-gcs-credentials   # accessKeyId / secretAccessKey (static or HMAC)
      endpointUrl: https://storage.googleapis.com
    # Optional BACKUP settings
    extraArgs: "allow_s3_native_copy=0"
    # Optional CLICKHOUSE_* variables (chart >= 1.9.0)
    extraEnv:
      - name: CLICKHOUSE_ARCHIVE_FORMAT
        value: tar.zst
    # No extraVolumes: the server uploads directly to the bucket
```

ClickHouse backup notes:
- The backup runs **on the server**: it needs network egress to the storage endpoint and receives the storage credentials inside the `BACKUP` statement (masked in `system.backups`, `system.backup_log` and `query_log`).
- Temporary AWS credentials (IRSA / `AWS_SESSION_TOKEN`) are **not** accepted by the S3 backup engine. Use a static key (or GCS HMAC key), or set `CLICKHOUSE_USE_SERVER_CREDENTIALS=true` and configure the credentials on the server.
- The backup is taken on the replica behind `DB_HOST`. With `Replicated` databases / `ReplicatedMergeTree` the metadata snapshot is consistent (taken from Keeper) and the data is what that replica holds; `ON CLUSTER` is not used because archives are not supported with it.
- Restore is manual for now: see [ClickHouse restore](#clickhouse-restore-manual).

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

#### Mixed Backends (S3 + Azure)

```yaml
databases:
  # Production database on AWS → S3
  - type: postgresql
    version: "17"
    periodicity:
      - type: daily
        retentionDays: 30
        schedule: "0 1 * * *"
    connectionInfo:
      host: "prod-postgres.aws.example.com"
      username: "backup_user"
      password: "secure_password"
      database: "production_db"
    aws:
      region: "us-east-1"
      bucket: "aws-backups"
      bucketPrefix: "production/postgres"

  # Staging database on Azure → Azure Blob Storage
  - type: postgresql
    version: "16"
    periodicity:
      - type: daily
        retentionDays: 14
        schedule: "0 2 * * *"
    connectionInfo:
      host: "staging-postgres.azure.example.com"
      username: "backup_user"
      password: "secure_password"
      database: "staging_db"
    storage:
      backend: "azure"
      azure:
        storageAccount: "mystorageaccount"
        storageKey: "mybase64encodedkey..."
        container: "azure-backups"
        prefix: "staging/postgres"
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

1. **Runtime Installation**: When the container starts, it reads the `POSTGRES_VERSION`, `MYSQL_VERSION` or `MARIADB_VERSION` environment variables (`clickhouse` installs nothing: it only needs `curl`)
2. **Dynamic Client Installation**: The appropriate database client is installed using Alpine's package manager
3. **Version Verification**: The installation is verified and client version is logged
4. **Database Operations**: The original dump/restore scripts are executed with the correct client version
5. **Multiple Schedules**: Each database can have multiple backup schedules with different retention policies
6. **Storage Upload**: The dump is uploaded to the configured storage backend using [rclone](https://rclone.org/), which handles multipart uploads, retries, and chunked transfers automatically. For `clickhouse` the **server** writes the archive to the bucket (`BACKUP ... TO S3()`/`AzureBlobStorage()` in `ASYNC` mode); the job polls `system.backups`, then confirms the object exists with rclone
7. **Path Structure**: Backups are stored at `<prefix>/<periodicity>/<year>/<month>/<day>/<dump_file>` (e.g., `daily/2025/03/24/dump_20250324_120000.sql.gz`; ClickHouse: `dump_20250324_120000.tar.zst`)
8. **Notifications**: Optional Slack notifications for backup status

## Security Notes

- Credentials never go on a command line: storage keys reach `rclone` through `RCLONE_*` environment variables, the MongoDB password through a private `--config` file, the ClickHouse password through a curl config file (mode 600) and the Slack webhook URL through curl's stdin config. Nothing sensitive shows up in `ps`/`/proc/*/cmdline`.
- Values that could carry credentials (`DUMP_OPTIONS`, ClickHouse server errors) are masked before being logged.
- `DB_NAME` (and ClickHouse database lists) are validated as plain identifiers; ClickHouse `DUMP_OPTIONS` must be `setting=value` pairs.
- Retention runs only after a successful upload and always keeps the `RETENTION_MIN_KEEP` most recent objects, so a broken dump can never wipe the history.
- Known gaps (see the security review in the repository issues/PRs): HTTP without TLS by default for ClickHouse (`CLICKHOUSE_SECURE=true` when the network is not trusted), restore trusts the archive content (`psql`/`mysql` meta-commands), dump image runs as root and installs clients at runtime.

## Storage Requirements

### Critical: Sufficient Storage in `/dumpscript` Directory

The DumpScript container uses the `/dumpscript` directory as its working directory and **temporary storage** for database dumps before uploading. It is **critical** to ensure this directory has sufficient storage space to accommodate the full size of your database dump.

#### Why Storage Space Matters

- **Temporary Storage**: Database dumps are created locally in `/dumpscript` before being compressed and uploaded
- **Compression Process**: The dump is compressed using gzip, which requires additional temporary space during compression
- **No Streaming**: The current implementation creates the complete dump file locally before uploading (not streaming)
- **Failure Risk**: Insufficient space will cause the backup process to fail with "No space left on device" errors

#### Storage Space Calculation

**Minimum Required Space:**
```
Required Space = Database Size × 1.5
```

**Recommended Space:**
```
Recommended Space = Database Size × 2.0
```

#### Examples

| Database Size | Minimum Space | Recommended Space |
|---------------|---------------|-------------------|
| 1 GB          | 1.5 GB        | 2 GB              |
| 10 GB         | 15 GB         | 20 GB             |
| 100 GB        | 150 GB        | 200 GB            |
| 500 GB        | 750 GB        | 1 TB              |

#### Kubernetes Configuration

When deploying via Helm chart, ensure your pod has sufficient storage:

```yaml
# Example: Using emptyDir with size limit
volumeMounts:
  - name: data
    mountPath: /dumpscript
volumes:
  - name: data
    emptyDir:
      sizeLimit: 20Gi  # Adjust based on your database size
```

#### Docker Configuration

When running with Docker, ensure the container has access to sufficient storage:

```bash
# Using tmpfs with size limit
docker run --tmpfs /dumpscript:rw,size=20g ...

# Using volume mount with size limit
docker run -v /host/storage:/dumpscript:rw ...
```

#### Monitoring Storage Usage

The container includes debug logs to monitor storage usage:

```
[DEBUG] Available space in /dumpscript: Filesystem      Size  Used Avail Use% Mounted on
[DEBUG] Available space in /dumpscript: tmpfs           20G   1.2G   19G   6% /dumpscript
```

#### Troubleshooting Storage Issues

If you encounter storage-related failures:

1. **Check available space**: Look for `[DEBUG] Available space in /dumpscript` in logs
2. **Monitor during backup**: Watch space usage during the dump process
3. **Increase storage**: Add more storage to the `/dumpscript` directory
4. **Consider database size**: Large databases may require significant temporary storage

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
- `docker/scripts/dump_db_to_s3.sh` - Database dump script
- `docker/scripts/restore_db_from_s3.sh` - Database restore script
- `docker/scripts/clickhouse_backup.sh` - ClickHouse native (server-side) backup driver
- `docker/scripts/storage_utils.sh` - Unified storage abstraction (S3 and Azure Blob Storage)
- `docker/scripts/install_db_clients.sh` - Dynamic client installation script
- `docker/scripts/entrypoint_dump.sh` - Dump container entrypoint
- `docker/scripts/entrypoint_restore.sh` - Restore container entrypoint
- `docker/scripts/notify_slack.sh` - Slack notification script

## AWS IAM Policy Requirements

To allow DumpScript to upload, list, and delete backups in your S3 bucket, the IAM role used by the container must have the following permissions:

- `s3:GetObject` – Read backup files from S3 (for restore)
- `s3:PutObject` – Upload new backup files to S3
- `s3:DeleteObject` – Remove old backups from S3 (for retention policy)
- `s3:ListBucket` – List objects in the S3 bucket (for cleanup and restore)
- `s3:ListObjects`, `s3:ListObjectsV2` – List objects within a bucket and support recursive listing

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

Replace `your-bucket-name` with the actual name of your S3 bucket.

## Azure RBAC Requirements

To allow DumpScript to upload, list, and delete backups in your Azure Blob Storage container, the identity (service principal, managed identity, or storage account key) must have the appropriate permissions.

### Using Storage Account Key or SAS Token

When using `AZURE_STORAGE_KEY`, full access is granted via the key itself — no additional RBAC configuration is needed.

When using `AZURE_STORAGE_SAS_TOKEN`, ensure the token has the following permissions:
- **Read** (`r`) – Download backup files for restore
- **Write** (`w`) – Upload new backup files
- **Delete** (`d`) – Remove old backups (retention policy)
- **List** (`l`) – List blobs in the container

### Using Azure RBAC (Workload Identity / Managed Identity)

Assign the **Storage Blob Data Contributor** role to the identity at the storage account or container level:

```bash
az role assignment create \
  --assignee "<principal-id>" \
  --role "Storage Blob Data Contributor" \
  --scope "/subscriptions/<sub-id>/resourceGroups/<rg>/providers/Microsoft.Storage/storageAccounts/<account>"
```

For Kubernetes with Azure Workload Identity, annotate the service account:

```yaml
serviceAccount:
  create: true
  annotations:
    azure.workload.identity/client-id: "<azure-client-id>"
```

## Full Instance Dump and Restore

### Full instance dump (DB_NAME omitted)

- `MySQL/MariaDB`: use `--all-databases` with `mysqldump`/`mariadb-dump`.
- `PostgreSQL`: use `pg_dumpall` for all databases, roles, and tablespaces.
- `MongoDB`: omit `--db` in `mongodump` to dump the entire instance.
- `ClickHouse`: `BACKUP TABLE system.users, system.roles, system.settings_profiles, system.row_policies, system.quotas, system.functions, system.named_collections, ALL EXCEPT DATABASES system, information_schema, INFORMATION_SCHEMA` (databases plus SQL-managed access entities). Tune with `CLICKHOUSE_EXCLUDE_DATABASES` / `CLICKHOUSE_BACKUP_ACCESS_ENTITIES`.

Required privileges depend on the engine. Ensure the user can list and read all databases.

### Full instance restore (DB_NAME omitted)

- `MySQL/MariaDB`: import directly into the server without selecting a database (`mysql`/`mariadb` reading the file). If the dump was generated with `--all-databases`, it will include creation and data for all databases.
- `PostgreSQL`: use `psql -d postgres` to apply `pg_dumpall` (roles, tablespaces, and all databases). Requires elevated privileges.
- `MongoDB`: omit `--db` in `mongorestore` to restore the entire instance.

For full instance restores, `CREATE_DB` only has an effect when `DB_NAME` is defined.

## ClickHouse

### Grants for the backup user

```sql
CREATE USER backup IDENTIFIED WITH sha256_password BY '<password>';
GRANT BACKUP, SHOW ON *.* TO backup;
GRANT SELECT ON system.backups TO backup;      -- polling the ASYNC status
GRANT READ, WRITE ON S3 TO backup;             -- destination S3()/GCS; ClickHouse < 25.7: GRANT S3 ON *.*
-- Azure destination: GRANT AZURE ON *.* TO backup;
-- Only with CLICKHOUSE_CLUSTER (polling every replica via clusterAllReplicas): GRANT REMOTE ON *.* TO backup;
```

Also allow egress from the ClickHouse pods to the storage endpoint (NetworkPolicy / firewall) and, in Kubernetes, the DumpScript namespace to reach the ClickHouse HTTP port.

### ClickHouse incremental backups

Set `CLICKHOUSE_BASE_BACKUP_PERIODICITY` to the periodicity that holds the full backups. Runs of that
periodicity are full; runs of any other periodicity look up the **newest archive of the base periodicity**
under the same prefix and send `SETTINGS base_backup = S3('<base url>'), use_same_s3_credentials_for_base_backup = 1`
(Azure: `base_backup = AzureBlobStorage(...)`). Example with the Helm chart: `weekly` (full, Sunday) +
`daily` (incremental, Monday–Saturday), both with `CLICKHOUSE_BASE_BACKUP_PERIODICITY=weekly`.

- Every incremental is taken against the latest **full**, never against the previous incremental: a
  restore needs at most two archives (the full and the chosen incremental).
- **Retention rule:** the base periodicity must keep its archives at least as long as the incremental
  periodicity **plus one full interval**, otherwise an incremental can outlive the base it depends on
  (e.g. weekly `RETENTION_DAYS=42` for daily `RETENTION_DAYS=35`). Bucket lifecycle rules must not
  expire objects before that either.
- If no base archive exists yet (first run, or the base was deleted), the job takes a **full** backup
  and logs a warning instead of failing.
- The base reference is written by ClickHouse into the `.backup` metadata of the incremental archive
  **without** the storage credentials (they are reused from the destination), so nothing sensitive
  lands in the bucket, the job log or `system.query_log`.
- Restoring from an incremental: point `RESTORE` at the incremental archive only and pass
  `SETTINGS use_same_s3_credentials_for_base_backup = 1` so the server reads the base with the same key:

```sql
RESTORE DATABASE analytics AS analytics_restored
  FROM S3('https://storage.googleapis.com/<bucket>/clickhouse/all/daily/2026/09/30/dump_20260930_040000.tar.zst', '<key>', '<secret>')
  SETTINGS use_same_s3_credentials_for_base_backup = 1;
```

### ClickHouse restore (manual)

The restore image does not automate ClickHouse yet. Run the statement on the server (any replica) with a user that can create databases/tables and insert:

```sql
-- restore into the original name (target must be empty or absent)
RESTORE DATABASE analytics
  FROM S3('https://storage.googleapis.com/<bucket>/clickhouse/analytics/daily/2026/09/29/dump_20260929_040000.tar.zst', '<key>', '<secret>');

-- restore next to the original, for inspection
RESTORE DATABASE analytics AS analytics_restored FROM S3('<same url>', '<key>', '<secret>');

-- full-instance archive: everything, including users/roles/grants
RESTORE ALL FROM S3('<url of the full-instance archive>', '<key>', '<secret>')
  SETTINGS allow_non_empty_tables = 1;   -- only if you really want to append into existing tables
```

Useful settings: `structure_only=1`, `allow_different_database_def=1`, `allow_different_table_def=1`, `create_database='if not exists'`. Progress in `system.backups` (add `ASYNC` for long restores).

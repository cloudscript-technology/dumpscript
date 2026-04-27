# dumpscript — Documentation

Full reference for every feature. Each page has an overview, relevant
environment variables, worked examples, and troubleshooting notes.

---

## Getting started

- [**Quick start**](./quickstart.md) — your first dump in 5 minutes
- [**Configuration reference**](./configuration.md) — every environment variable
- [**Architecture**](./architecture.md) — design patterns, component diagram,
  pipeline sequence

---

## Commands

- [`dump`](./commands/dump.md) — dump a live database and upload
- [`restore`](./commands/restore.md) — download a dump and apply it
- [`cleanup`](./commands/cleanup.md) — retention-based deletion of old backups

---

## Engines (13)

| Engine | Dump | Restore | Verifier | Docs |
|---|---|---|---|---|
| PostgreSQL | `pg_dump` / `pg_dumpall` | `psql` | footer | [postgres](./engines/postgres.md) |
| MySQL | `mariadb-dump` | `mariadb` | footer | [mysql](./engines/mysql.md) |
| MariaDB | `mariadb-dump` | `mariadb` | footer | [mariadb](./engines/mariadb.md) |
| MongoDB | `mongodump --archive --gzip` | `mongorestore` | magic `0x8199e26d` | [mongodb](./engines/mongodb.md) |
| CockroachDB | `psql + SHOW CREATE` | `psql` | footer | [cockroach](./engines/cockroach.md) |
| Redis | `redis-cli --rdb` | **unsupported** | RDB magic | [redis](./engines/redis.md) |
| SQL Server | `mssql-scripter` | `sqlcmd` | `GO` terminator | [sqlserver](./engines/sqlserver.md) |
| Oracle | `exp` | `imp` | `EXPORT:V` | [oracle](./engines/oracle.md) |
| Elasticsearch | scroll API (pure Go) | `_bulk` API | NDJSON tail | [elasticsearch](./engines/elasticsearch.md) |
| etcd | `etcdctl snapshot save` | **unsupported** | BoltDB magic | [etcd](./engines/etcd.md) |
| ClickHouse | `clickhouse-client FORMAT Native` | `INSERT FORMAT Native` | envelope | [clickhouse](./engines/clickhouse.md) |
| Neo4j | `neo4j-admin database dump` | `neo4j-admin database load` | envelope | [neo4j](./engines/neo4j.md) |
| SQLite | `sqlite3 .dump` | `sqlite3` | `COMMIT;` | [sqlite](./engines/sqlite.md) |

See [engines index](./engines/README.md) for the full matrix and picker.

---

## Storage backends

- [Overview](./storage/README.md) — S3 vs Azure vs GCS; key layout
- [S3 (AWS / MinIO / S3-compatible)](./storage/s3.md)
- [Azure Blob (real cloud + Azurite)](./storage/azure.md)
- [Google Cloud Storage (native + Workload Identity)](./storage/gcs.md)

---

## Kubernetes operator

- [Operator overview](./operator/README.md) — CRDs `BackupSchedule` + `Restore`
- [`BackupSchedule` reference](./operator/backupschedule.md) — full spec/status/lifecycle
- [`Restore` reference](./operator/restore.md) — one-shot restore CR
- [Secret refs catalog](./operator/secret-refs.md) — every `*SecretRef` field

---

## Features

- [Distributed locking](./features/locking.md) — day-level `.lock` object
- [Content verification](./features/verification.md) — per-engine truncation detection
- [Upload integrity check](./features/integrity.md) — post-upload SHA-256/CRC32C/size verification
- [Retention](./features/retention.md) — age-based cleanup
- [Notifiers](./features/notifiers.md) — Slack / Discord / Teams / Webhook / Stdout
- [Slack notifications (deep-dive)](./features/slack_notifications.md) — start/success/failure/skipped
- [Prometheus metrics](./features/prometheus.md) — Pushgateway + stderr
- [Logging](./features/logging.md) — pretty console + JSON modes

---

## Operations

- [Kubernetes / CronJob](./operations/kubernetes.md) — recommended deployment
- [Docker image](./operations/docker_image.md) — build flags, sizes, pinning
- [Testing](./operations/testing.md) — unit + e2e workflows

---

## Development

- [Adding a new engine](./development/adding_an_engine.md)

---

## Index by topic

| Want to… | Go to |
|---|---|
| Dump a fresh DB today | [Quick start](./quickstart.md) |
| Understand every env var | [Configuration reference](./configuration.md) |
| Pick the right engine | [Engines matrix](./engines/README.md) |
| Run against MinIO / LocalStack | [S3 backend](./storage/s3.md) |
| Deploy to EKS with IRSA | [Kubernetes guide](./operations/kubernetes.md) |
| Understand why the lock never orphans forever | [Locking](./features/locking.md) |
| Add a new database engine | [Adding an engine](./development/adding_an_engine.md) |

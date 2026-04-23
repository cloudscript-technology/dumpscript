# Configuration reference

Every knob `dumpscript` understands. All configuration is via **environment
variables** (parsed with [`envconfig`](https://github.com/kelseyhightower/envconfig)).
There are no config files or CLI flags for runtime settings — only two
meta-flags:

```
--log-level    override LOG_LEVEL (debug|info|warn|error)
--log-format   override LOG_FORMAT (json|console)
```

---

## Database — required

| Variable | Default | Description |
|---|---|---|
| `DB_TYPE` | — | One of `postgresql`, `mysql`, `mariadb`, `mongodb`, `cockroach`, `redis`, `sqlserver`, `oracle`, `elasticsearch`, `etcd`, `clickhouse`, `neo4j`, `sqlite` |
| `DB_HOST` | — | Host/address (not required for `sqlite`) |
| `DB_PORT` | per-engine default (below) | TCP port |
| `DB_USER` | — (optional for `redis`, `etcd`, `elasticsearch`, `sqlite`) | Username / role |
| `DB_PASSWORD` | — | Password; empty allowed |
| `DB_NAME` | empty | Database / index name (semantics vary per engine — see the engine's doc) |
| `DUMP_OPTIONS` | empty | Extra raw tokens forwarded to the engine CLI (or consumed by the dumper itself — e.g. `--scheme=https` for ES/etcd) |
| `CREATE_DB` | `false` | Restore: create the DB before applying the dump (Postgres/MySQL/MariaDB) |

### Default ports per engine

| Engine | Port |
|---|---|
| postgresql | 5432 |
| mysql | 3306 |
| mariadb | 3306 |
| mongodb | 27017 |
| cockroach | 26257 |
| redis | 6379 |
| sqlserver | 1433 |
| oracle | 1521 |
| elasticsearch | 9200 |
| etcd | 2379 |
| clickhouse | 9000 |
| neo4j | 7687 |
| sqlite | n/a (file-based) |

---

## Pipeline / behaviour

| Variable | Default | Description |
|---|---|---|
| `PERIODICITY` | — | `daily`, `weekly`, `monthly`, `yearly`; required for `dump` |
| `RETENTION_DAYS` | `0` (disabled) | Delete older backups before dumping |
| `WORK_DIR` | `/dumpscript` | Local scratch dir |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `json` | `json` or `console` — see [Logging](./features/logging.md) |
| `VERIFY_CONTENT` | `true` | Run per-engine content verifier post-dump |
| `DUMP_TIMEOUT` | `2h` | Hard ceiling on the dump process |
| `RESTORE_TIMEOUT` | `2h` | Hard ceiling on the restore process |

---

## Storage — selector

| Variable | Default | Description |
|---|---|---|
| `STORAGE_BACKEND` | `s3` | `s3` or `azure` |

---

## Storage — S3

Applies when `STORAGE_BACKEND=s3`.

| Variable | Default | Description |
|---|---|---|
| `AWS_REGION` | — | Region |
| `S3_BUCKET` | — | Bucket |
| `S3_PREFIX` | — | Key prefix (e.g. `postgresql-dumps`) |
| `AWS_ACCESS_KEY_ID` | — | Static key (skip if using IRSA) |
| `AWS_SECRET_ACCESS_KEY` | — | Static secret |
| `AWS_SESSION_TOKEN` | — | Temporary STS token |
| `AWS_ROLE_ARN` | — | Enables IRSA assume-role on EKS |
| `AWS_S3_ENDPOINT_URL` | — | MinIO/GCS override (e.g. `https://storage.googleapis.com`) |
| `S3_STORAGE_CLASS` | — | `STANDARD_IA`, `GLACIER`, etc. (AWS only) |
| `S3_KEY` | — | **Restore only** — object key to download |

Deep dive: [S3 backend](./storage/s3.md).

---

## Storage — Azure

Applies when `STORAGE_BACKEND=azure`.

| Variable | Default | Description |
|---|---|---|
| `AZURE_STORAGE_ACCOUNT` | — | Storage account name |
| `AZURE_STORAGE_KEY` | — | Shared key (or use SAS) |
| `AZURE_STORAGE_SAS_TOKEN` | — | SAS token alternative |
| `AZURE_STORAGE_CONTAINER` | — | Container |
| `AZURE_STORAGE_PREFIX` | falls back to `S3_PREFIX` | Blob prefix |
| `AZURE_STORAGE_ENDPOINT` | `https://<account>.blob.core.windows.net` | Override for Azurite / Azure gov |

Deep dive: [Azure backend](./storage/azure.md).

---

## Upload tuning (both backends)

| Variable | Default | Description |
|---|---|---|
| `STORAGE_UPLOAD_CUTOFF` | `200M` | Size threshold for multipart/block upload |
| `STORAGE_CHUNK_SIZE` | `100M` | Multipart chunk size |
| `STORAGE_UPLOAD_CONCURRENCY` | `4` | Parallel upload workers |

Accepts `k`, `M`, `G` suffixes (e.g. `512M`, `2G`).

---

## Slack

| Variable | Default | Description |
|---|---|---|
| `SLACK_WEBHOOK_URL` | — | Incoming webhook URL |
| `SLACK_CHANNEL` | `#alerts` | Channel override |
| `SLACK_USERNAME` | `DumpScript Bot` | Bot display name |
| `SLACK_NOTIFY_SUCCESS` | `false` | Emit `EventSuccess` in addition to failures |

Deep dive: [Slack notifications](./features/slack_notifications.md).

---

## Prometheus

| Variable | Default | Description |
|---|---|---|
| `PROMETHEUS_ENABLED` | `false` | Emit metrics at exit |
| `PROMETHEUS_PUSHGATEWAY_URL` | — | Pushgateway endpoint (optional) |
| `PROMETHEUS_JOB_NAME` | `dumpscript` | Job label |
| `PROMETHEUS_INSTANCE` | hostname | Instance label |
| `PROMETHEUS_LOG_ON_EXIT` | `false` | Dump the metrics text to stderr as well |

Deep dive: [Prometheus metrics](./features/prometheus.md).

---

## Validation rules

`dumpscript` rejects a bad config at startup with a specific error message.
Validation is split into two phases:

### `ValidateCommon` (runs for every subcommand)

- `DB_TYPE` must be one of the supported engines (listed above).
- `STORAGE_BACKEND` must be `s3` or `azure`.
- Backend-specific requirements (e.g. `S3_BUCKET` for s3, `AZURE_STORAGE_ACCOUNT` + credentials for azure).

### `ValidateDump` / `ValidateRestore`

- `PERIODICITY` required for `dump`.
- `S3_KEY` required for `restore`.
- **Per-engine connection checks** (`internal/config/config.go:validateConnection`):
  - `sqlite` → requires `DB_NAME` (file path).
  - `redis`, `etcd`, `elasticsearch` → require `DB_HOST` only (auth optional).
  - All other engines → require both `DB_HOST` and `DB_USER`.

---

## Putting it all together — an EKS example

```yaml
env:
  # engine
  - { name: DB_TYPE,      value: "postgresql" }
  - { name: DB_HOST,      value: "pg.prod.local" }
  - { name: DB_USER,      value: "backup" }
  - { name: DB_PASSWORD,  valueFrom: { secretKeyRef: { name: pg, key: pw } } }
  - { name: DB_NAME,      value: "app" }

  # scheduling
  - { name: PERIODICITY,  value: "daily" }
  - { name: RETENTION_DAYS, value: "30" }

  # storage via IRSA
  - { name: STORAGE_BACKEND, value: "s3" }
  - { name: AWS_REGION,      value: "us-east-1" }
  - { name: AWS_ROLE_ARN,    value: "arn:aws:iam::123456789012:role/dumpscript" }
  - { name: S3_BUCKET,       value: "prod-backups" }
  - { name: S3_PREFIX,       value: "pg" }

  # observability
  - { name: SLACK_WEBHOOK_URL, valueFrom: { secretKeyRef: { name: slack, key: url } } }
  - { name: SLACK_NOTIFY_SUCCESS, value: "true" }
  - { name: LOG_FORMAT, value: "json" }
  - { name: PROMETHEUS_ENABLED, value: "true" }
  - { name: PROMETHEUS_PUSHGATEWAY_URL, value: "http://pushgateway.monitoring.svc:9091" }
```

---

## Next

- [Architecture](./architecture.md)
- [Engine reference](./engines/README.md)
- [Storage backends](./storage/README.md)

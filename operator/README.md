# dumpscript-operator

Kubernetes operator for the [dumpscript](../) backup/restore binary.

It exposes two custom resources:

- **`BackupSchedule`** — a recurring backup driven by a cron expression. The
  controller materialises it into a managed `batch/v1` CronJob.
- **`Restore`** — a one-shot restore. The controller materialises it into a
  one-shot `batch/v1` Job and reflects terminal state back to
  `.status.phase`.

Every dumpscript binary feature has a first-class CRD field — no env-var
incantations required for the common path. Anything not yet promoted to a
typed field can still be passed via `spec.extraEnv`.

---

## Install

```sh
# CRDs
make install

# operator (with the published image)
make deploy IMG=ghcr.io/cloudscript-technology/dumpscript-operator:latest
```

Local dev (kind):

```sh
make docker-build IMG=localhost/dumpscript-operator:dev
kind load docker-image localhost/dumpscript-operator:dev
make deploy IMG=localhost/dumpscript-operator:dev
```

---

## BackupSchedule reference

```yaml
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata:
  name: postgres-prod
  namespace: backups
spec:
  schedule: "0 2 * * *"          # cron expression
  periodicity: daily             # daily | weekly | monthly | yearly
  retentionDays: 30              # 0 disables retention
  suspend: false                 # pause without deleting

  # ── Image ───────────────────────────────────────────────────────────
  image: ghcr.io/cloudscript-technology/dumpscript:latest
  imagePullPolicy: IfNotPresent  # Always | Never | IfNotPresent
  imagePullSecrets:
    - name: ghcr-pull-secret

  # ── Database (which DB to dump) ────────────────────────────────────
  database:
    type: postgresql             # 13 engines supported (see below)
    host: postgres.prod.svc.cluster.local
    port: 5432                   # optional — operator defaults per engine
    name: app
    credentialsSecretRef:
      name: postgres-credentials
      usernameKey: username      # optional, default "username"
      passwordKey: password      # optional, default "password"
    options: "--no-owner"        # raw extra flags passed to the engine CLI
    optionsSecretRef:            # alternative: load DUMP_OPTIONS from a Secret
      name: pg-extra-flags
      key: dump-options
    postgresql:
      version: "17"              # informational; surfaced as POSTGRES_VERSION
    mysql:
      version: "8.0"             # MYSQL_VERSION
    mariadb:
      version: "11.4"            # MARIADB_VERSION
    mongodb:
      authSource: admin          # → --authenticationDatabase=admin
    volume:                      # mount a Volume into the dumpscript pod (sqlite)
      mountPath: /data
      persistentVolumeClaim:
        claimName: sqlite-data

  # ── Storage (where the dump goes) ──────────────────────────────────
  storage:
    backend: s3                  # s3 | gcs | azure
    s3:
      bucket: prod-backups
      prefix: postgres/
      region: us-east-1
      endpointURL: ""            # MinIO / non-AWS override
      storageClass: STANDARD_IA
      roleARN: ""                # IRSA — leave empty for static keys
      credentialsSecretRef:
        name: aws-credentials
      sse: aws:kms               # AES256 | aws:kms
      sseKMSKeyID: arn:aws:kms:us-east-1:123:key/abc
    azure:
      account: prodbackups
      container: pg
      prefix: postgres/
      endpoint: ""               # leave empty in Azure cloud
      credentialsSecretRef:
        name: azure-credentials
        sharedKeyKey: AZURE_STORAGE_KEY
        sasTokenKey: AZURE_STORAGE_SAS_TOKEN
    gcs:
      bucket: prod-backups
      prefix: postgres/
      projectID: my-project
      endpoint: ""               # fake-gcs-server / on-prem only
      credentialsSecretRef:      # leave empty on GKE Workload Identity
        name: gcs-credentials
        keyFile: key.json
    uploadCutoff: 200M
    chunkSize: 100M
    concurrency: 4

  # ── Notifications (any subset) ─────────────────────────────────────
  notifications:
    notifySuccess: true          # also notify on success (default: failures only)
    stdout: true
    slack:
      webhookSecretRef: { name: slack, key: webhook-url }
      channel: "#backups"
      username: dumpscript
    discord:
      webhookSecretRef: { name: discord, key: webhook-url }
    teams:
      webhookSecretRef: { name: teams, key: webhook-url }
    webhook:
      urlSecretRef: { name: webhook, key: url }
      authHeaderSecretRef: { name: webhook, key: auth-header }

  # ── Runtime tuning (binary behavior) ───────────────────────────────
  dryRun: false                  # validate config + reachability, skip dump
  compression: gzip              # gzip | zstd
  dumpTimeout: 2h
  lockGracePeriod: 24h           # 0 disables stale-lock recovery
  verifyContent: true
  workDir: /dumpscript
  logLevel: info                 # debug | info | warn | error
  logFormat: json                # json | console
  metricsListen: ""              # ":9090" to enable /metrics in-pod
  dumpRetry:
    maxAttempts: 3
    initialBackoff: 5s
    maxBackoff: 5m
  prometheus:
    enabled: false
    pushgatewayURL: http://pushgateway.monitoring.svc:9091
    jobName: dumpscript
    instance: postgres-prod
    logOnExit: false

  # ── CronJob & Job tunables ─────────────────────────────────────────
  concurrencyPolicy: Forbid      # Forbid | Allow | Replace
  startingDeadlineSeconds: 300
  backoffLimit: 0                # produced Job's BackoffLimit
  activeDeadlineSeconds: 7200
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 7

  # ── Pod scheduling ─────────────────────────────────────────────────
  serviceAccountName: dumpscript-sa
  resources:
    requests: { cpu: "100m", memory: "256Mi" }
    limits:   { memory: "1Gi" }
  nodeSelector:
    workload: backup
  tolerations:
    - { key: backup-only, operator: Exists, effect: NoSchedule }
  affinity:
    podAntiAffinity: {}
  priorityClassName: low-priority
  extraEnv:
    - { name: HTTPS_PROXY, value: "http://proxy.corp:3128" }
```

### Status fields surfaced via `kubectl get backupschedule`

```text
NAME           SCHEDULE    ENGINE       BACKEND  SUSPENDED  READY  LAST-SUCCESS  AGE
postgres-prod  0 2 * * *   postgresql   s3       false      True   3h            7d
```

`-o wide` adds: `Periodicity`, `Retention`, `Last-Failure`, `Current-Run`,
`Last-Job`, `Last-Duration`, `Total-Runs`, `Failures`, `Reason`, `Message`.

`.status` carries: `lastScheduleTime`, `lastSuccessTime`, `lastFailureTime`,
`lastRetentionTime`, `lastJobName`, `lastDurationSeconds`, `totalRuns`,
`consecutiveFailures`, `currentRun`, `observedGeneration`, `conditions[]`
(standard `metav1.Condition` with `Type=Ready`).

---

## Restore reference

```yaml
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: Restore
metadata:
  name: postgres-restore
  namespace: backups
spec:
  sourceKey: postgres/daily/2026/04/27/dump_20260427_020000.sql.gz
  createDB: true                 # CREATE DATABASE before applying

  image: ghcr.io/cloudscript-technology/dumpscript:latest
  imagePullPolicy: IfNotPresent
  imagePullSecrets: [ { name: ghcr-pull-secret } ]
  serviceAccountName: dumpscript-sa

  database:                      # same shape as BackupSchedule.spec.database
    type: postgresql
    host: postgres.prod.svc.cluster.local
    name: app
    credentialsSecretRef: { name: postgres-credentials }

  storage:                       # same shape as BackupSchedule.spec.storage
    backend: s3
    s3:
      bucket: prod-backups
      region: us-east-1
      credentialsSecretRef: { name: aws-credentials }

  ttlSecondsAfterFinished: 86400 # GC the Job after 24h
  backoffLimit: 0
  activeDeadlineSeconds: 7200

  # Runtime tuning (mirror of BackupSchedule.spec, minus retry)
  dryRun: false
  compression: gzip              # codec of the source artifact (auto-detected)
  restoreTimeout: 2h
  verifyContent: true            # post-restore TCP reachability check
  workDir: /dumpscript
  logLevel: info
  logFormat: json
  metricsListen: ""
  prometheus:
    enabled: false
```

`kubectl get restore`:

```text
NAME              PHASE      ENGINE      SOURCE                            READY  STARTED  COMPLETED  AGE
postgres-restore  Succeeded  postgresql  postgres/daily/2026/04/27/...     True   2m       1m         3m
```

`-o wide` adds: `Job`, `Duration`, `Backend`, `Reason`, `Message`.

---

## Supported database engines

13 engines, listed in `database.type`:

| Engine | Backup | Restore | Notes |
|---|---|---|---|
| postgresql | ✅ | ✅ | `postgresql.version` informational |
| mysql | ✅ | ✅ | `mysql.version` informational |
| mariadb | ✅ | ✅ | shares mysql client tooling |
| mongodb | ✅ | ✅ | `mongodb.authSource` for auth-enabled clusters |
| cockroach | ✅ | ✅ | |
| clickhouse | ✅ | ✅ | `database.name = db.table` |
| sqlserver | ✅ | ✅ | requires `mssql-scripter` in image |
| oracle | ✅ | ✅ | requires Oracle instantclient |
| neo4j | ✅ | ✅ | |
| redis | ✅ | ❌ unsupported | RDB snapshot |
| etcd | ✅ | ❌ unsupported | snapshot via etcdctl |
| elasticsearch | ✅ | ✅ | scroll-based |
| sqlite | ✅ | ✅ | needs `database.volume` |

Default ports (applied automatically when `database.port=0`):
postgres=5432, mysql/mariadb=3306, mongodb=27017, redis=6379, etcd=2379,
sqlserver=1433, oracle=1521, elasticsearch=9200, clickhouse=9000, neo4j=7687,
cockroach=26257.

---

## CRD field → env var mapping

The operator translates each typed field into the env var the dumpscript
binary expects (`internal/config/config.go`). Cheat sheet:

| CRD field | Env var |
|---|---|
| `spec.database.type` | `DB_TYPE` |
| `spec.database.host` | `DB_HOST` |
| `spec.database.port` | `DB_PORT` |
| `spec.database.name` | `DB_NAME` |
| `spec.database.options` | `DUMP_OPTIONS` |
| `spec.database.optionsSecretRef` | `DUMP_OPTIONS` (from Secret) |
| `spec.database.credentialsSecretRef` | `DB_USER` + `DB_PASSWORD` (from Secret) |
| `spec.database.mongodb.authSource` | appended to `DUMP_OPTIONS` |
| `spec.database.postgresql.version` | `POSTGRES_VERSION` |
| `spec.database.mysql.version` | `MYSQL_VERSION` |
| `spec.database.mariadb.version` | `MARIADB_VERSION` |
| `spec.periodicity` | `PERIODICITY` |
| `spec.retentionDays` | `RETENTION_DAYS` |
| `spec.dryRun` | `DRY_RUN` |
| `spec.compression` | `COMPRESSION_TYPE` |
| `spec.dumpTimeout` | `DUMP_TIMEOUT` |
| `spec.restoreTimeout` (Restore) | `RESTORE_TIMEOUT` |
| `spec.lockGracePeriod` | `LOCK_GRACE_PERIOD` |
| `spec.workDir` | `WORK_DIR` |
| `spec.logLevel` | `LOG_LEVEL` |
| `spec.logFormat` | `LOG_FORMAT` |
| `spec.verifyContent` | `VERIFY_CONTENT` |
| `spec.metricsListen` | `METRICS_LISTEN` |
| `spec.dumpRetry.maxAttempts` | `DUMP_RETRIES` |
| `spec.dumpRetry.initialBackoff` | `DUMP_RETRY_BACKOFF` |
| `spec.dumpRetry.maxBackoff` | `DUMP_RETRY_MAX_BACKOFF` |
| `spec.prometheus.enabled` | `PROMETHEUS_ENABLED` |
| `spec.prometheus.pushgatewayURL` | `PROMETHEUS_PUSHGATEWAY_URL` |
| `spec.prometheus.jobName` | `PROMETHEUS_JOB_NAME` |
| `spec.prometheus.instance` | `PROMETHEUS_INSTANCE` |
| `spec.prometheus.logOnExit` | `PROMETHEUS_LOG_ON_EXIT` |
| `spec.storage.backend` | `STORAGE_BACKEND` |
| `spec.storage.s3.bucket` | `S3_BUCKET` |
| `spec.storage.s3.prefix` | `S3_PREFIX` |
| `spec.storage.s3.region` | `AWS_REGION` |
| `spec.storage.s3.endpointURL` | `AWS_S3_ENDPOINT_URL` |
| `spec.storage.s3.storageClass` | `S3_STORAGE_CLASS` |
| `spec.storage.s3.roleARN` | `AWS_ROLE_ARN` (+ projected token) |
| `spec.storage.s3.sse` | `S3_SSE` |
| `spec.storage.s3.sseKMSKeyID` | `S3_SSE_KMS_KEY_ID` |
| `spec.storage.s3.credentialsSecretRef` | `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` (from Secret) |
| `spec.storage.azure.*` | `AZURE_STORAGE_*` (account/container/prefix/endpoint/key/sastoken) |
| `spec.storage.gcs.*` | `GCS_*` (bucket/prefix/projectID/endpoint/credentialsFile) |
| `spec.storage.uploadCutoff` | `STORAGE_UPLOAD_CUTOFF` |
| `spec.storage.chunkSize` | `STORAGE_CHUNK_SIZE` |
| `spec.storage.concurrency` | `STORAGE_UPLOAD_CONCURRENCY` |
| `spec.notifications.slack.*` | `SLACK_*` |
| `spec.notifications.discord.*` | `DISCORD_*` |
| `spec.notifications.teams.*` | `TEAMS_WEBHOOK_URL` |
| `spec.notifications.webhook.*` | `WEBHOOK_*` |
| `spec.notifications.stdout` | `NOTIFY_STDOUT` |
| `spec.extraEnv` | merged into the container env (operator-managed values win on collision) |

For a deeper env-var reference and validation rules, see
[`docs/configuration.md`](../docs/configuration.md).

---

## Operator-emitted Prometheus metrics

The operator manager itself exposes these on its `/metrics` endpoint
(`controller-manager-metrics-service:8443/metrics`):

| Metric | Type | Labels | Source |
|---|---|---|---|
| `dumpscript_backup_total` | Counter | namespace, schedule, engine, result | bumped on observed terminal Job |
| `dumpscript_backup_duration_seconds` | Histogram | namespace, schedule, engine, result | Job start→completion |
| `dumpscript_restore_total` | Counter | namespace, restore, engine, result | bumped on observed terminal Restore Job |
| `dumpscript_restore_duration_seconds` | Histogram | namespace, restore, engine, result | Job start→completion |

The dumpscript pod itself can additionally expose its own `/metrics` (with
`spec.metricsListen=":9090"`) or push to a Pushgateway (with
`spec.prometheus.*`). Pick the path that matches your topology.

---

## Events

The reconcilers emit Kubernetes Events on the CR object:

```sh
kubectl describe backupschedule postgres-prod
# Events:
#   Type    Reason            Age   From                       Message
#   Normal  Reconciled        5m    backupschedule-controller  created CronJob postgres-prod
#   Normal  LastRunSucceeded  3m    backupschedule-controller  backup job ...-1234 completed successfully
```

---

## Project layout

```
operator/
├── api/v1alpha1/                 # CRD types
├── cmd/                          # main.go
├── config/                       # kustomize bases (CRDs, RBAC, deploy, samples)
├── internal/
│   └── controller/
│       ├── backupschedule_controller.go
│       ├── builder.go            # CR → CronJob/Job materialisation
│       ├── metrics.go            # Prometheus collectors
│       └── restore_controller.go
└── test/e2e/                     # smoke tests (deploy + metrics endpoint)
```

The full integration tests for the BackupSchedule → CronJob → backup → S3
flow live at [`tests/kind-e2e/`](../tests/kind-e2e) (parent repo) — they
exercise 6 DBs × 3 storage backends and the operator together.

---

## License

Apache-2.0. See [`../LICENSE`](../LICENSE) for the full text.

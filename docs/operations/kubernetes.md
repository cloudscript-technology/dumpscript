# Kubernetes deployment

The recommended deployment shape is a `CronJob` per database per schedule.

---

## Minimal CronJob

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: pg-daily-backup
  namespace: platform-backup
spec:
  schedule: "0 2 * * *"          # 02:00 UTC every day
  concurrencyPolicy: Forbid       # defense-in-depth — lock also catches overlap
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 7
  jobTemplate:
    spec:
      backoffLimit: 0             # rely on the next scheduled run
      template:
        spec:
          restartPolicy: OnFailure
          serviceAccountName: dumpscript    # IRSA-annotated
          containers:
            - name: dumpscript
              image: localhost/dumpscript:go-alpine
              args: ["dump"]
              envFrom:
                - secretRef: { name: dumpscript-pg-env }
              resources:
                requests: { cpu: 100m,  memory: 128Mi }
                limits:   { cpu: 1,     memory: 1Gi  }
```

---

## IRSA ServiceAccount (EKS)

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: dumpscript
  namespace: platform-backup
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/dumpscript
```

IAM policy: see [S3 backend](../storage/s3.md#aws--irsa-on-eks-recommended).

---

## Secret for env

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: dumpscript-pg-env
stringData:
  DB_TYPE: postgresql
  DB_HOST: pg.prod.svc
  DB_USER: backup
  DB_PASSWORD: "s3cret"      # ideally from an External Secret / SealedSecret
  DB_NAME: app
  PERIODICITY: daily
  RETENTION_DAYS: "30"
  STORAGE_BACKEND: s3
  AWS_REGION: us-east-1
  AWS_ROLE_ARN: arn:aws:iam::123456789012:role/dumpscript
  S3_BUCKET: prod-backups
  S3_PREFIX: postgresql-dumps
  SLACK_WEBHOOK_URL: "https://hooks.slack.com/services/…"
  SLACK_NOTIFY_SUCCESS: "true"
  LOG_FORMAT: json
  PROMETHEUS_ENABLED: "true"
  PROMETHEUS_PUSHGATEWAY_URL: http://pushgateway.monitoring.svc:9091
```

Use `ExternalSecrets`, `SealedSecrets`, or a SOPS pipeline to keep
`DB_PASSWORD` out of plaintext.

---

## Resource hints by engine

| Engine | CPU request | Memory limit | Notes |
|---|---|---|---|
| postgres (small) | 100m | 512Mi | 1–10 GB DBs |
| postgres (large) | 500m | 2Gi | multi-GB dumps — bump `STORAGE_CHUNK_SIZE=512M` too |
| mysql / mariadb | 100m | 512Mi | Similar profile to postgres |
| mongo | 200m | 1Gi | `mongodump` is memory-heavy for large collections |
| redis | 50m | 128Mi | RDB snapshot is tiny |
| elasticsearch | 200m | 512Mi | Scroll keeps server-side state; not memory-heavy on the dumpscript side |
| etcd | 50m | 128Mi | Snapshots are small |
| sqlite | 50m | 128Mi | Local file, zero network |

---

## Preventing overlap

Two layers:

1. **`concurrencyPolicy: Forbid`** on the CronJob — Kubernetes itself skips
   a fire if the previous pod is still running.
2. **Distributed `.lock` object** — catches overlap across clusters / nodes
   where the CronJob controller can't see the other side.

You want both.

---

## Cross-region DR shape

One bucket, two regions:

```
us-east (CronJob) ─┐
                   ├─► s3://prod-backups/pg/daily/…  (bucket in us-east-1, replicated to us-west-2)
eu-west (CronJob) ─┘     (lock object keeps them from double-dumping)
```

Or separate buckets per region and an explicit cross-region sync (cheaper,
less magic).

---

## Restore as a Job

Restore is a one-shot Job, not a CronJob:

```yaml
apiVersion: batch/v1
kind: Job
metadata: { name: pg-restore-today }
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: dumpscript
          image: localhost/dumpscript:go-alpine
          args: ["restore"]
          envFrom:
            - secretRef: { name: dumpscript-pg-env }
          env:
            - { name: S3_KEY, value: "pg/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz" }
```

---

## Back

- [Docs home](../README.md)
- [S3 backend](../storage/s3.md)

# Quick start

Goal: take your first backup in under 5 minutes.

---

## Prerequisites

- Docker or Podman
- An S3-compatible bucket (AWS S3, MinIO, GCS, etc.) **or** an Azure Blob
  container — see [Storage backends](./storage/README.md) if you have neither
- A reachable database

---

## 1. Build the image

```sh
git clone https://github.com/cloudscript-technology/dumpscript
cd dumpscript
make image
```

The default image bundles `pg_dump 18`, `mariadb-dump 11.8`, `mongodb-tools`,
`redis-cli`, `sqlite3`, and `etcdctl`.

Want a smaller, pinned variant? See [Docker image](./operations/docker_image.md).

---

## 2. Run a PostgreSQL dump

```sh
podman run --rm \
  -e DB_TYPE=postgresql \
  -e DB_HOST=mydb.svc.cluster.local \
  -e DB_PORT=5432 \
  -e DB_USER=backup \
  -e DB_PASSWORD=secret \
  -e DB_NAME=app \
  -e STORAGE_BACKEND=s3 \
  -e AWS_REGION=us-east-1 \
  -e S3_BUCKET=my-backups \
  -e S3_PREFIX=postgresql-dumps \
  -e AWS_ACCESS_KEY_ID=... \
  -e AWS_SECRET_ACCESS_KEY=... \
  -e PERIODICITY=daily \
  -e RETENTION_DAYS=30 \
  localhost/dumpscript:go-alpine dump
```

The artefact lands at:

```
s3://my-backups/postgresql-dumps/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz
```

---

## 3. Verify it worked

```sh
aws s3 ls s3://my-backups/postgresql-dumps/daily/
aws s3 cp s3://my-backups/postgresql-dumps/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz - | \
  gunzip | head -20
```

You should see `pg_dump` header lines (`SET statement_timeout`, etc).

---

## 4. Restore

Same image, different subcommand:

```sh
podman run --rm \
  -e DB_TYPE=postgresql \
  -e DB_HOST=mydb.svc.cluster.local \
  -e DB_USER=backup \
  -e DB_PASSWORD=secret \
  -e DB_NAME=app \
  -e S3_BUCKET=my-backups \
  -e S3_PREFIX=postgresql-dumps \
  -e S3_KEY=postgresql-dumps/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz \
  localhost/dumpscript:go-alpine restore
```

---

## 5. Schedule in Kubernetes

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: postgres-daily-backup
spec:
  schedule: "0 2 * * *"        # 02:00 UTC every day
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: dumpscript
              image: localhost/dumpscript:go-alpine
              args: ["dump"]
              envFrom:
                - secretRef: { name: dumpscript-env }
```

Full manifest with IRSA, probes, and Prometheus scraping → see
[Kubernetes guide](./operations/kubernetes.md).

---

## What happens on each run

```
┌─ dump pipeline starting
├─ [1/4] preflight: verifying destination is reachable
│   ✔ destination reachable
├─ [2/4] acquire lock
│   ✔ lock acquired
├─ [3/4] dump + verify
│   ✔ dump produced (size=12.3 MiB in 4.2s)
│   ✔ gzip envelope valid
│   ✔ content verified (postgres footer found)
├─ [4/4] upload + notify
│   ✔ uploaded to s3://my-backups/postgresql-dumps/…
└─ dump pipeline complete (12.3 MiB in 6.1s)
```

Each stage is documented:

- Preflight + reachability: [Storage overview](./storage/README.md)
- Lock acquire/release semantics: [Locking](./features/locking.md)
- Content verify markers: [Verification](./features/verification.md)
- Retention cleanup (when `RETENTION_DAYS>0`): [Retention](./features/retention.md)
- Slack alerts on failure: [Slack](./features/slack_notifications.md)

---

## Common environment shapes

| Scenario | Minimal env vars |
|---|---|
| AWS S3 + EKS (IRSA) | `AWS_ROLE_ARN`, `AWS_REGION`, `S3_BUCKET`, `S3_PREFIX` |
| MinIO (local) | `AWS_S3_ENDPOINT_URL=http://minio:9000`, `AWS_ACCESS_KEY_ID/SECRET`, `S3_BUCKET` |
| GCS (HMAC) | `AWS_S3_ENDPOINT_URL=https://storage.googleapis.com`, HMAC keys, `S3_BUCKET` |
| Azure Blob | `STORAGE_BACKEND=azure`, `AZURE_STORAGE_ACCOUNT/KEY`, `AZURE_STORAGE_CONTAINER` |
| Azurite (local) | same as Azure + `AZURE_STORAGE_ENDPOINT=http://azurite:10000/devstoreaccount1` |

Full reference: [configuration](./configuration.md).

---

## Useful flags worth knowing

| Goal | Set |
|---|---|
| Validate config + reachability without touching the DB | `DRY_RUN=true` |
| Smaller dumps, ~2x faster | `COMPRESSION_TYPE=zstd` (gzip stays default) |
| Survive a transient network blip mid-dump | `DUMP_RETRIES=3` (default), tunable via `DUMP_RETRY_BACKOFF` / `DUMP_RETRY_MAX_BACKOFF` |
| Recover from a stale lock left by a crashed run | `LOCK_GRACE_PERIOD=24h` (default) |
| S3 server-side encryption | `S3_SSE=aws:kms` + optional `S3_SSE_KMS_KEY_ID` |
| Expose `/metrics` to a sidecar / mesh | `METRICS_LISTEN=:9090` |
| Preview retention deletes before they happen | `DRY_RUN=true` on `dumpscript cleanup` |

Inside the operator, every one of these has a typed CRD field — see
[`operator/README.md`](../operator/README.md) for the full reference.

## Next steps

- [Configuration reference](./configuration.md)
- [Pick your engine](./engines/README.md)
- [Kubernetes CronJob](./operations/kubernetes.md)

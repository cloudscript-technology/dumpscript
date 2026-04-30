# `cleanup` subcommand

Standalone retention sweep — deletes backup objects older than
`RETENTION_DAYS` **without** performing a dump.

```
dumpscript cleanup
```

Use this when you want retention management decoupled from dumps (e.g. a
daily cleanup Job that runs on a different node than the dumpers).

---

## What it does

```
┌─ cleanup starting
├─ List <prefix>/<periodicity>/
├─ For each object whose path-embedded YYYY/MM/DD is older than cutoff:
│     Delete(object)
└─ cleanup complete (deleted=N, kept=M, skipped=K)
```

Only matches files with dump-shaped names and extensions — `.lock` files and
unknown objects are preserved.

---

## Required environment variables

| Variable | Notes |
|---|---|
| `DB_TYPE` | Required for validation; a placeholder (`postgresql`) is fine |
| `DB_HOST` / `DB_USER` | Use dummy values — `cleanup` never connects to the DB |
| `STORAGE_BACKEND` | `s3` or `azure` |
| `S3_BUCKET` / `AZURE_STORAGE_CONTAINER` | Target |
| `S3_PREFIX` / `AZURE_STORAGE_PREFIX` | Prefix to sweep |
| `RETENTION_DAYS` | Required (`0` deletes nothing; a sweep with `0` is a no-op) |
| `PERIODICITY` | `daily` \| `weekly` \| `monthly` \| `yearly` (picks the sub-prefix) |

---

## Example — keep 30 daily dumps

```sh
podman run --rm \
  -e DB_TYPE=postgresql \
  -e DB_HOST=dummy -e DB_USER=dummy -e DB_PASSWORD=dummy \
  -e STORAGE_BACKEND=s3 \
  -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  -e S3_BUCKET=prod-backups \
  -e S3_PREFIX=postgresql-dumps \
  -e PERIODICITY=daily \
  -e RETENTION_DAYS=30 \
  localhost/dumpscript:go-alpine cleanup
```

---

## How the cutoff is computed

```
cutoff := today - RETENTION_DAYS
```

Where `today` is UTC. Only the **path-embedded** `YYYY/MM/DD` is compared —
never the object's `LastModified`. This keeps retention correct under:

- Storage-class transitions (S3 Glacier restore changes `LastModified`)
- Re-uploads of the same object
- Bucket replication

---

## Filename matcher

A file is considered a dump artefact (and therefore a candidate for deletion)
if its key matches any of these extensions:

- `*.sql.gz`
- `*.archive.gz`
- `*.rdb.gz`
- `*.dmp.gz`
- `*.db.gz`
- `*.native.gz`
- `*.neo4j.gz`
- `*.ndjson.gz`

Other objects (`.lock`, manifests, etc.) are skipped.

---

## Scheduling a weekly cleanup

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: backup-cleanup
spec:
  schedule: "30 4 * * 0"         # Sundays at 04:30 UTC
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: dumpscript
              image: localhost/dumpscript:go-alpine
              args: ["cleanup"]
              envFrom:
                - secretRef: { name: dumpscript-env }
```

---

## Related

- [`dump`](./dump.md) (runs retention inline when `RETENTION_DAYS > 0`)
- [Retention feature](../features/retention.md) — how the matcher works

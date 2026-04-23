# `dump` subcommand

Full dump → upload → notify workflow.

```
dumpscript dump
```

---

## What it does

```
┌─ dump pipeline starting (execution_id=…)
├─ [1/4] preflight: verify destination is reachable
├─ [2/4] acquire lock (day-level .lock)
│        (opt)  retention cleanup if RETENTION_DAYS > 0
├─ [3/4] dump + verify (envelope + per-engine content)
├─ [4/4] upload + notify
└─ dump pipeline complete
```

Every step logs with the shared `execution_id`, so you can trace a whole run
through structured logs.

---

## Required environment variables

| Variable | Notes |
|---|---|
| `DB_TYPE` | One of the 13 supported engines |
| `DB_HOST` | Except for `sqlite` |
| `DB_USER` | Required for SQL engines; optional for `redis`/`etcd`/`elasticsearch` |
| `DB_PASSWORD` | Usually required, empty allowed |
| `PERIODICITY` | `daily` \| `weekly` \| `monthly` \| `yearly` |
| `STORAGE_BACKEND` | `s3` or `azure` |
| Backend creds | See [configuration](../configuration.md) |

---

## Output key layout

```
<prefix>/<periodicity>/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.<ext>.gz
```

Extension per engine:

| Engine | Ext |
|---|---|
| postgres / mysql / mariadb / cockroach / sqlserver / sqlite | `.sql` |
| mongo | `.archive` |
| redis | `.rdb` |
| oracle | `.dmp` |
| elasticsearch | `.ndjson` |
| etcd | `.db` |
| clickhouse | `.native` |
| neo4j | `.neo4j` |

All are gzip-compressed (`.gz` suffix).

---

## Worked example — MariaDB on EKS

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: mariadb-weekly-backup
spec:
  schedule: "0 3 * * 0"          # Sunday 03:00 UTC
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: dumpscript       # IRSA role attached
          restartPolicy: OnFailure
          containers:
            - name: dumpscript
              image: localhost/dumpscript:go-alpine
              args: ["dump"]
              env:
                - { name: DB_TYPE,     value: "mariadb" }
                - { name: DB_HOST,     value: "mariadb.prod" }
                - { name: DB_USER,     value: "backup" }
                - { name: DB_PASSWORD, valueFrom: { secretKeyRef: { name: mariadb, key: pw } } }
                - { name: DB_NAME,     value: "app" }
                - { name: PERIODICITY, value: "weekly" }
                - { name: RETENTION_DAYS, value: "90" }
                - { name: STORAGE_BACKEND, value: "s3" }
                - { name: AWS_REGION,      value: "us-east-1" }
                - { name: S3_BUCKET,       value: "prod-backups" }
                - { name: S3_PREFIX,       value: "mariadb" }
                - { name: SLACK_WEBHOOK_URL, valueFrom: { secretKeyRef: { name: slack, key: url } } }
```

---

## Failure modes and what happens

| Situation | Outcome |
|---|---|
| Destination unreachable | Exit 1, Slack `failure` |
| Lock already held | Exit 0, Slack `skipped` (not a failure) |
| Dump process fails | Exit 1, Slack `failure`, lock released |
| Envelope invalid | Exit 1, file deleted, Slack `failure` |
| Per-engine verifier rejects | Exit 1, file deleted, Slack `failure` |
| Upload fails | Exit 1, local file kept (retry-able), Slack `failure` |
| Pod killed mid-dump | Lock orphans (manual cleanup) |

---

## Timeouts

| Variable | Default |
|---|---|
| `DUMP_TIMEOUT` | `2h` — total budget for dump step |

`dumpscript` uses `context.WithTimeout(cmd.Context(), DumpTimeout)`, so a
stuck `pg_dump` is SIGKILL'd automatically.

---

## Related

- [`restore`](./restore.md) — the inverse operation
- [`cleanup`](./cleanup.md) — standalone retention sweep
- [Locking](../features/locking.md)
- [Verification](../features/verification.md)

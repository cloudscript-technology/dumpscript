# SQLite

`DB_TYPE=sqlite` • no port (file-based) • extension `.sql.gz`

---

## How it works

- **Dump**: `sqlite3 <path> .dump` → plain ANSI SQL → gzip.
- **Restore**: `sqlite3 <path>` reading gunzipped SQL from stdin. Target
  file is created if missing (sqlite3 default behavior).
- **Verifier**: tail must end with `COMMIT;` after trailing-whitespace trim.

---

## Env vars

| Var | Notes |
|---|---|
| `DB_TYPE` | `sqlite` |
| `DB_NAME` | **required** — filesystem path to the `.sqlite` file |
| `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASSWORD` | unused |

The dumpscript container must have **read** access to the file for dump,
**write** access to the parent dir for restore. Typically achieved by a
bind-mount or volume shared with the application pod.

---

## Dump

```sh
podman run --rm \
  --mount type=bind,source=/var/app/data,target=/data \
  -e DB_TYPE=sqlite \
  -e DB_NAME=/data/app.sqlite \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=sqlite \
  -e PERIODICITY=daily -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  localhost/dumpscript:go-alpine dump
```

## Restore

```sh
podman run --rm \
  --mount type=bind,source=/var/app/data,target=/data \
  -e DB_TYPE=sqlite \
  -e DB_NAME=/data/app.sqlite \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=sqlite \
  -e S3_KEY=sqlite/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz \
  localhost/dumpscript:go-alpine restore
```

---

## Kubernetes deployment pattern

SQLite is typically used by a single-pod application with a PersistentVolume.
Run dumpscript as a sidecar CronJob with the same PVC:

```yaml
kind: CronJob
metadata: { name: sqlite-daily-backup }
spec:
  schedule: "0 3 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          volumes:
            - name: data
              persistentVolumeClaim: { claimName: app-data }
          containers:
            - name: dumpscript
              image: localhost/dumpscript:go-alpine
              args: ["dump"]
              volumeMounts:
                - { name: data, mountPath: /data, readOnly: true }
              env:
                - { name: DB_TYPE, value: sqlite }
                - { name: DB_NAME, value: /data/app.sqlite }
                # …
```

Note `readOnly: true` — for dump you only need read access.

---

## Tested versions

SQLite 3.x (all versions — the `.dump` output format is stable since 3.0).

---

## Tips

- **WAL mode**: `.dump` is consistent — it wraps everything in `BEGIN
  TRANSACTION;` / `COMMIT;`.
- **Very large DBs**: consider `sqlite3 .backup` (binary) instead of `.dump`
  (text). We use `.dump` because text + gzip is portable across all SQLite
  versions; `.backup` ties you to the minor version.
- **Concurrent writers**: `.dump` reads via a transaction — safe even during
  application writes. You might get a slightly stale snapshot.

---

## Back

- [Engines matrix](./README.md)
- [Docs home](../README.md)

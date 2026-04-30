# `restore` subcommand

Download a specific dump from storage and apply it to a live database.

```
dumpscript restore
```

---

## What it does

```
┌─ restore pipeline starting
├─ [1/3] download S3_KEY (or Azure equivalent) → /dumpscript/restore.gz
├─ [2/3] restore via engine-specific client (psql / mariadb / mongorestore / …)
└─ [3/3] notify success
```

No lock is taken on restore (no contention concern — the caller explicitly
picks the key). No retention touch.

---

## Required environment variables

| Variable | Notes |
|---|---|
| `DB_TYPE` | Must match the dumper that produced the artefact |
| `DB_HOST` | Except for `sqlite` |
| `DB_USER` / `DB_PASSWORD` | Per-engine rules |
| `DB_NAME` | For `sqlite`, the path to the target file |
| `STORAGE_BACKEND` | `s3` or `azure` |
| `S3_BUCKET` / `AZURE_STORAGE_CONTAINER` | Source bucket/container |
| `S3_KEY` | **Required** — the full object key |
| `CREATE_DB` | `true` to create the database first (pg/mysql/mariadb) |

---

## Example — restore a specific Postgres dump

```sh
podman run --rm \
  -e DB_TYPE=postgresql \
  -e DB_HOST=pg.staging \
  -e DB_USER=backup \
  -e DB_PASSWORD=secret \
  -e DB_NAME=app \
  -e STORAGE_BACKEND=s3 \
  -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... \
  -e AWS_SECRET_ACCESS_KEY=... \
  -e S3_BUCKET=prod-backups \
  -e S3_PREFIX=postgresql-dumps \
  -e S3_KEY=postgresql-dumps/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz \
  -e CREATE_DB=true \
  localhost/dumpscript:go-alpine restore
```

---

## Finding the right key

```sh
aws s3 ls s3://prod-backups/postgresql-dumps/daily/ --recursive \
  | awk '{print $4}' | grep dump_ | sort | tail -10
```

Or for Azure:

```sh
az storage blob list \
  --account-name prodbackups --container-name dumps \
  --prefix postgresql-dumps/daily/ --query "[].name" -o tsv | sort | tail -10
```

---

## Engine-specific gotchas

| Engine | Note |
|---|---|
| **Redis** | Restore is **unsupported** — the RDB format must be placed at `<redis-data-dir>/dump.rdb` and the server restarted. Returns `ErrRedisRestoreUnsupported`. |
| **etcd** | Restore is **unsupported** — `etcdctl snapshot restore` creates a fresh data-dir and needs cluster-wide coordination. Returns `ErrEtcdRestoreUnsupported`. |
| **Neo4j** | Requires the DB to be **stopped** before the load. |
| **ClickHouse** | Target table must already exist with matching schema (`FORMAT Native` preserves neither DDL nor types). |
| **SQLite** | The target file is **created if missing** (sqlite3 default). |
| **CockroachDB** | Runs through `psql`; works for dumps produced by the matching Cockroach dumper. |

Individual engine docs under [`./../engines/`](../engines/README.md) cover
each gotcha in depth.

---

## Failure modes

| Situation | Outcome |
|---|---|
| `S3_KEY` missing | Validation error at startup |
| Key doesn't exist in bucket | Exit 1 (storage error) |
| Unauthorised / wrong credentials | Exit 1 |
| Restore client exits non-zero | Exit 1, Slack `failure` |
| Engine doesn't support restore (redis/etcd) | Exit 1 with sentinel error + explanatory log |

---

## Timeouts

| Variable | Default |
|---|---|
| `RESTORE_TIMEOUT` | `2h` — total budget for the restore process |

---

## Related

- [`dump`](./dump.md)
- [`cleanup`](./cleanup.md)
- [Engines](../engines/README.md)

# PostgreSQL

`DB_TYPE=postgresql` • default port `5432` • extension `.sql.gz`

---

## How it works

- **Dump**: `pg_dump` when `DB_NAME` is set; `pg_dumpall` when it is empty
  (full-cluster dump including roles).
- **Restore**: `psql -d <DB_NAME>` reading the gunzipped SQL from stdin.
- **Verifier**: scans the tail for `-- PostgreSQL database dump complete`
  (pg_dump) or `-- PostgreSQL database cluster dump complete` (pg_dumpall).

The image ships `pg_dump 18`, which covers **every Postgres server version
9.2 → 18** via forward compatibility of the pg client wire.

---

## Env vars

| Var | Notes |
|---|---|
| `DB_TYPE` | `postgresql` |
| `DB_HOST` | required |
| `DB_PORT` | default 5432 |
| `DB_USER` | required |
| `DB_PASSWORD` | required |
| `DB_NAME` | optional — empty = `pg_dumpall` |
| `DUMP_OPTIONS` | raw pg_dump flags — e.g. `--format=plain --no-owner --no-acl` |
| `CREATE_DB` | restore only — `CREATE DATABASE` before applying |

---

## Dump one database

```sh
podman run --rm \
  -e DB_TYPE=postgresql \
  -e DB_HOST=pg.prod -e DB_PORT=5432 \
  -e DB_USER=backup -e DB_PASSWORD=secret \
  -e DB_NAME=app \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=pg \
  -e AWS_REGION=us-east-1 -e PERIODICITY=daily \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  localhost/dumpscript:go-alpine dump
```

Produces:

```
pg/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz
```

## Dump the whole cluster (pg_dumpall)

Same invocation, but **omit `DB_NAME`**:

```sh
...
  -e DB_USER=postgres \
  # no DB_NAME
...
```

---

## Restore

```sh
podman run --rm \
  -e DB_TYPE=postgresql -e DB_HOST=pg.stage \
  -e DB_USER=backup -e DB_PASSWORD=secret -e DB_NAME=app \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=pg \
  -e S3_KEY=pg/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz \
  -e CREATE_DB=true \
  localhost/dumpscript:go-alpine restore
```

---

## Version matrix (tested by e2e)

| Server | Client | Result |
|---|---|---|
| 13 | pg_dump 18 | ✅ |
| 14 | pg_dump 18 | ✅ |
| 15 | pg_dump 18 | ✅ |
| 16 | pg_dump 18 | ✅ |
| 17 | pg_dump 18 | ✅ |
| 18 | pg_dump 18 | ✅ |

The `TestPostgres` suite (`tests/e2e/engines_test.go`) exercises each server
version as a sub-test.

---

## Common flags via `DUMP_OPTIONS`

| Flag | Purpose |
|---|---|
| `--no-owner --no-acl` | Drop `ALTER OWNER` + GRANT statements — useful for restoring to a different role |
| `--schema-only` | DDL only |
| `--data-only` | Data only |
| `--exclude-table=PATTERN` | Skip specific tables |
| `--compress=9` | pg_dump-level gzip (redundant with our gzip envelope) |

---

## Tips

- **RDS IAM auth**: pass the IAM token as `DB_PASSWORD`; the token is valid
  for 15 min. Re-schedule the CronJob more frequently than that, or generate
  it freshly via an initContainer.
- **Very large DBs**: set `DUMP_TIMEOUT=6h` and `STORAGE_CHUNK_SIZE=512M` to
  avoid multipart-small-parts throttling.
- **Search_path pollution**: `--no-owner` is usually enough; if not, try
  `--no-publications --no-subscriptions`.

---

## Back

- [Engines matrix](./README.md)
- [Docs home](../README.md)

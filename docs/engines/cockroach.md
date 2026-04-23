# CockroachDB

`DB_TYPE=cockroach` • default port `26257` • extension `.sql.gz`

---

## How it works

CockroachDB speaks the Postgres wire protocol but does **not** implement the
full `pg_catalog` shim that `pg_dump 18` requires (notably
`pg_extension.tableoid`). A straight `pg_dump` invocation fails with:

```
ERROR: column "x.tableoid" does not exist
```

The Cockroach dumper therefore works CRDB-native:

1. Enumerate tables via `SHOW TABLES FROM <db>`.
2. For each table emit the DDL from `SHOW CREATE TABLE`.
3. Stream data via `psql \copy <table> TO STDOUT`.
4. Wrap the assembled stream in `BEGIN;` / `COMMIT;`.
5. Append the footer marker `-- PostgreSQL database dump complete` so the
   shared Postgres verifier accepts it.

**Restore** goes through plain `psql` with the captured SQL from stdin — the
output is standard Postgres-syntax, so any CRDB (or Postgres) target works.

**Verifier**: reuses the Postgres footer marker (`-- PostgreSQL database
dump complete`).

---

## Env vars

| Var | Notes |
|---|---|
| `DB_TYPE` | `cockroach` |
| `DB_HOST` | required (cluster entrypoint, typically the HAProxy / internal LB) |
| `DB_PORT` | default 26257 |
| `DB_USER` | required (e.g. `root`) |
| `DB_PASSWORD` | empty for `--insecure` clusters |
| `DB_NAME` | **required** — the database to dump |

`DB_NAME` is required — the dumper walks tables per-database and has no
"all databases" mode.

---

## Dump

```sh
podman run --rm \
  -e DB_TYPE=cockroach \
  -e DB_HOST=crdb.prod -e DB_PORT=26257 \
  -e DB_USER=root -e DB_PASSWORD="" \
  -e DB_NAME=appdb \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=crdb \
  -e PERIODICITY=daily -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  localhost/dumpscript:go-alpine dump
```

## Restore

```sh
podman run --rm \
  -e DB_TYPE=cockroach -e DB_HOST=crdb.stage \
  -e DB_USER=root -e DB_PASSWORD="" -e DB_NAME=appdb \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=crdb \
  -e S3_KEY=crdb/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz \
  localhost/dumpscript:go-alpine restore
```

---

## Tested versions

- CockroachDB v23.2.x ⚠️ (pg_dump path fails — covered by our custom dumper)
- CockroachDB v24.2.x ✅ via e2e `TestCockroach`

Older servers (≤ v22) may lack `SHOW CREATE TABLE` output format stability —
pin an image variant tied to your CRDB version if you go back that far.

---

## Limitations

- **Foreign keys & sequences** in the dump output are whatever `SHOW CREATE
  TABLE` returns — usually complete, but exotic constraints may need review.
- **UDTs / user-defined types** (new in v24) aren't dumped — CRDB's
  `SHOW CREATE TYPE` hasn't been wired in.
- Large tables: the dumper streams per-table sequentially; for multi-TB
  clusters consider CRDB Enterprise `BACKUP`/`RESTORE` instead.

---

## Tips

- **--insecure clusters**: set `DB_PASSWORD=""` (empty); `psql` accepts no
  password when the server allows it.
- **Certificate auth**: pass `sslmode=verify-full sslrootcert=... sslcert=...
  sslkey=...` in `DUMP_OPTIONS` — passed through to `psql`.
- **Large single table**: pg-wire `\copy` is efficient; the gzip envelope
  adds roughly 2–5× compression on typical OLTP data.

---

## Back

- [Engines matrix](./README.md)
- [Docs home](../README.md)

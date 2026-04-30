# MySQL

`DB_TYPE=mysql` • default port `3306` • extension `.sql.gz`

---

## How it works

- **Dump**: `mariadb-dump` (MariaDB's drop-in replacement for `mysqldump`,
  11.8 in the image). Fully compatible with MySQL 5.7 and 8.0.
- **Restore**: `mariadb` CLI reading gunzipped SQL from stdin.
- **Verifier**: footer marker `-- Dump completed`.

The single `mariadb-dump 11.8` client covers MySQL 5.7, 8.0 and every MariaDB
server version.

---

## Env vars

| Var | Notes |
|---|---|
| `DB_TYPE` | `mysql` |
| `DB_HOST` / `DB_PORT` | required / default 3306 |
| `DB_USER` / `DB_PASSWORD` | required |
| `DB_NAME` | optional — empty = `--all-databases` |
| `DUMP_OPTIONS` | raw mariadb-dump flags |
| `CREATE_DB` | restore only |

---

## Dump

```sh
podman run --rm \
  -e DB_TYPE=mysql \
  -e DB_HOST=mysql.prod -e DB_USER=backup -e DB_PASSWORD=secret \
  -e DB_NAME=app \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=mysql \
  -e PERIODICITY=daily -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  localhost/dumpscript:go-alpine dump
```

## Restore

```sh
podman run --rm \
  -e DB_TYPE=mysql -e DB_HOST=mysql.stage \
  -e DB_USER=backup -e DB_PASSWORD=secret -e DB_NAME=app \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=mysql \
  -e S3_KEY=mysql/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz \
  -e CREATE_DB=true \
  localhost/dumpscript:go-alpine restore
```

---

## Useful `DUMP_OPTIONS`

| Flag | Effect |
|---|---|
| `--single-transaction` | Consistent dump without locking tables (InnoDB) |
| `--routines --triggers` | Include stored procedures + triggers |
| `--events` | Include scheduled events |
| `--skip-lock-tables` | MyISAM-aware, use with consistency plan |
| `--hex-blob` | Binary-safe exports for blob columns |

Default for a clean, consistent dump:

```
DUMP_OPTIONS="--single-transaction --routines --triggers --events"
```

---

## Tips

- **MySQL 5.7 on ARM**: the test harness uses `linux/amd64` platform; in
  production on ARM hosts you may need `--platform=linux/amd64` on the
  **source DB container**, not on dumpscript.
- **caching_sha2_password (8.0 default)**: works — mariadb-dump 11.x has
  native support via `mariadb-connector-c`.
- **Long dump + `--single-transaction`**: pair with
  `innodb_flush_log_at_trx_commit=2` on the target to speed up restore.

---

## Back

- [Engines matrix](./README.md)
- [Docs home](../README.md)

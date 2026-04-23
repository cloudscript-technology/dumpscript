# MariaDB

`DB_TYPE=mariadb` • default port `3306` • extension `.sql.gz`

---

## How it works

Essentially identical to the [MySQL engine](./mysql.md) — same binary
(`mariadb-dump 11.8`), same verifier footer, same restore client. The only
reason to pick `mariadb` over `mysql` as `DB_TYPE` is:

- The server is a MariaDB 11.x instance that dropped the `mysql` symlink
  (since MariaDB 11.0, the `mysql` CLI was renamed to `mariadb`). Our
  restorer invokes `mariadb` when `DB_TYPE=mariadb` and `mysql` when
  `DB_TYPE=mysql` — matching the client symlink that each server ships.
- Future-proofing: if MariaDB ever diverges in dump options, we can keep the
  implementations separate without breaking MySQL users.

---

## Env vars

Identical to MySQL. See [MySQL](./mysql.md#env-vars).

---

## Dump + restore

```sh
podman run --rm \
  -e DB_TYPE=mariadb \
  -e DB_HOST=mariadb.prod -e DB_USER=backup -e DB_PASSWORD=secret \
  -e DB_NAME=app \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=mariadb \
  -e PERIODICITY=daily -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  localhost/dumpscript:go-alpine dump
```

Restore — only difference from MySQL is the engine pick:

```sh
...
  -e DB_TYPE=mariadb \
  -e S3_KEY=mariadb/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz \
...
```

---

## Tested versions

- MariaDB 10.11 LTS ✅
- MariaDB 11.4 LTS ✅
- MariaDB 11.x (latest stable) ✅

---

## Tips

- **Galera cluster**: point at any node — `mariadb-dump --single-transaction`
  is Galera-safe.
- **Column store (MariaDB ColumnStore 22.x)**: use `--skip-extended-insert`
  to avoid memory blow-up on restore.
- **Dynamic columns**: stored as BLOB, preserved verbatim.

---

## Back

- [MySQL (same tooling)](./mysql.md)
- [Engines matrix](./README.md)
- [Docs home](../README.md)

# SQL Server

`DB_TYPE=sqlserver` • default port `1433` • extension `.sql.gz`

---

## How it works

- **Dump**: `mssql-scripter` (Microsoft's Python-based tool) emits plain T-SQL.
- **Restore**: `sqlcmd -b` reading gunzipped SQL from stdin.
- **Verifier**: tail must end with `\nGO` batch terminator after optional
  trailing whitespace.

### Image requirement

The default Alpine image **does not** ship `mssql-scripter` or `sqlcmd`
(Microsoft-provided, Debian-based). Build a custom image variant:

```Dockerfile
FROM localhost/dumpscript:go-alpine AS base
FROM debian:bookworm-slim
# install mssql-tools18 and mssql-scripter (pip)
RUN apt-get update && apt-get install -y curl gnupg python3-pip \
 && curl -fsSL https://packages.microsoft.com/keys/microsoft.asc | apt-key add - \
 && curl https://packages.microsoft.com/config/debian/12/prod.list > /etc/apt/sources.list.d/mssql-release.list \
 && apt-get update \
 && ACCEPT_EULA=Y apt-get install -y msodbcsql18 mssql-tools18 \
 && pip install mssql-scripter --break-system-packages
COPY --from=base /usr/local/bin/dumpscript /usr/local/bin/dumpscript
ENV PATH="/opt/mssql-tools18/bin:$PATH"
ENTRYPOINT ["/usr/local/bin/dumpscript"]
```

---

## Env vars

| Var | Notes |
|---|---|
| `DB_TYPE` | `sqlserver` |
| `DB_HOST` | required (server or listener) |
| `DB_PORT` | default 1433 |
| `DB_USER` | required (e.g. `sa`, or a domain user) |
| `DB_PASSWORD` | required |
| `DB_NAME` | **required** — there's no cluster-wide dump |
| `DUMP_OPTIONS` | raw `mssql-scripter` flags (e.g. `--schema-only`) |

---

## Dump

```sh
podman run --rm \
  -e DB_TYPE=sqlserver \
  -e DB_HOST=mssql.prod -e DB_PORT=1433 \
  -e DB_USER=sa -e DB_PASSWORD=secret \
  -e DB_NAME=app \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=mssql \
  -e PERIODICITY=daily -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  my-registry/dumpscript:go-mssql dump
```

## Restore

```sh
podman run --rm \
  -e DB_TYPE=sqlserver -e DB_HOST=mssql.stage \
  -e DB_USER=sa -e DB_PASSWORD=secret -e DB_NAME=app \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=mssql \
  -e S3_KEY=mssql/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz \
  my-registry/dumpscript:go-mssql restore
```

---

## Tested versions

Unit tests validate the command-line assembly and verifier logic. Full
e2e with a live SQL Server container requires the custom image above and
is not part of the default CI matrix.

---

## Limitations

- **Always Encrypted / TDE data** is dumped in its encrypted form; restore
  requires matching keys on the target.
- **Native `.bak` files** are NOT used — `mssql-scripter` produces T-SQL
  text, which is portable but larger than binary backups.
- **`BACKUP DATABASE` native format**: if you prefer that, dump it manually
  and put the `.bak` in S3; `dumpscript restore` won't drive it.

---

## Back

- [Engines matrix](./README.md)
- [Docs home](../README.md)

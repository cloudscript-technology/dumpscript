# Oracle

`DB_TYPE=oracle` • default port `1521` • extension `.dmp.gz`

---

## How it works

- **Dump**: legacy `exp` (classic export). We use `exp` rather than `expdp`
  because Data Pump writes server-side to `DATA_PUMP_DIR` and can't produce
  a stream-to-stdout result.
- **Restore**: `imp` reading the gunzipped DMP from stdin.
- **Verifier**: ASCII magic `EXPORT:V` within the first 512 bytes of the
  decompressed DMP.

### Image requirement

The default Alpine image **does not** include Oracle Instant Client. Build
a custom image variant that:

1. Pulls Oracle Instant Client (`basic`, `tools`) — requires Oracle's
   licence acceptance.
2. Installs `gcompat` for glibc compatibility on Alpine (or switch to a
   glibc base).
3. Sets `LD_LIBRARY_PATH` / `PATH` to the client bin/lib dirs.

Sketch:

```Dockerfile
FROM oraclelinux:8-slim
RUN microdnf install -y oracle-instantclient-release-el8 \
 && microdnf install -y oracle-instantclient-basic oracle-instantclient-tools
COPY --from=localhost/dumpscript:go-alpine /usr/local/bin/dumpscript /usr/local/bin/dumpscript
ENTRYPOINT ["/usr/local/bin/dumpscript"]
```

---

## Env vars

| Var | Notes |
|---|---|
| `DB_TYPE` | `oracle` |
| `DB_HOST` | required (typically the SCAN listener) |
| `DB_PORT` | default 1521 |
| `DB_USER` | required |
| `DB_PASSWORD` | required |
| `DB_NAME` | service name (e.g. `ORCLPDB1`) |
| `DUMP_OPTIONS` | raw `exp` flags (e.g. `CONSISTENT=Y OWNER=SCHEMA`) |

Connection string built as `user/pass@host:port/service` (EZCONNECT).

---

## Dump

```sh
podman run --rm \
  -e DB_TYPE=oracle \
  -e DB_HOST=oracle.prod -e DB_PORT=1521 \
  -e DB_USER=app_user -e DB_PASSWORD=secret \
  -e DB_NAME=ORCLPDB1 \
  -e DUMP_OPTIONS="OWNER=APP_USER CONSISTENT=Y" \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=oracle \
  -e PERIODICITY=daily -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  my-registry/dumpscript:go-oracle dump
```

## Restore

```sh
podman run --rm \
  -e DB_TYPE=oracle -e DB_HOST=oracle.stage \
  -e DB_USER=app_user -e DB_PASSWORD=secret -e DB_NAME=ORCLPDB1 \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=oracle \
  -e S3_KEY=oracle/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.dmp.gz \
  my-registry/dumpscript:go-oracle restore
```

---

## Tested versions

Oracle e2e is not included in the default CI matrix due to image licensing.
Unit tests cover command assembly + verifier logic.

---

## Limitations

- **`exp` is deprecated** since Oracle 12c; Oracle supports it but doesn't
  add new features. For modern deployments, use Data Pump (`expdp`) with
  external orchestration outside this tool.
- **Full-database dumps** (`FULL=Y`) require `SYSDBA` privilege; most users
  dump schema-by-schema with `OWNER=...`.
- **CLOB/BLOB performance**: `exp` handles them but at reduced throughput
  vs. Data Pump; factor that into `DUMP_TIMEOUT` for large tables.

---

## Back

- [Engines matrix](./README.md)
- [Docs home](../README.md)

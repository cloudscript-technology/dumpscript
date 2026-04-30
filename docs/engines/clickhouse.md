# ClickHouse

`DB_TYPE=clickhouse` • default port `9000` • extension `.native.gz`

---

## How it works

- **Dump**: `clickhouse-client --query="SELECT * FROM <db>.<table> FORMAT Native"`
  streaming to gzip. `FORMAT Native` is ClickHouse's compact columnar binary
  interchange format.
- **Restore**: `clickhouse-client --query="INSERT INTO <db>.<table> FORMAT Native"`
  with the gunzipped bytes on stdin.
- **Verifier**: envelope-only (non-empty + full gzip CRC) — Native format
  has no stable magic bytes.

### Image requirement

The default Alpine image **does not** ship `clickhouse-client`. Build a
custom image:

```Dockerfile
FROM localhost/dumpscript:go-alpine
RUN apk add --no-cache clickhouse || \
    (wget -qO- https://builds.clickhouse.com/master/aarch64/clickhouse \
         > /usr/local/bin/clickhouse \
     && chmod +x /usr/local/bin/clickhouse \
     && ln -s /usr/local/bin/clickhouse /usr/local/bin/clickhouse-client)
```

---

## Env vars

| Var | Notes |
|---|---|
| `DB_TYPE` | `clickhouse` |
| `DB_HOST` / `DB_PORT` | required / default 9000 |
| `DB_USER` / `DB_PASSWORD` | required |
| `DB_NAME` | **required** — must be `<database>.<table>` |

Single-table per run is a design choice: `FORMAT Native` is schema-opaque,
and the restorer requires the target table to exist with a matching schema.
Multi-table = multiple dumpscript runs with different `S3_PREFIX` each.

---

## Dump

```sh
podman run --rm \
  -e DB_TYPE=clickhouse \
  -e DB_HOST=ch.prod -e DB_PORT=9000 \
  -e DB_USER=backup -e DB_PASSWORD=secret \
  -e DB_NAME=analytics.events \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=clickhouse/analytics.events \
  -e PERIODICITY=daily -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  my-registry/dumpscript:go-clickhouse dump
```

## Restore

```sh
# Ensure the target table exists with the expected schema first:
clickhouse-client --host=ch.stage --query="CREATE TABLE IF NOT EXISTS analytics.events (...) ..."

podman run --rm \
  -e DB_TYPE=clickhouse -e DB_HOST=ch.stage \
  -e DB_USER=backup -e DB_PASSWORD=secret \
  -e DB_NAME=analytics.events \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=clickhouse/analytics.events \
  -e S3_KEY=clickhouse/analytics.events/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.native.gz \
  my-registry/dumpscript:go-clickhouse restore
```

---

## Tested versions

- ClickHouse 22.x, 23.x, 24.x ✅ (same Native wire format across versions)
- e2e not included in default CI (requires custom image).

---

## Limitations

- **Schema not preserved** — Native is row-data only. If you need DDL
  preservation, dump `system.tables`/`system.columns` separately or use
  [`clickhouse-backup`](https://github.com/Altinity/clickhouse-backup) for
  full-DB snapshots.
- **Replicated tables**: dumping from a single replica is fine; restoring
  into a replica will re-replicate — this can be surprising on multi-DC
  clusters.
- **Large tables**: Native is efficient, but `clickhouse-client` buffers in
  memory for some query shapes. Prefer `SELECT *` over complex projections.

---

## Back

- [Engines matrix](./README.md)
- [Docs home](../README.md)

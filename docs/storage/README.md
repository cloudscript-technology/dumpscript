# Storage backends

`dumpscript` writes artefacts to **S3-compatible** object stores or
**Azure Blob Storage**, chosen by `STORAGE_BACKEND`.

---

## Comparison

|                    | S3 (AWS / MinIO / GCS) | Azure Blob |
|---|---|---|
| `STORAGE_BACKEND`  | `s3` (default)          | `azure` |
| Endpoint override  | `AWS_S3_ENDPOINT_URL`   | `AZURE_STORAGE_ENDPOINT` |
| Static creds       | `AWS_ACCESS_KEY_ID/SECRET` | `AZURE_STORAGE_KEY` or SAS |
| Federated identity | IRSA (`AWS_ROLE_ARN`)   | SAS token |
| Container concept  | bucket                  | container |
| Prefix var         | `S3_PREFIX`             | `AZURE_STORAGE_PREFIX` (falls back to `S3_PREFIX`) |
| Restore key        | `S3_KEY`                | `S3_KEY` (shared) |

Deep dives:

- [S3 backend (AWS, MinIO, GCS)](./s3.md)
- [Azure Blob (cloud + Azurite)](./azure.md)

---

## Key layout

Both backends use the same layout:

```
<prefix>/<periodicity>/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.<ext>.gz
```

Examples:

```
pg/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz
mongo/weekly/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.archive.gz
redis/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.rdb.gz
```

The `.lock` object lives alongside the day folder:

```
<prefix>/<periodicity>/YYYY/MM/DD/.lock
```

Contents: JSON payload with `execution_id`, `hostname`, `pid`, `started_at`
— see [Locking](../features/locking.md).

---

## Upload tuning

Both backends share:

| Variable | Default | Description |
|---|---|---|
| `STORAGE_UPLOAD_CUTOFF` | `200M` | Size threshold for multipart/block upload |
| `STORAGE_CHUNK_SIZE` | `100M` | Multipart chunk size |
| `STORAGE_UPLOAD_CONCURRENCY` | `4` | Parallel upload workers |

Accepts `k`, `M`, `G` suffixes (e.g. `512M`, `2G`).

For multi-GB dumps, raise `STORAGE_CHUNK_SIZE` to `512M` and
`STORAGE_UPLOAD_CONCURRENCY` to `8` to avoid S3's
"too many small parts" limit.

---

## Retry + logging decorators

Both backends are wrapped with two Decorators:

- **`storage.Retrying`** — exponential backoff on transient errors
  (timeouts, 5xx, throttling). Base interval 500ms, up to 5 attempts.
- **`storage.Logging`** — structured `slog` events per operation
  (`upload.start`, `upload.ok`, `list`, `delete` …).

Both decorators are applied automatically; there are no env knobs to toggle
them.

---

## Reachability preflight

Before any dump, the pipeline calls `Storage.List(prefix)` with a 10s
timeout. A non-nil error **fails the run** before touching the database —
avoids the common failure mode where the dump succeeds but the bucket is
unreachable, leaving you with nothing.

This is especially useful for:

- IRSA token expiry (will surface as `AccessDenied` instantly).
- VPC endpoints misconfigured (timeout, never hangs the whole dump).
- Credentials rotation out of sync (auth errors caught early).

---

## Back

- [Docs home](../README.md)
- [Configuration reference](../configuration.md)

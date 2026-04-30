# Elasticsearch

`DB_TYPE=elasticsearch` • default port `9200` • extension `.ndjson.gz`

---

## How it works

Native Go implementation — **no external CLI required**.

- **Dump**: HTTP scroll API (`POST /<index>/_search?scroll=1m`, size 1000),
  writing each hit as one NDJSON line (`{"_id":"…","_source":{…}}`). Streams
  directly into the gzip writer.
- **Restore**: batched `POST /_bulk` (500 docs per request) reading the
  gunzipped NDJSON line-by-line.
- **Verifier**: the last non-empty NDJSON line must parse as valid JSON —
  catches SIGKILL-truncated dumps.

---

## Env vars

| Var | Notes |
|---|---|
| `DB_TYPE` | `elasticsearch` |
| `DB_HOST` | required |
| `DB_PORT` | default 9200 |
| `DB_USER` | **optional** — basic auth user |
| `DB_PASSWORD` | optional |
| `DB_NAME` | **required** — the single index to dump/restore |
| `DUMP_OPTIONS` | `--scheme=https` and/or `--auth-header=<raw>` |

### Auth modes

- **Basic**: set `DB_USER` + `DB_PASSWORD`.
- **API key / Bearer**: leave user/password empty and pass the full
  `Authorization:` header value via `DUMP_OPTIONS=--auth-header=ApiKey <base64>`.

### TLS

- `--scheme=https` (via `DUMP_OPTIONS`) switches the URL scheme.
- Certificate validation uses the system trust store.

---

## Dump

```sh
podman run --rm \
  -e DB_TYPE=elasticsearch \
  -e DB_HOST=es.prod -e DB_PORT=9200 \
  -e DB_USER=dumpscript -e DB_PASSWORD=secret \
  -e DB_NAME=my-index \
  -e DUMP_OPTIONS="--scheme=https" \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=es \
  -e PERIODICITY=daily -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  localhost/dumpscript:go-alpine dump
```

## Restore

```sh
podman run --rm \
  -e DB_TYPE=elasticsearch -e DB_HOST=es.stage \
  -e DB_USER=dumpscript -e DB_PASSWORD=secret \
  -e DB_NAME=my-index \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=es \
  -e S3_KEY=es/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.ndjson.gz \
  localhost/dumpscript:go-alpine restore
```

---

## Tested versions

- Elasticsearch 7.x ✅ (same HTTP shape)
- Elasticsearch 8.13.0 ✅ via e2e `TestElasticsearch`
- OpenSearch 2.x ✅ (Amazon OpenSearch is compatible)

---

## Limitations

- **Single index per run** — multi-index backup = multiple runs, each with a
  different `S3_PREFIX`.
- **No alias / template / mapping dump** — the NDJSON carries only
  `_source`. For a full cluster snapshot, use ES native snapshot repositories
  (`PUT /_snapshot/<repo>`) instead.
- **Version mapping mismatch**: restore assumes the target index already
  exists with a compatible mapping. Create the index with the right mapping
  **before** running `restore`.

---

## Tips

- **Large indices**: the scroll keeps a context open on the server for 1
  minute between pages — default page size 1000 is a good balance.
- **OpenSearch Service (AWS)**: pass the signed URL directly in `DB_HOST`
  and use `--auth-header=AWS4-HMAC-...` — SigV4 signing is outside the tool;
  pre-sign a token with an initContainer.
- **Self-signed TLS**: not supported directly; mount the CA bundle into
  `/etc/ssl/certs/` before the image starts.

---

## Back

- [Engines matrix](./README.md)
- [Docs home](../README.md)

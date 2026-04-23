# MongoDB

`DB_TYPE=mongodb` • default port `27017` • extension `.archive.gz`

---

## How it works

- **Dump**: `mongodump --archive=/dev/stdout --gzip`. The `--archive` format
  is a compact binary BSON stream Mongo-native.
- **Restore**: `mongorestore --archive --gzip` reading from stdin.
- **Verifier**: archive magic `0x8199e26d` in the first 4 bytes after gunzip.

Since `mongodump` already compresses with `--gzip`, our outer gzip adds a
second (cheap) layer — mainly so every engine's artefact shares the same
`.gz` suffix and pipeline.

---

## Env vars

| Var | Notes |
|---|---|
| `DB_TYPE` | `mongodb` |
| `DB_HOST` / `DB_PORT` | required / default 27017 |
| `DB_USER` / `DB_PASSWORD` | required |
| `DB_NAME` | required (single DB — use DUMP_OPTIONS for multi-DB) |
| `DUMP_OPTIONS` | raw mongodump / mongorestore flags — required for auth database |

---

## Example — auth against admin DB

```sh
podman run --rm \
  -e DB_TYPE=mongodb \
  -e DB_HOST=mongo.prod -e DB_USER=admin -e DB_PASSWORD=secret \
  -e DB_NAME=app \
  -e DUMP_OPTIONS="--authenticationDatabase=admin" \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=mongo \
  -e PERIODICITY=daily -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  localhost/dumpscript:go-alpine dump
```

## MongoDB Atlas (SRV)

Pass the full connection string in `DB_HOST`:

```sh
-e DB_HOST="mongodb+srv://user:pw@cluster.example.mongodb.net/?retryWrites=true"
```

Leave `DB_USER` / `DB_PASSWORD` empty if they are already embedded in the
SRV URI.

## Restore

```sh
podman run --rm \
  -e DB_TYPE=mongodb -e DB_HOST=mongo.stage \
  -e DB_USER=admin -e DB_PASSWORD=secret -e DB_NAME=app \
  -e DUMP_OPTIONS="--authenticationDatabase=admin" \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=mongo \
  -e S3_KEY=mongo/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.archive.gz \
  localhost/dumpscript:go-alpine restore
```

---

## Useful `DUMP_OPTIONS`

| Flag | Purpose |
|---|---|
| `--authenticationDatabase=admin` | Most common — auth against `admin` DB |
| `--oplog` | Include oplog for point-in-time consistency |
| `--readPreference=secondary` | Offload dump work to a replica |
| `--db=OTHER` | Override `DB_NAME` (rarely needed) |

---

## Tested versions

- MongoDB 4.4 ✅
- MongoDB 5.0 ✅
- MongoDB 6.0 ✅
- MongoDB 7.0 ✅

The `mongodb-tools` (`mongodump`/`mongorestore` 100.x) in the image covers
all of these.

---

## Tips

- **Atlas M0 / M2 / M5 (shared)**: `mongodump` works, but rate limits apply.
  Add `--readPreference=secondary` to avoid impacting the primary.
- **Sharded clusters**: point at a mongos; `mongodump` handles shards
  transparently.
- **Oplog replay**: pair `--oplog` (dump) + `--oplogReplay` (restore) for
  PITR-ish semantics inside the dump window.

---

## Back

- [Engines matrix](./README.md)
- [Docs home](../README.md)

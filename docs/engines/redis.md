# Redis

`DB_TYPE=redis` • default port `6379` • extension `.rdb.gz`

---

## How it works

- **Dump**: `redis-cli --rdb <tmpfile>` produces a standard RDB snapshot
  (same format the server writes on `BGSAVE`). We use a temp file because
  `redis-cli --rdb /dev/stdout` calls `ftruncate()`/`fsync()` on the target
  which fails on pipes.
- **Restore**: **unsupported** — returns `ErrRedisRestoreUnsupported`. RDB
  restore requires stopping the server, replacing `<data-dir>/dump.rdb`, and
  restarting. Filesystem + lifecycle control are outside a dump tool's remit.
- **Verifier**: magic bytes `REDIS` + 4 ASCII digits (RDB version) at the
  start of the decompressed file.

---

## Env vars

| Var | Notes |
|---|---|
| `DB_TYPE` | `redis` |
| `DB_HOST` | required |
| `DB_PORT` | default 6379 |
| `DB_USER` | **optional** — only required for ACL-based auth (Redis 6+) |
| `DB_PASSWORD` | optional |
| `DB_NAME` | ignored (Redis has numeric DBs, not named) |

---

## Dump

```sh
podman run --rm \
  -e DB_TYPE=redis \
  -e DB_HOST=redis.prod -e DB_PORT=6379 \
  -e DB_PASSWORD=secret \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=redis \
  -e PERIODICITY=daily -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  localhost/dumpscript:go-alpine dump
```

## Restore (manual)

```sh
# Download
aws s3 cp s3://prod-backups/redis/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.rdb.gz - \
  | gunzip > /tmp/dump.rdb

# Apply on the Redis host
systemctl stop redis
cp /tmp/dump.rdb /var/lib/redis/dump.rdb
chown redis:redis /var/lib/redis/dump.rdb
systemctl start redis
```

For online / key-level restore, use
[redis-dump-go](https://github.com/yannh/redis-dump-go) or
[RIOT](https://redis.github.io/riot/).

---

## Cluster mode

Point `DB_HOST` at **one primary** per shard; the RDB snapshot reflects only
that node's keyspace. For a full cluster backup, run one `dumpscript dump`
per shard (different `S3_PREFIX` each).

---

## Tips

- **AOF mode**: `redis-cli --rdb` still works because it issues a SYNC to
  produce an RDB stream regardless of the server's persistence mode.
- **Replica snapshot**: point at a replica to avoid load on the primary —
  `redis-cli --rdb` then uses the replica's chain.
- **Large keyspaces**: the RDB stream is efficient; plan for it to be
  ~20–50% of the in-memory footprint after gzip.

---

## Back

- [Engines matrix](./README.md)
- [Docs home](../README.md)

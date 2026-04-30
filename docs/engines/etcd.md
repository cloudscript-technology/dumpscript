# etcd

`DB_TYPE=etcd` • default port `2379` • extension `.db.gz`

---

## How it works

- **Dump**: `etcdctl snapshot save <tmpfile>`. The tool writes atomically via
  `rename`, so we can't use `/dev/stdout`; the dumper writes to a temp file
  and streams it through gzip.
- **Restore**: **unsupported** — returns `ErrEtcdRestoreUnsupported`.
  `etcdctl snapshot restore` produces a fresh data-dir that must be
  bootstrapped as a new cluster (new `--initial-cluster-token`,
  `--initial-cluster-state=new`), applied per-node. That's operator work,
  not dump-tool work.
- **Verifier**: BoltDB magic `0xED0CDAED` (in either endian) within the
  first 4 KiB of the decompressed snapshot.

---

## Env vars

| Var | Notes |
|---|---|
| `DB_TYPE` | `etcd` |
| `DB_HOST` | required — typically one cluster member |
| `DB_PORT` | default 2379 |
| `DB_USER` | optional (auth-enabled clusters pass `user:pass` to `--user`) |
| `DB_PASSWORD` | optional |
| `DUMP_OPTIONS` | `--scheme=https` and/or extra `etcdctl` flags (e.g. `--cacert=...`) |

`etcdctl` is driven with `ETCDCTL_API=3` and `--endpoints={scheme}://{host}:{port}`.

---

## Dump

```sh
podman run --rm \
  -e DB_TYPE=etcd \
  -e DB_HOST=etcd.prod -e DB_PORT=2379 \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=etcd \
  -e PERIODICITY=daily -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  localhost/dumpscript:go-alpine dump
```

## Restore (manual procedure)

```sh
# Download
aws s3 cp s3://prod-backups/etcd/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.db.gz - \
  | gunzip > /tmp/snapshot.db

# On each etcd member, STOP etcd first, then:
etcdctl snapshot restore /tmp/snapshot.db \
  --data-dir=/var/lib/etcd-new \
  --name=<member-name> \
  --initial-cluster=<full-cluster-spec> \
  --initial-cluster-token=<new-token> \
  --initial-advertise-peer-urls=<this-node-peer-url>

# Update systemd unit to point at /var/lib/etcd-new and restart
```

Full procedure: [etcd docs — Restoring a cluster](https://etcd.io/docs/v3.5/op-guide/recovery/).

---

## mTLS-authenticated clusters

```sh
-e DUMP_OPTIONS="--scheme=https --cacert=/certs/ca.pem --cert=/certs/etcd.pem --key=/certs/etcd-key.pem"
```

Mount certs into the container via a Kubernetes Secret + volume mount.

---

## Tested versions

- etcd v3.5.x ✅ via e2e `TestEtcd` (quay.io/coreos/etcd:v3.5.13 image)
- etcd v3.4.x ✅ (compatible snapshot format)

---

## Tips

- **K8s control plane**: pair this with a retention policy of ≥ 30 days;
  snapshots are tiny (MBs, not GBs) so keep many.
- **Multi-member cluster**: a snapshot from any member is authoritative —
  raft consensus guarantees consistency.
- **Compaction**: if your `etcd` has a lot of deleted revisions,
  pre-compact (`etcdctl compact`) + defrag (`etcdctl defrag`) on the member
  you back up to reduce snapshot size.

---

## Back

- [Engines matrix](./README.md)
- [Docs home](../README.md)

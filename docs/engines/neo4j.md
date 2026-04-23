# Neo4j

`DB_TYPE=neo4j` • default port `7687` (Bolt) • extension `.neo4j.gz`

---

## How it works

- **Dump**: `neo4j-admin database dump --to-stdout --database=<DB_NAME>`
  (Neo4j 5+). Streams the archive straight to our gzip writer.
- **Restore**: `neo4j-admin database load --from-stdin --database=<DB_NAME>
  --overwrite-destination=true`.
- **Verifier**: envelope-only (non-empty + gzip CRC). The Neo4j archive
  format is not publicly documented byte-for-byte across versions.

⚠️ **Database must be stopped** for both dump and load — `neo4j-admin`
touches store files directly. For hot backups, use Neo4j Enterprise +
`neo4j-admin backup` (online) instead; out of scope here.

### Image requirement

`neo4j-admin` is part of the Neo4j server distribution (Java + ~400 MB).
Build a custom image:

```Dockerfile
FROM neo4j:5-community
COPY --from=localhost/dumpscript:go-alpine /usr/local/bin/dumpscript /usr/local/bin/dumpscript
ENTRYPOINT ["/usr/local/bin/dumpscript"]
```

---

## Env vars

| Var | Notes |
|---|---|
| `DB_TYPE` | `neo4j` |
| `DB_NAME` | **required** — Neo4j database to dump (default `neo4j`) |
| `DB_HOST` | ignored by the tool (reads files directly) — pass `localhost` |
| `DB_USER` / `DB_PASSWORD` | unused |
| `DUMP_OPTIONS` | extra `neo4j-admin` flags |

---

## Dump (on the Neo4j host / sidecar)

```sh
# Stop Neo4j first!
systemctl stop neo4j

podman run --rm \
  --mount type=bind,source=/var/lib/neo4j/data,target=/data \
  -e DB_TYPE=neo4j \
  -e DB_NAME=neo4j \
  -e DB_HOST=localhost \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=neo4j \
  -e PERIODICITY=daily -e AWS_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  my-registry/dumpscript:go-neo4j dump

systemctl start neo4j
```

## Restore

```sh
systemctl stop neo4j

podman run --rm \
  --mount type=bind,source=/var/lib/neo4j/data,target=/data \
  -e DB_TYPE=neo4j -e DB_HOST=localhost -e DB_NAME=neo4j \
  -e S3_BUCKET=prod-backups -e S3_PREFIX=neo4j \
  -e S3_KEY=neo4j/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.neo4j.gz \
  my-registry/dumpscript:go-neo4j restore

systemctl start neo4j
```

---

## Tested versions

- Neo4j 5.x Community ✅ (via `neo4j-admin database dump --to-stdout`)
- Neo4j 4.x: the `neo4j-admin dump` syntax differed (no `database` subcommand).
  Upgrade or adapt `DUMP_OPTIONS` accordingly.

---

## Limitations

- **Enterprise online backup** (`neo4j-admin backup`) is not wired into our
  dumper. If you have Enterprise, prefer its native tooling for zero-downtime.
- **Large graphs**: dumps scale with the on-disk store size; plan
  `DUMP_TIMEOUT` accordingly.
- **Cypher-level exports** (APOC's `apoc.export.cypher.all`) produce
  portable Cypher text — different tradeoff; not covered here.

---

## Back

- [Engines matrix](./README.md)
- [Docs home](../README.md)

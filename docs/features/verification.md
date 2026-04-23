# Content verification

Before uploading, dumpscript checks that the gzip file on disk is not
just well-formed but **semantically complete**. A truncated dump can
still be a valid gzip, and a valid gzip may still be garbage if the
engine process was SIGKILL'd mid-stream.

Two layers run, in order:

1. **Envelope** — `Artifact.Verify()` (`internal/dumper/dumper.go`).
   Confirms the gzip CRC/ISIZE trailer is present and the header bytes
   are valid.
2. **Content** — per-engine `Verifier.Verify()` (`internal/verifier/`).
   Scans for a well-known footer marker or magic signature.

If either fails, the local file is removed and the run fails (exit 1 +
Slack `EventFailure`). The broken dump never touches the bucket.

Disable with `VERIFY_CONTENT=false` if you use exotic `DUMP_OPTIONS` that
suppress the footer (e.g. `mysqldump --skip-comments`).

---

## Per-engine strategies

| Engine | Strategy | Code |
|---|---|---|
| postgres | Tail contains `-- PostgreSQL database dump complete` or `-- PostgreSQL database cluster dump complete` | `verifier/postgres.go` |
| mysql / mariadb | Tail contains `-- Dump completed` | `verifier/sql.go` |
| mongodb | Archive magic `0x8199e26d` at start | `verifier/mongo.go` |
| cockroach | Same as postgres (shared registration) | `verifier/postgres.go:26-29` |
| redis | Magic `REDIS` + 4 ASCII-digit version | `verifier/redis.go` |
| sqlserver | Trailing `\nGO` batch terminator | `verifier/sqlserver.go` |
| oracle | `EXPORT:V` magic in first 512 bytes | `verifier/oracle.go` |
| elasticsearch | Last non-empty NDJSON line parses as JSON | `verifier/elasticsearch.go` |
| etcd | BoltDB magic `0xED0CDAED` (either endian) | `verifier/etcd.go` |
| clickhouse | Envelope-only (non-empty + gzip CRC) | `verifier/clickhouse.go` |
| neo4j | Envelope-only (format opaque across versions) | `verifier/neo4j.go` |
| sqlite | Tail ends with `COMMIT;` | `verifier/sqlite.go` |

---

## Why envelope-only for some

ClickHouse `FORMAT Native` and Neo4j archive formats have no stable magic
bytes (ClickHouse) or are too version-dependent to rely on (Neo4j). For
these, we trust the gzip CRC — it catches SIGKILL-truncated streams which
is the primary failure mode — and assert a non-empty decompressed payload.

---

## Turning it off

```sh
-e VERIFY_CONTENT=false
```

Use cases:

- `mysqldump --skip-comments` (suppresses the `-- Dump completed` footer)
- `mongodump --archive --gzip` called with `--skip-*` flags
- Ad-hoc engines added via a fork where the verifier doesn't yet exist

---

## Self-registration

Each verifier file calls `Register(config.DB<type>, ctor)` from its
`init()`. The factory `verifier.New` is a pure map lookup — there is no
hardcoded `switch` on `DBType`. See
[Adding a new engine](../development/adding_an_engine.md) for the pattern.

---

## Back

- [Docs home](../README.md)
- [Engines matrix](../engines/README.md)

# Engines

All 13 engines `dumpscript` supports, with their tradeoffs and links to
per-engine deep dives.

---

## Full matrix

| Engine | Protocol | Dump tool | Restore tool | Restore supported? | Verifier strategy |
|---|---|---|---|---|---|
| [PostgreSQL](./postgres.md) | pg-wire | `pg_dump` / `pg_dumpall` | `psql` | ✅ | Footer marker |
| [MySQL](./mysql.md) | MySQL | `mariadb-dump` | `mariadb` | ✅ | Footer marker |
| [MariaDB](./mariadb.md) | MariaDB | `mariadb-dump` | `mariadb` | ✅ | Footer marker |
| [MongoDB](./mongodb.md) | mongo | `mongodump --archive --gzip` | `mongorestore` | ✅ | Archive magic `0x8199e26d` |
| [CockroachDB](./cockroach.md) | pg-wire | `psql` + `SHOW CREATE` | `psql` | ✅ | Postgres footer marker |
| [Redis](./redis.md) | Redis | `redis-cli --rdb` | — | 🚫 sentinel | RDB magic `REDIS` |
| [SQL Server](./sqlserver.md) | TDS | `mssql-scripter` | `sqlcmd` | ✅ | `GO` batch terminator |
| [Oracle](./oracle.md) | SQL\*Net | `exp` | `imp` | ✅ | `EXPORT:V` magic |
| [Elasticsearch](./elasticsearch.md) | HTTP | scroll API (native Go) | `_bulk` API (native Go) | ✅ | Final NDJSON line valid |
| [etcd](./etcd.md) | gRPC | `etcdctl snapshot save` | — | 🚫 sentinel | BoltDB magic `0xED0CDAED` |
| [ClickHouse](./clickhouse.md) | native | `clickhouse-client … FORMAT Native` | `INSERT … FORMAT Native` | ✅ (table must pre-exist) | Envelope-only |
| [Neo4j](./neo4j.md) | Bolt / FS | `neo4j-admin database dump` | `neo4j-admin database load` | ✅ (DB stopped) | Envelope-only |
| [SQLite](./sqlite.md) | file | `sqlite3 .dump` | `sqlite3` | ✅ | `COMMIT;` footer |

---

## Picking the right engine

| Question | Answer |
|---|---|
| My DB speaks Postgres wire but isn't Postgres (e.g. CockroachDB, YugabyteDB) | `cockroach` for Cockroach, `postgresql` for YugabyteDB |
| MySQL 8.0 with caching_sha2_password | `mysql` — the bundled `mariadb-dump 11.8` handles it |
| Cloud-managed MongoDB Atlas | `mongodb` — pass an `mongodb+srv://` connection string via `DUMP_OPTIONS` or `DB_HOST` |
| Redis Cluster | `redis` — point at one primary; RDB snapshot covers local shard only |
| SQL Server Always-On | `sqlserver` — point at the listener |
| Oracle RAC | `oracle` — use the SCAN listener in `DB_HOST` |
| Amazon OpenSearch | `elasticsearch` — compatible |
| Single-node etcd for Kubernetes | `etcd` — dumps are fast, trivial |
| Columnar analytics | `clickhouse` — one table per run |
| Graph | `neo4j` — community edition requires DB stopped |
| Embedded DBs | `sqlite` — file-based, no host |

---

## Restore-unsupported engines

Two engines return a sentinel error on restore because the restore operation
is fundamentally out of scope for a "dump tool":

- **Redis** (`ErrRedisRestoreUnsupported`) — RDB format must be placed at
  `<redis-data-dir>/dump.rdb` and the server restarted. Filesystem access
  and restart coordination are outside the tool's remit.
- **etcd** (`ErrEtcdRestoreUnsupported`) — `etcdctl snapshot restore`
  rebuilds a fresh data-dir with a new `--initial-cluster-token`, applied
  per-node across a fresh cluster. Needs coordinated multi-node orchestration.

Both are clearly logged with actionable next-step instructions.

---

## Client tools baked into the image

| Tool | Purpose | Apk package |
|---|---|---|
| `pg_dump`, `psql` | Postgres + Cockroach | `postgresql18-client` |
| `mariadb-dump`, `mariadb` | MySQL + MariaDB | `mariadb-client` |
| `mongodump`, `mongorestore` | MongoDB | `mongodb-tools` |
| `redis-cli` | Redis | `redis` |
| `sqlite3` | SQLite | `sqlite` |
| `etcdctl` | etcd | `etcd-ctl` |

These are the engines that run out-of-the-box. SQL Server, Oracle, ClickHouse
and Neo4j require building a custom image with the proprietary/JVM clients
installed — see each engine's page.

---

## Back

- [Docs home](../README.md)

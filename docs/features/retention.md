# Retention

Delete backups older than `RETENTION_DAYS` under a prefix.

Runs automatically at the start of every `dump` when `RETENTION_DAYS > 0`,
and also as the standalone [`cleanup`](../commands/cleanup.md) subcommand.

---

## Env vars

| Var | Default | Description |
|---|---|---|
| `RETENTION_DAYS` | `0` (disabled) | Days to keep |
| `PERIODICITY` | — | Picks the sub-prefix to sweep (`daily` / `weekly` / ...) |

---

## How the cutoff is computed

```
cutoff := today - RETENTION_DAYS     # today is UTC
```

Only the **path-embedded** date is compared — never the object's
`LastModified`. This is robust to:

- Storage-class transitions (S3 Glacier restore bumps `LastModified`)
- Re-uploads of the same key
- Bucket replication

The matcher parses `YYYY/MM/DD` from the day-folder in the key:

```
<prefix>/<periodicity>/YYYY/MM/DD/dump_*
                        ↑ parsed as the object's "effective date"
```

Malformed paths are **skipped** (never deleted), which is the safer default.

---

## Which files get deleted?

A key is considered a dump artefact if its suffix matches one of:

- `*.sql.gz`
- `*.archive.gz`
- `*.rdb.gz`
- `*.dmp.gz`
- `*.db.gz`
- `*.native.gz`
- `*.neo4j.gz`
- `*.ndjson.gz`

Other objects (`.lock` files, unknown suffixes, directories) are preserved
even when older than the cutoff. This avoids nuking lock files or
operator-placed bookkeeping objects.

---

## Examples

### Keep 30 daily dumps

```sh
-e PERIODICITY=daily -e RETENTION_DAYS=30
```

When running on a given day, everything under
`<prefix>/daily/YYYY/MM/DD/` with an embedded date older than
(today − 30 days) is deleted.

### Keep 2 years of weekly + 5 years of monthly

Two CronJobs, each with its own `PERIODICITY` and `RETENTION_DAYS`:

```yaml
# weekly backup + retention
env: [ {name: PERIODICITY, value: weekly}, {name: RETENTION_DAYS, value: "730"} ]

# monthly backup + retention
env: [ {name: PERIODICITY, value: monthly}, {name: RETENTION_DAYS, value: "1825"} ]
```

---

## Log output

```
retention cleanup prefix=<prefix>/daily/ retention_days=30 cutoff=YYYY-MM-DD
retention cleanup done deleted=7 kept=30 skipped=2
```

- `deleted` — removed
- `kept` — within the retention window
- `skipped` — matcher didn't recognise the path (preserved out of safety)

---

## Back

- [Docs home](../README.md)
- [`cleanup` subcommand](../commands/cleanup.md)

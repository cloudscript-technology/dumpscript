# Logging

Structured logs via `log/slog`, in two output flavours:

- **`json`** — one event per line (default, parseable by Loki / DataDog / etc.)
- **`console`** — human-friendly coloured output, powered by
  [`tint`](https://github.com/lmittmann/tint). Use in dev or when tailing
  live pods.

---

## Env / CLI

| Var / flag | Default | Description |
|---|---|---|
| `LOG_LEVEL` / `--log-level` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` / `--log-format` | `json` | `json` \| `console` |

CLI flags override env vars. Useful for debugging a specific run:

```sh
podman run --rm …  localhost/dumpscript:go-alpine --log-level=debug --log-format=console dump
```

---

## Shared attributes

Every event carries:

- `subcmd` — `dump`, `restore`, `cleanup`
- `db_type` — `postgresql`, `mysql`, etc.
- `backend` — `s3`, `azure`
- `periodicity` — `daily`, `weekly`, …
- `execution_id` — 16-char hex token, stable across a whole run. Use this
  to correlate with Slack and Prometheus output.

Additional fields appear per event — e.g. upload events add `key` and `size`.

---

## Console example

```
INFO  dumpscript starting  subcmd=dump db_type=postgresql backend=s3 host=pg.prod
INFO  ┌─ dump pipeline starting  execution_id=a1b2c3d4e5f60718
INFO  ├─ [1/4] preflight: verifying destination is reachable  prefix=pg
INFO  │   ✔ destination reachable  elapsed=3.2ms
INFO  ├─ [2/4] acquire lock  key=pg/daily/…/.lock
INFO  │   ✔ lock acquired
INFO  ├─ [3/4] dump + verify  host=pg.prod db_name=app
INFO  executing pg_dump  args=["-h","pg.prod","-p","5432",…]
INFO  │   ✔ dump produced  size=12.3MiB elapsed=4.2s
INFO  │   ✔ gzip envelope valid
INFO  │   ✔ content verified
INFO  ├─ [4/4] upload + notify  key=pg/daily/…/dump_*.sql.gz
INFO  │   ✔ uploaded  bytes=12893044 elapsed=1.9s
INFO  └─ dump pipeline complete  size=12.3MiB elapsed=6.1s
```

Durations are humanised (`4.2s`, `3.2ms`) and sizes use binary prefixes
(`12.3MiB`).

---

## JSON example

```json
{"time":"...","level":"INFO","msg":"dumpscript starting","subcmd":"dump","db_type":"postgresql","backend":"s3"}
{"time":"...","level":"INFO","msg":"┌─ dump pipeline starting","execution_id":"a1b2c3d4e5f60718"}
{"time":"...","level":"INFO","msg":"│   ✔ dump produced","size":12893044,"elapsed":4213000000}
```

In JSON mode, `size` is bytes and `elapsed` is nanoseconds — Prometheus /
log tools can post-process them.

---

## Correlating across systems

The same `execution_id` appears in:

- every log line of a run
- the Slack payload's `execution_id` field
- the `.lock` JSON payload (for forensics)
- the `dumpscript_result{execution_id=…}` metric

`grep <id>` across your log stream gives you the complete story of any
dump.

---

## Tips

- **Production**: keep `LOG_FORMAT=json` — Loki / DataDog can query on any
  field natively.
- **Dev / ad-hoc runs**: `--log-format=console` is kinder to your eyeballs.
- **Quieting third-party chattiness**: `pg_dump` / `mariadb-dump` stderr is
  passed through; set `DUMP_OPTIONS="--quiet"` (pg) or similar if too noisy.

---

## Back

- [Docs home](../README.md)
- [Configuration reference](../configuration.md)

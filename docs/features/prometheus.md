# Prometheus metrics

Push metrics to a Pushgateway (for batch-job visibility) or emit them on
stderr for log-based scraping.

---

## Env vars

| Var | Default | Description |
|---|---|---|
| `PROMETHEUS_ENABLED` | `false` | Master switch |
| `PROMETHEUS_PUSHGATEWAY_URL` | — | Push endpoint (e.g. `http://pushgateway:9091`) |
| `PROMETHEUS_JOB_NAME` | `dumpscript` | Job label |
| `PROMETHEUS_INSTANCE` | hostname | Instance label |
| `PROMETHEUS_LOG_ON_EXIT` | `false` | Also emit metrics text on stderr |

Both sinks can run at the same time — useful when the Pushgateway is
unreliable and you still want metrics captured via log shipping.

---

## Metrics exposed

| Name | Type | Labels | Description |
|---|---|---|---|
| `dumpscript_start_time_seconds` | Gauge | `db_type`, `outcome` | Unix ts of run start |
| `dumpscript_end_time_seconds` | Gauge | `db_type`, `outcome` | Unix ts of run end |
| `dumpscript_duration_seconds` | Gauge | `db_type`, `outcome` | Wall-clock duration |
| `dumpscript_artifact_bytes` | Gauge | `db_type` | Size of the dump file uploaded |
| `dumpscript_result` | Gauge | `db_type`, `outcome` (`success`/`failure`/`skipped`) | `1` for the terminal outcome |

All metrics carry the `job` / `instance` labels from the config.

---

## Pushgateway example

```sh
-e PROMETHEUS_ENABLED=true
-e PROMETHEUS_PUSHGATEWAY_URL=http://pushgateway.monitoring.svc:9091
-e PROMETHEUS_JOB_NAME=pg-daily-backup
```

Scrape the Pushgateway from Prometheus as usual:

```yaml
scrape_configs:
  - job_name: pushgateway
    honor_labels: true
    static_configs:
      - targets: ['pushgateway.monitoring.svc:9091']
```

---

## Log-based scraping example

```sh
-e PROMETHEUS_ENABLED=true
-e PROMETHEUS_LOG_ON_EXIT=true
-e PROMETHEUS_PUSHGATEWAY_URL=""      # skip push, only stderr emit
```

Stderr will contain, at the end of the run:

```
# TYPE dumpscript_duration_seconds gauge
dumpscript_duration_seconds{db_type="postgresql",outcome="success"} 6.12
# TYPE dumpscript_artifact_bytes gauge
dumpscript_artifact_bytes{db_type="postgresql"} 12900000
…
```

Ship these lines via Loki / Fluent Bit to a log analytics backend, or parse
them with a sidecar exporter.

---

## Example alert — no successful backup in 36 hours

```yaml
- alert: DumpscriptBackupStale
  expr: |
    time() - max by (db_type, instance) (
      dumpscript_end_time_seconds{outcome="success"}
    ) > 36 * 3600
  for: 15m
  labels: { severity: page }
  annotations:
    summary: "No successful {{ $labels.db_type }} backup in 36h on {{ $labels.instance }}"
```

Pairs well with a second alert on `dumpscript_result{outcome="failure"} > 0`
for acute failures.

---

## Back

- [Docs home](../README.md)
- [Configuration reference](../configuration.md)

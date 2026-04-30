# Slack notifications

Incoming-webhook integration. Four event kinds cover every terminal
outcome of a dump run.

---

## Env vars

| Var | Default | Description |
|---|---|---|
| `SLACK_WEBHOOK_URL` | — | Incoming webhook URL |
| `SLACK_CHANNEL` | `#alerts` | Override channel (else whatever the webhook defaults to) |
| `SLACK_USERNAME` | `DumpScript Bot` | Display name in Slack |
| `SLACK_NOTIFY_SUCCESS` | `false` | Also emit `EventSuccess` (not just failures) |

Leaving `SLACK_WEBHOOK_URL` unset disables notifications entirely (the
pipeline uses a Noop notifier).

---

## Event types

| Kind | Color | When | `SLACK_NOTIFY_SUCCESS` required? |
|---|---|---|---|
| `EventStart` | — | Run begins | no |
| `EventSuccess` | `good` (green) | Dump + upload succeeded | **yes** |
| `EventFailure` | `danger` (red) | Any pipeline error | no |
| `EventSkipped` | `warning` (yellow) | Lock already held by another run | no |

Default (`SLACK_NOTIFY_SUCCESS=false`) — the channel stays quiet on
success and only pings for failures / skips. Turn it on for auditable
"every backup fired" visibility.

---

## Payload shape

```json
{
  "channel": "#database-alerts",
  "username": "DumpScript Bot",
  "attachments": [{
    "fallback": "dumpscript: postgresql dump succeeded",
    "color":    "good",
    "title":    "dump succeeded",
    "fields": [
      { "title": "execution_id", "value": "…",    "short": true },
      { "title": "db_type",      "value": "postgresql", "short": true },
      { "title": "db_host",      "value": "pg.prod", "short": true },
      { "title": "size",         "value": "12.3 MiB", "short": true },
      { "title": "duration",     "value": "6.1s",    "short": true }
    ],
    "ts": 1714060800
  }]
}
```

All outcomes carry `execution_id` so you can correlate between Slack,
logs, and metrics.

---

## Setting up the webhook

1. Visit <https://api.slack.com/apps> → **Create New App** → **From scratch**.
2. Name it (e.g. "DumpScript Alerts"), pick your workspace.
3. Enable **Incoming Webhooks**, then **Add New Webhook to Workspace**.
4. Pick a default channel → copy the URL.

```sh
-e SLACK_WEBHOOK_URL="https://hooks.slack.com/services/T.../B.../…"
-e SLACK_CHANNEL="#database-alerts"
-e SLACK_NOTIFY_SUCCESS=true
```

Store the webhook URL in a Kubernetes Secret — they are bearer tokens.

---

## Testing locally

The e2e test `TestSlackNotification` spins up a tiny Python HTTP server,
points the dumper at it, and asserts the payload matches the expected
`"color":"danger"` shape. See `tests/e2e/features_test.go` for reference.

---

## Related

- [Locking](./locking.md) — explains why `EventSkipped` exists
- [Configuration reference](../configuration.md) — full env var list

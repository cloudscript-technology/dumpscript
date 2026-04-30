# Notifiers

5 canais suportados pelo `dumpscript`, todos com **auto-registration** —
adicionar um novo é 1 arquivo + 1 `init()`, igual aos engines.

| Notifier | Env vars | Quando ligar |
|---|---|---|
| **Slack** | `SLACK_WEBHOOK_URL`, `SLACK_CHANNEL`, `SLACK_USERNAME`, `SLACK_NOTIFY_SUCCESS` | Channel default em times Slack-first |
| **Discord** | `DISCORD_WEBHOOK_URL`, `DISCORD_USERNAME`, `DISCORD_NOTIFY_SUCCESS` | Times em Discord (gaming/SMB) |
| **Microsoft Teams** | `TEAMS_WEBHOOK_URL`, `TEAMS_NOTIFY_SUCCESS` | Conector legacy MessageCard |
| **Webhook genérico** | `WEBHOOK_URL`, `WEBHOOK_AUTH_HEADER`, `WEBHOOK_NOTIFY_SUCCESS` | PagerDuty Events API, Opsgenie, n8n, Zapier, endpoint custom |
| **Stdout JSON** | `NOTIFY_STDOUT`, `NOTIFY_STDOUT_SUCCESS` | Log-based scraping (CI dashboards, fluent-bit) |

Múltiplos canais coexistem: o `dumpscript` usa um `Multi` notifier que faz
fan-out pra todos os ativos sem short-circuit em falha parcial. Erros são
agregados via `errors.Join`.

---

## Tipos de evento

Todo notifier recebe os mesmos 4 kinds:

| Kind | Color (chat) | Quando |
|---|---|---|
| `EventStart` | — | Run iniciada (informativo) |
| `EventSuccess` | `good` (verde) | Dump + upload sucederam (opt-in via `*_NOTIFY_SUCCESS=true`) |
| `EventFailure` | `danger` (vermelho) | Qualquer erro do pipeline |
| `EventSkipped` | `warning` (amarelo) | Lock já em uso por outra run — exit 0, **não é falha** |

Default = só notifica failure/skipped. Set `*_NOTIFY_SUCCESS=true` se quer
auditar "todo backup rodou".

Cada payload carrega `execution_id` + `db_type` + `db_host` + `size` +
`duration` + (se houver) `err.Error()` — basta `grep <execution_id>` pra
correlacionar com logs e métricas.

---

## Slack

```sh
-e SLACK_WEBHOOK_URL="https://hooks.slack.com/services/T.../B.../..."
-e SLACK_CHANNEL="#database-alerts"
-e SLACK_USERNAME="DumpScript Bot"
-e SLACK_NOTIFY_SUCCESS=true
```

Setup do webhook:
1. <https://api.slack.com/apps> → Create New App → From scratch
2. Enable **Incoming Webhooks**
3. Add New Webhook to Workspace → escolhe canal
4. Copia a URL (é um bearer token disfarçado — guarda em `Secret`)

Detalhes da setup: [features/slack_notifications.md](./slack_notifications.md).

---

## Discord

```sh
-e DISCORD_WEBHOOK_URL="https://discord.com/api/webhooks/.../..."
-e DISCORD_USERNAME="DumpScript"
-e DISCORD_NOTIFY_SUCCESS=false
```

Setup:
1. Server settings → Integrations → Webhooks → New Webhook
2. Copia URL

Payload: shape `{content, username}` (Discord webhook protocol).

---

## Microsoft Teams

```sh
-e TEAMS_WEBHOOK_URL="https://outlook.office.com/webhook/..."
-e TEAMS_NOTIFY_SUCCESS=false
```

Setup (legacy connector — recomendado pra esta integração):
1. Channel → ⋯ → Connectors → Incoming Webhook
2. Configure → Create
3. Copia URL

Payload: `MessageCard` schema (`@type`, `themeColor`, `title`, `text`).
Cores por kind:
- success → `2ECC40` (verde)
- failure → `FF4136` (vermelho)
- skipped → `FFDC00` (amarelo)
- start   → `0074D9` (azul)

---

## Webhook genérico

POST JSON pra qualquer endpoint HTTP. Use pra integrar com:
- PagerDuty Events API v2
- Opsgenie
- n8n / Zapier / Make
- Endpoint interno de incident management

```sh
-e WEBHOOK_URL="https://events.pagerduty.com/v2/enqueue"
-e WEBHOOK_AUTH_HEADER="Token token=YOUR_INTEGRATION_KEY"
-e WEBHOOK_NOTIFY_SUCCESS=false
```

Payload (JSON):
```json
{
  "kind": "failure",
  "context": "postgres",
  "execution_id": "abc123def456",
  "path": "pg/daily/2026/04/27/dump_20260427_020000.sql.gz",
  "size": 12300000,
  "err": "pg_dump: connection lost"
}
```

`Authorization` header opcional — passe `WEBHOOK_AUTH_HEADER` raw (`Bearer xxx`,
`Token xxx`, `ApiKey xxx`, etc.).

---

## Stdout JSON

```sh
-e NOTIFY_STDOUT=true
-e NOTIFY_STDOUT_SUCCESS=true
```

Emite **uma linha JSON por evento** no stdout do pod. Útil pra:
- CI dashboards parseando logs
- Fluent-bit / Promtail / Vector roteando pra Loki/CloudWatch
- Sidecar exporters convertendo pra métricas

```json
{"event":"success","context":"postgres","execution_id":"abc123","path":"pg/daily/dump_*.sql.gz","size":12345}
```

---

## Multi-canal

Todos os notifiers acima são **independentes** — pode setar 1, 2, 3 ou
todos:

```sh
-e SLACK_WEBHOOK_URL="..."
-e DISCORD_WEBHOOK_URL="..."
-e WEBHOOK_URL="https://opsgenie.com/api/..."
-e WEBHOOK_AUTH_HEADER="GenieKey ..."
-e NOTIFY_STDOUT=true
```

`notify.New(cfg, log)` detecta os 4 ativos, retorna um `*Multi` que dispara
todos em paralelo. Falha em um (ex: Slack 503) **não suprime** os outros
nem aborta o pipeline — todas as falhas são agregadas via `errors.Join` e
logadas como warning.

Veja exemplo completo em
[`examples/operator/mariadb-multi-notifier.yaml`](../../examples/operator/mariadb-multi-notifier.yaml).

---

## Adicionando um notifier novo

Padrão self-register, igual aos engines/storages. Em `internal/notify/`,
crie um arquivo:

```go
package notify

func init() {
    Register("pagerduty", func(cfg *config.Config, log *slog.Logger) (Notifier, bool) {
        if cfg.PagerDuty.IntegrationKey == "" {
            return nil, false
        }
        return NewPagerDuty(cfg, log), true
    })
}

type PagerDuty struct{ ... }
func NewPagerDuty(cfg *config.Config, log *slog.Logger) *PagerDuty { ... }
func (p *PagerDuty) Notify(ctx context.Context, e Event) error { ... }
```

Zero edits em `factory.go`, zero `switch` hardcoded. O `notify.New`
detecta o novo notifier automaticamente.

---

## Back

- [Docs home](../README.md)
- [Slack setup detalhado](./slack_notifications.md)
- [Configuration reference](../configuration.md)

# Kubernetes Operator

`dumpscript-operator` torna o backup declarativo: você aplica um
`BackupSchedule` CR e o operator materializa o resto (CronJob, RBAC,
Secret refs, status agregado).

> Código em [`/operator`](../../operator). 16 samples prontos em
> [`examples/operator/`](../../examples/operator/).

---

## Por que usar o operator?

Em vez de manter manifestos K8s à mão (CronJob + Secret + ServiceAccount),
você aplica **um único CR** descrevendo intent, e o operator cuida do
resto. Compare:

|  | CronJob nativo | dumpscript-operator |
|---|---|---|
| `kubectl get` mostra última run? | ❌ tem que olhar Jobs filhos | ✅ `kubectl get backupschedule` mostra `LAST-SUCCESS` |
| Validation antecipada (admission) | só no pod startup | ✅ rejeita CR malformado no `apply` |
| Auto-default port/chunk por engine | não | ✅ `engine=postgres` → port=5432 etc |
| Resolve Secret refs declarativamente | manual em env | ✅ `credentialsSecretRef` |
| Auto-mount de volume Secret (GCS SA JSON) | manual | ✅ automático |
| RBAC scoping | mistura SA+Secret+CronJob | ✅ um CRD |
| Métricas Prometheus do estado | não | ✅ `dumpscript_backup_total` / `_duration_seconds` no `/metrics` do operator |
| Conditions + Events para debug | não | ✅ `kubectl describe` mostra `Reconciled`, `LastRunSucceeded`, `LastRunFailed` |
| Status agregado (totalRuns, consecutiveFailures, etc.) | não | ✅ `kubectl get backupschedule` |
| Tunables por engine type-safe (mongo authSource, sqlite volume, S3 SSE-KMS) | manual via env | ✅ first-class fields |
| Pod scheduling (resources, nodeSelector, tolerations, affinity) | manual em CronJob | ✅ no CRD |
| `dryRun`, `compression`, `dumpRetry`, `lockGracePeriod` | manual via env | ✅ no CRD |

---

## CRDs

O operator define dois CRDs no API group
**`dumpscript.cloudscript.com.br/v1alpha1`**:

### `BackupSchedule`
Backup recorrente. O reconciler materializa como uma `batch/v1.CronJob`
gerenciada, com owner-ref pra GC automático e status agregado.

→ [BackupSchedule reference](./backupschedule.md)

### `Restore`
One-shot restore. O reconciler cria uma `batch/v1.Job` e atualiza
`status.phase` (`Pending` → `Running` → `Succeeded`/`Failed`).

→ [Restore reference](./restore.md)

---

## Convenção `*SecretRef`

**Regra**: todo field cujo nome termina em `SecretRef` aponta pra um
`Secret` no mesmo namespace do CR. Tudo o resto é valor inline.

```yaml
spec:
  database:
    type: postgresql
    host: pg.prod.svc                  # inline
    name: app                          # inline
    credentialsSecretRef:              # → Secret
      name: pg-prod-creds
  storage:
    backend: s3
    s3:
      bucket: prod-backups             # inline
      region: us-east-1                # inline
      roleARN: arn:aws:iam::...        # inline (não é segredo)
  notifications:
    slack:
      webhookSecretRef:                # → Secret
        name: slack-webhook
        key: url
      channel: "#alerts"               # inline
```

→ [Secret refs reference](./secret-refs.md) (todos os campos catalogados)

---

## Quick start

```sh
# 1. Instala os CRDs
cd operator && make install

# 2. Roda o controller (modo dev, fora do cluster)
make run

# OU faz deploy in-cluster
make docker-build docker-push IMG=ghcr.io/your-org/dumpscript-operator:dev
make deploy IMG=ghcr.io/your-org/dumpscript-operator:dev

# 3. Cria o Secret com as credenciais do DB
kubectl create secret generic pg-prod-creds -n backups \
  --from-literal=username=backup \
  --from-literal=password=secret

# 4. Aplica um BackupSchedule
kubectl apply -f examples/operator/postgres-s3-irsa.yaml -n backups

# 5. Confere o status
kubectl get backupschedule -n backups
# NAME            SCHEDULE     ENGINE       BACKEND   LAST-SUCCESS   LAST-FAILURE   SUSPENDED
# pg-prod-daily   0 2 * * *    postgresql   s3        2026-04-27     <none>         false
```

---

## Páginas

- [Installation](./installation.md) — make targets, helm, OperatorHub bundle
- [BackupSchedule](./backupschedule.md) — todos os fields do spec + status
- [Restore](./restore.md) — fields + lifecycle phases
- [Secret refs](./secret-refs.md) — catálogo completo dos `*SecretRef`
- [Architecture](./architecture.md) — reconciler flow, owner refs, watches

## Examples

Todos em [`examples/operator/`](../../examples/operator/) (16 samples + README):

- `postgres-s3-irsa.yaml` — canonical, IRSA-based
- `postgres-gcs-workload-identity.yaml` — zero secrets via Workload Identity
- `postgres-azure-sharedkey.yaml` — Azure Blob Shared Key
- `mariadb-multi-notifier.yaml` — 5 notifiers ativos simultaneamente
- ... e 12 outros cobrindo todo engine + backend

---

## Back

- [Docs home](../README.md)
- [Operator source](../../operator)
- [Operator examples](../../examples/operator/)

# `Restore` reference

CRD `Restore` (`dumpscript.cloudscript.com.br/v1alpha1`) — restore one-shot
declarativo. O reconciler cria uma `batch/v1.Job` espelhando o spec e
atualiza `status.phase` ao longo do ciclo de vida.

Re-aplicar o mesmo CR é idempotente; pra restaurar o mesmo dump de novo,
use um `metadata.name` diferente.

---

## Spec

| Campo | Tipo | Default | Descrição |
|---|---|---|---|
| `sourceKey` | string | required | Object key completo dentro do bucket (ex: `pg/daily/YYYY/MM/DD/dump_*.sql.gz`) |
| `database` | DatabaseSpec | required | DB destino (mesmo shape do `BackupSchedule.spec.database`) |
| `storage` | StorageSpec | required | Backend de origem (mesmo shape do `BackupSchedule.spec.storage`) |
| `createDB` | bool | `false` | Emite `CREATE DATABASE` antes do restore (Postgres/MySQL/MariaDB). No-op pra Mongo/CRDB |
| `notifications` | NotificationsSpec | — | Notificadores |
| `image` | string | `ghcr.io/cloudscript-technology/dumpscript:latest` | Override |
| `serviceAccountName` | string | — | KSA pra IRSA/WI |
| `ttlSecondsAfterFinished` | *int32 | `86400` (24h) | TTL do Job pós-conclusão |
| `imagePullPolicy` | enum | — | `Always` \| `Never` \| `IfNotPresent` |
| `imagePullSecrets` | []corev1.LocalObjectReference | — | Pull secrets do registry privado |
| `dryRun` | bool | `false` | Valida config + reachability e pula o restore |
| `compression` | enum | — | `gzip` \| `zstd` — codec do source artifact (auto-detectado pela extensão `.gz`/`.zst`) |
| `restoreTimeout` | *metav1.Duration | `2h` | Hard-cap do `restore` (passa como `RESTORE_TIMEOUT`) |
| `verifyContent` | *bool | `true` | Habilita verifier pós-restore (TCP probe no `database.host:port`) |
| `workDir` | string | `/dumpscript` | Scratch dir |
| `logLevel` | enum | `info` | `debug` \| `info` \| `warn` \| `error` |
| `logFormat` | enum | `json` | `json` \| `console` |
| `metricsListen` | string | empty | `:9090` para expor `/metrics` no pod |
| `prometheus` | *PrometheusSpec | — | Pushgateway (mesmo shape do BackupSchedule) |
| `backoffLimit` | *int32 | `0` | Job's BackoffLimit (restores raramente se beneficiam de retry) |
| `activeDeadlineSeconds` | *int64 | — | Mata o pod após N segundos rodando |
| `resources` | corev1.ResourceRequirements | — | Limits/requests do container |
| `nodeSelector` | map[string]string | — | Pod scheduling |
| `tolerations` | []corev1.Toleration | — | Pod scheduling |
| `affinity` | *corev1.Affinity | — | Pod scheduling |
| `priorityClassName` | string | — | Pod priority |
| `extraEnv` | []corev1.EnvVar | — | Env vars extras (operator-managed vencem) |

`database` / `storage` / `notifications` têm o mesmo shape do
`BackupSchedule` — ver [BackupSchedule reference](./backupschedule.md#databasespec).

---

## Status

| Campo | Tipo | Atualizado por |
|---|---|---|
| `phase` | enum | reconciler — `Pending` \| `Running` \| `Succeeded` \| `Failed` |
| `jobName` | string | nome do `batch/v1.Job` criado |
| `startedAt` | *metav1.Time | quando o Job começou |
| `completedAt` | *metav1.Time | quando terminou (success ou fail) |
| `durationSeconds` | int64 | Duração de StartedAt → CompletedAt (0 enquanto rodando) |
| `message` | string | Texto humano: sucesso traz "restore from <key> completed", falha traz job + attempts |
| `observedGeneration` | int64 | Generation observada no último reconcile |
| `conditions` | []metav1.Condition | `Ready` reflete o phase (True/False/Unknown) |

`kubectl get restore` (default columns):

```
NAME                              PHASE       ENGINE       SOURCE                                            READY  STARTED  COMPLETED  AGE
pg-staging-restore-from-prod      Succeeded   postgresql   pg/daily/2026/04/26/dump_*.sql.gz                 True   2m       1m         3m
```

`kubectl get restore -o wide`:

```
+ JOB                            DURATION  BACKEND  REASON              MESSAGE
  restore-pg-staging-restore...  92        s3       RestoreSucceeded    restore from pg/... completed successfully
```

### Events

| Reason | Type | Quando |
|---|---|---|
| `RestoreRunning` | Normal | Job criado |
| `RestoreSucceeded` | Normal | Job observado em sucesso terminal |
| `RestoreFailed` | Warning | Job observado em falha terminal |
| `RestoreJobError` | Warning | Falha ao criar o Job |

---

## Lifecycle

```mermaid
stateDiagram-v2
  [*] --> Pending: CR applied
  Pending --> Running: reconciler creates Job
  Running --> Succeeded: Job CompletionTime != nil
  Running --> Failed: Job Failed > 0
  Succeeded --> [*]: TTL expira → Job removido (status preservado)
  Failed --> [*]: idem
```

- **Idempotência**: o reconciler usa nome determinístico do Job
  (`restore-<cr-name>`); re-apply do mesmo CR não duplica.
- **TTL**: `ttlSecondsAfterFinished` controla quando o Job filho é
  garbage-collected. O CR `Restore` em si não é deletado — fica
  acessível pra `kubectl describe` indefinidamente.
- **Owner reference**: o Job tem ownerRef apontando pro CR; deletar o
  CR remove o Job + pod imediatamente.

---

## Engine-specific gotchas

| Engine | Detalhe |
|---|---|
| **Redis** | Não suportado — retorna `ErrRedisRestoreUnsupported` (RDB precisa stop+replace+restart). |
| **etcd** | Não suportado — `etcdctl snapshot restore` rebuilda data-dir, requer coordenação multi-node. |
| **Cockroach** | DB destino **deve pré-existir**; `createDB: true` é no-op (CRDB não aceita CREATE DATABASE no replay). |
| **MongoDB** | `createDB: true` é redundante (Mongo cria collections sob demanda). |
| **ClickHouse** | Tabela destino **deve pré-existir** com schema compatível (Native format não preserva DDL). |
| **Neo4j** | DB precisa estar **stopped** (Community Edition limitation). |
| **SQLite** | Cria o arquivo se não existir. |

---

## Examples

| Sample | Cenário |
|---|---|
| [restore-postgres.yaml](../../examples/operator/restore-postgres.yaml) | + `createDB: true` |
| [restore-mongodb-create-db.yaml](../../examples/operator/restore-mongodb-create-db.yaml) | Atlas roundtrip |
| [restore-cockroach.yaml](../../examples/operator/restore-cockroach.yaml) | DB destino pré-criado |

---

## Back

- [Operator overview](./README.md)
- [BackupSchedule reference](./backupschedule.md)
- [Secret refs](./secret-refs.md)

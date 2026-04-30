# `BackupSchedule` reference

CRD `BackupSchedule` (`dumpscript.cloudscript.com.br/v1alpha1`) — backup
recorrente. O reconciler cria e mantém uma `batch/v1.CronJob` espelhando
o spec, e propaga o estado das runs filhas pro `status`.

---

## Spec

| Campo | Tipo | Default | Descrição |
|---|---|---|---|
| `schedule` | string | required | Cron expression (5 campos POSIX). Ex: `"0 2 * * *"` |
| `periodicity` | enum | required | `daily` \| `weekly` \| `monthly` \| `yearly` — controla o prefix layout do storage e a janela de retention |
| `retentionDays` | int | `0` | Dumps mais antigos que isso são deletados antes de cada run. `0` desativa |
| `database` | [DatabaseSpec](#databasespec) | required | DB de origem |
| `storage` | [StorageSpec](#storagespec) | required | Backend de destino |
| `notifications` | [NotificationsSpec](#notificationsspec) | — | Notificadores |
| `image` | string | `ghcr.io/cloudscript-technology/dumpscript:latest` | Override da imagem |
| `serviceAccountName` | string | — | KSA para IRSA (EKS) ou Workload Identity (GKE) |
| `suspend` | bool | `false` | Pausa o CronJob sem deletar |
| `failedJobsHistoryLimit` | *int32 | `7` | Passa pro CronJob gerado |
| `successfulJobsHistoryLimit` | *int32 | `3` | Passa pro CronJob gerado |
| `imagePullPolicy` | enum | — | `Always` \| `Never` \| `IfNotPresent` |
| `imagePullSecrets` | []corev1.LocalObjectReference | — | Pull secrets do registry privado |
| `dryRun` | bool | `false` | Valida config + reachability e pula dump+upload (para smoke-testar uma schedule recém aplicada) |
| `compression` | enum | `gzip` | `gzip` \| `zstd` — codec do artifact em disco |
| `dumpTimeout` | *metav1.Duration | `2h` | Hard-cap do `dump` (passa como `DUMP_TIMEOUT`) |
| `lockGracePeriod` | *metav1.Duration | `24h` | Lock antigo demais → take-over; `0` desativa |
| `verifyContent` | *bool | `true` | Habilita verifier por engine pós-dump |
| `workDir` | string | `/dumpscript` | Scratch dir dentro do pod |
| `logLevel` | enum | `info` | `debug` \| `info` \| `warn` \| `error` |
| `logFormat` | enum | `json` | `json` \| `console` |
| `metricsListen` | string | empty | `:9090` para expor `/metrics` no pod (Pushgateway via `prometheus.*`) |
| `dumpRetry` | *RetryPolicy | `{3, 5s, 5m}` | Retry exponencial em falhas transientes |
| `prometheus` | *PrometheusSpec | — | Pushgateway-based metrics |
| `concurrencyPolicy` | enum | `Forbid` | `Forbid` \| `Allow` \| `Replace` |
| `startingDeadlineSeconds` | *int64 | — | CronJob skip se atrasou demais |
| `backoffLimit` | *int32 | `0` | Job's BackoffLimit |
| `activeDeadlineSeconds` | *int64 | — | Mata o pod após N segundos rodando |
| `resources` | corev1.ResourceRequirements | — | Limits/requests do container dumpscript |
| `nodeSelector` | map[string]string | — | Pod scheduling |
| `tolerations` | []corev1.Toleration | — | Pod scheduling |
| `affinity` | *corev1.Affinity | — | Pod scheduling |
| `priorityClassName` | string | — | Pod priority |
| `extraEnv` | []corev1.EnvVar | — | Env vars extras (operator-managed vencem em colisão) |

### `dumpRetry:` (RetryPolicy)

| Campo | Tipo | Default | Descrição |
|---|---|---|---|
| `maxAttempts` | int32 | `3` | Total de tentativas (1 = sem retry, mas passa pelo decorator) |
| `initialBackoff` | *metav1.Duration | `5s` | Delay inicial; dobra a cada retry |
| `maxBackoff` | *metav1.Duration | `5m` | Cap do exponential backoff |

### `prometheus:` (PrometheusSpec)

| Campo | Tipo | Default | Descrição |
|---|---|---|---|
| `enabled` | bool | `false` | Liga emissão de métricas |
| `pushgatewayURL` | string | — | URL do Pushgateway |
| `jobName` | string | `dumpscript` | Label `job` |
| `instance` | string | — | Label `instance` |
| `logOnExit` | bool | `false` | Imprime as métricas no stderr ao sair (debug) |

---

## DatabaseSpec

| Campo | Tipo | Notas |
|---|---|---|
| `type` | enum | `postgresql` \| `mysql` \| `mariadb` \| `mongodb` \| `cockroach` \| `redis` \| `sqlserver` \| `oracle` \| `elasticsearch` \| `etcd` \| `clickhouse` \| `neo4j` \| `sqlite` |
| `host` | string | Não required pra `sqlite` (usa `name` como path do arquivo) |
| `port` | int32 | Override do default por engine |
| `name` | string | DB / index / `db.table` (clickhouse) / path do `.sqlite` |
| `credentialsSecretRef` | [DBCredentialsSecretRef](./secret-refs.md#multi-key--databasecredentialssecretref-dbcredentialssecretref) | Opcional pra `redis`/`etcd`/`elasticsearch`/`sqlite` |
| `options` | string | Flags raw pro engine CLI (use só pra flags **não-sensíveis**) |
| `optionsSecretRef` | [SecretKeyRef](./secret-refs.md#single-key-name-key) | DUMP_OPTIONS via Secret (use quando contém token) |
| `mongodb.authSource` | string | `admin` quando o user está no admin DB. Operator anexa `--authenticationDatabase=<value>` ao DUMP_OPTIONS |
| `postgresql.version` | string | Informativo. Maps para `POSTGRES_VERSION` env (default `16` no binário) |
| `mysql.version` | string | Maps para `MYSQL_VERSION` env (default `8.0`) |
| `mariadb.version` | string | Maps para `MARIADB_VERSION` env (default `11.4`) |
| `volume.mountPath` | string | Required quando `volume` está set; mount path no pod (ex: `/data`) |
| `volume.persistentVolumeClaim` | *corev1.PersistentVolumeClaimVolumeSource | PVC pra DBs file-based (sqlite) |
| `volume.emptyDir` | *corev1.EmptyDirVolumeSource | Util quando initContainer popula o arquivo |
| `volume.configMap` | *corev1.ConfigMapVolumeSource | SQLite tiny shipped via ConfigMap |
| `volume.secret` | *corev1.SecretVolumeSource | DB file via Secret |

`optionsSecretRef` ganha precedência sobre `options` quando ambos setados.

---

## StorageSpec

| Campo | Tipo | Notas |
|---|---|---|
| `backend` | enum | `s3` \| `azure` \| `gcs` |
| `s3` | *S3Storage | required quando `backend=s3` |
| `azure` | *AzureStorage | required quando `backend=azure` |
| `gcs` | *GCSStorage | required quando `backend=gcs` |
| `uploadCutoff` | string | default `200M` |
| `chunkSize` | string | default `100M` |
| `concurrency` | int32 | default `4` |

### `s3:` block

| Campo | Tipo | Notas |
|---|---|---|
| `bucket` | string | required |
| `prefix` | string | — |
| `region` | string | required pra AWS, qualquer pra MinIO |
| `endpointURL` | string | override pra MinIO/GCS-HMAC/Wasabi/B2 |
| `storageClass` | string | `STANDARD_IA`, `GLACIER` (AWS only) |
| `roleARN` | string | IRSA assume-role; omita pra usar `credentialsSecretRef` |
| `credentialsSecretRef` | *S3CredentialsSecretRef | omita pra IRSA |
| `sse` | string | Server-side encryption: `AES256` ou `aws:kms`. Empty desabilita |
| `sseKMSKeyID` | string | KMS key ARN/alias quando `sse=aws:kms`. Empty usa o default da bucket |

### `azure:` block

| Campo | Tipo | Notas |
|---|---|---|
| `account` | string | required |
| `container` | string | required |
| `prefix` | string | — |
| `endpoint` | string | override (Azurite, Gov clouds) |
| `credentialsSecretRef` | *AzureCredentialsSecretRef | Shared Key OU SAS token |

### `gcs:` block

| Campo | Tipo | Notas |
|---|---|---|
| `bucket` | string | required |
| `prefix` | string | — |
| `projectID` | string | obrigatório só se SA não embute |
| `credentialsSecretRef` | *GCSCredentialsSecretRef | omita pra Workload Identity |

---

## NotificationsSpec

| Campo | Tipo | Notas |
|---|---|---|
| `notifySuccess` | bool | `false` por default — só notifica falha/skipped |
| `slack` | *SlackNotifier | `{webhookSecretRef, channel?, username?}` |
| `discord` | *DiscordNotifier | `{webhookSecretRef, username?}` |
| `teams` | *TeamsNotifier | `{webhookSecretRef}` |
| `webhook` | *WebhookNotifier | `{urlSecretRef, authHeaderSecretRef?}` (PagerDuty etc.) |
| `stdout` | bool | emite JSON line por evento no stdout do pod |

Múltiplos notifiers são suportados — todos disparam em paralelo, falha em
um não suprime os outros.

→ Detalhe completo dos refs em [secret-refs.md](./secret-refs.md).

---

## Status

| Campo | Tipo | Atualizado por |
|---|---|---|
| `lastScheduleTime` | *metav1.Time | reconciler — wall-clock da última run disparada |
| `lastSuccessTime` | *metav1.Time | reconciler — `CompletionTime` do Job mais recente que sucedeu |
| `lastFailureTime` | *metav1.Time | reconciler — `CreationTimestamp` do Job mais recente que falhou |
| `lastRetentionTime` | *metav1.Time | mirror de `lastSuccessTime` quando `RetentionDays > 0`. Best-effort |
| `lastJobName` | string | Nome do Job terminal mais recente (success ou failure) |
| `lastDurationSeconds` | int64 | Duração do Job terminal mais recente |
| `totalRuns` | int64 | Total de Jobs terminais observados (limitado por history limits) |
| `consecutiveFailures` | int32 | Falhas seguidas desde o último success. Reset a 0 no próximo success |
| `currentRun` | string | nome do Job ativo (`""` quando idle) |
| `observedGeneration` | int64 | Generation observada pelo último reconcile |
| `conditions` | []metav1.Condition | padrão K8s — `Ready` reflete o último run terminal |

`kubectl get backupschedule` (default columns):

```
NAME            SCHEDULE     ENGINE       BACKEND   SUSPENDED   READY   LAST-SUCCESS   AGE
pg-prod-daily   0 2 * * *    postgresql   s3        false       True    3h             7d
```

`kubectl get backupschedule -o wide` (priority=1 columns):

```
+ PERIODICITY  RETENTION  LAST-FAILURE  CURRENT-RUN  LAST-JOB                LAST-DURATION  TOTAL-RUNS  FAILURES  REASON              MESSAGE
  daily        30         <none>        <none>       pg-prod-daily-29063840  251            87          0         LastRunSucceeded    most recent run succeeded
```

### Events

O reconciler emite Events na CR (visíveis via `kubectl describe`):

| Reason | Type | Quando |
|---|---|---|
| `Reconciled` | Normal | CronJob criado/atualizado |
| `LastRunSucceeded` | Normal | Job terminal observado pela primeira vez com sucesso |
| `LastRunFailed` | Warning | Job terminal observado pela primeira vez com falha |
| `CronJobError` | Warning | Falha ao criar/atualizar a CronJob |

---

## Lifecycle

```mermaid
flowchart LR
  A[Apply CR] --> B[Reconcile: build CronJob]
  B --> C[Set ownerRef + Create/Update]
  C --> D[Owns CronJob]
  D --> E[Watch Jobs filhos via label]
  E --> F[refreshStatus: last-success/failure/current]
  F -->|delta| G[Status().Update]
  G --> A
```

- **Owner reference**: deletar o CR remove a CronJob (GC nativo K8s).
- **Suspend toggle**: `kubectl patch backupschedule X -p '{"spec":{"suspend":true}}' --type=merge` propaga pra CronJob; reverter idem.
- **Concurrency**: `concurrencyPolicy: Forbid` no CronJob gerado +
  distributed `.lock` no bucket evitam runs simultâneas (ver
  [Locking](../features/locking.md)).

---

## Examples

| Sample | Cenário |
|---|---|
| [postgres-s3-irsa.yaml](../../examples/operator/postgres-s3-irsa.yaml) | Canonical — IRSA |
| [postgres-gcs-workload-identity.yaml](../../examples/operator/postgres-gcs-workload-identity.yaml) | Zero-secret via WI |
| [mariadb-multi-notifier.yaml](../../examples/operator/mariadb-multi-notifier.yaml) | 5 notifiers ativos |
| [postgres-cluster-pg-dumpall.yaml](../../examples/operator/postgres-cluster-pg-dumpall.yaml) | `name` vazio → `pg_dumpall` |
| [suspended.yaml](../../examples/operator/suspended.yaml) | `suspend: true` |

Lista completa em [`examples/operator/README.md`](../../examples/operator/README.md).

---

## Back

- [Operator overview](./README.md)
- [Restore reference](./restore.md)
- [Secret refs](./secret-refs.md)

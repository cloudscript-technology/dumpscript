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
| `currentRun` | string | nome do Job ativo (`""` quando idle) |
| `conditions` | []metav1.Condition | padrão K8s — types comuns: `Ready`, `Healthy` |

`kubectl get backupschedule` exibe via printcolumn:

```
NAME            SCHEDULE     ENGINE       BACKEND   LAST-SUCCESS   LAST-FAILURE   SUSPENDED
pg-prod-daily   0 2 * * *    postgresql   s3        2026-04-27     <none>         false
```

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

# Operator examples

CRs de exemplo para o `dumpscript-operator` — copie, troque os
nomes/credenciais, aplique. Cada arquivo cobre uma combinação realista de
**engine + storage backend + auth mode + notifiers**.

> **CRDs do operator** ficam em [`../../operator/config/crd/bases/`](../../operator/config/crd/bases/).
> Os samples aqui assumem que os CRDs já estão instalados (`make install`
> dentro de `../../operator`).

---

## BackupSchedule

### Storage backends (mesma engine `postgresql` para isolar a variável)

| Arquivo | Backend | Auth |
|---|---|---|
| [`postgres-s3-irsa.yaml`](./postgres-s3-irsa.yaml) | S3 (AWS) | IRSA — sem static keys no cluster |
| [`postgres-gcs-workload-identity.yaml`](./postgres-gcs-workload-identity.yaml) | GCS nativo | Workload Identity (GKE) — **zero secrets** |
| [`postgres-azure-sharedkey.yaml`](./postgres-azure-sharedkey.yaml) | Azure Blob | Shared Key |
| [`mysql-minio.yaml`](./mysql-minio.yaml) | MinIO (S3-compat) | Static keys + endpoint override |

### Variantes Postgres

| Arquivo | Cenário |
|---|---|
| [`postgres-cluster-pg-dumpall.yaml`](./postgres-cluster-pg-dumpall.yaml) | `DB_NAME` vazio → `pg_dumpall` (todo o cluster) |

### Engines diversos (S3 como backend default)

| Arquivo | Engine | Notas |
|---|---|---|
| [`mariadb-multi-notifier.yaml`](./mariadb-multi-notifier.yaml) | MariaDB | + 5 notifiers ativos simultâneos |
| [`mongodb-atlas.yaml`](./mongodb-atlas.yaml) | MongoDB Atlas | SRV connection string |
| [`cockroach-insecure.yaml`](./cockroach-insecure.yaml) | CockroachDB | `--insecure` (no TLS) |
| [`redis-dump-only.yaml`](./redis-dump-only.yaml) | Redis | Restore não suportado pela ferramenta |
| [`etcd-k8s-control-plane.yaml`](./etcd-k8s-control-plane.yaml) | etcd | Snapshot do control plane K8s |
| [`elasticsearch-https-basic.yaml`](./elasticsearch-https-basic.yaml) | Elasticsearch | TLS + basic auth |
| [`sqlite-pvc.yaml`](./sqlite-pvc.yaml) | SQLite | Acessa o `.sqlite` via PVC compartilhada |

### Modos especiais

| Arquivo | Cenário |
|---|---|
| [`suspended.yaml`](./suspended.yaml) | Schedule pausado (`spec.suspend: true`) sem deletar |

## Restore

| Arquivo | Cenário |
|---|---|
| [`restore-postgres.yaml`](./restore-postgres.yaml) | One-shot Postgres restore de S3 |
| [`restore-mongodb-create-db.yaml`](./restore-mongodb-create-db.yaml) | Mongo Atlas + `createDB: true` |
| [`restore-cockroach.yaml`](./restore-cockroach.yaml) | CockroachDB restore via `psql` |

---

## Convenções nos exemplos

### Secret refs — sufixo `SecretRef`

**Regra**: todo field cujo nome termina em `SecretRef` aponta pra um
`Secret` no mesmo namespace do CR. Tudo o resto é valor inline.

| Field | Tipo | O que aponta |
|---|---|---|
| `database.credentialsSecretRef` | `{name, usernameKey?, passwordKey?}` | Secret com user/password (defaults: `username`/`password`) |
| `database.optionsSecretRef` | `{name, key}` | Secret com a string `DUMP_OPTIONS` (use quando contém token) |
| `storage.s3.credentialsSecretRef` | `{name, accessKeyIDKey?, secretAccessKeyKey?, sessionTokenKey?}` | AWS keys estáticas (defaults: `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`) |
| `storage.azure.credentialsSecretRef` | `{name, sharedKeyKey?, sasTokenKey?}` | Storage Account Shared Key OU SAS token |
| `storage.gcs.credentialsSecretRef` | `{name, keyFile?}` | Service Account JSON (default key: `key.json`); montado como volume read-only |
| `notifications.slack.webhookSecretRef` / `discord.webhookSecretRef` / `teams.webhookSecretRef` | `{name, key}` | Incoming Webhook URL |
| `notifications.webhook.urlSecretRef` | `{name, key}` | URL do webhook genérico |
| `notifications.webhook.authHeaderSecretRef` | `{name, key}` | Valor do header `Authorization` (`Bearer ...`, `ApiKey ...`) |

Você cria os Secrets separadamente — veja o cabeçalho de cada YAML pra
saber quais keys ele espera.

### Outros padrões

- **Imagens** apontam para `ghcr.io/cloudscript-technology/dumpscript:latest`
  por default. Sobreescreva via `spec.image` quando precisar de variantes
  (mssql, oracle, neo4j com tools embutidas).
- **ServiceAccountName** é setado nos exemplos que usam Workload Identity /
  IRSA. O KSA precisa existir e estar anotado com o GSA/Role correspondente
  — isso é responsabilidade do operador do cluster, não do CR.

---

## Engines não cobertos com sample

`sqlserver`, `oracle`, `neo4j`, `clickhouse` — todos suportados pela
ferramenta, mas exigem **imagens custom** com clientes proprietários (ver
`docs/engines/`). Quando você tiver a imagem custom, basta trocar
`spec.image` em qualquer um dos samples acima e ajustar `spec.database.type`.

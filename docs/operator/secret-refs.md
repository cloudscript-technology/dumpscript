# Secret refs — catálogo completo

A regra única do CRD do operator:

> **Todo field cujo nome termina em `SecretRef` aponta pra um `Secret`
> no mesmo namespace do CR. Tudo o resto é valor inline.**

Esta página cataloga **cada** `*SecretRef` da API, com:
- shape esperado (single-key vs multi-key)
- nomes default das keys (que você pode overridar)
- exemplo de Secret + CR

---

## Single-key (`{name, key}`)

Aponta pra **uma** key dentro de um Secret.

| Field | Conteúdo |
|---|---|
| `database.optionsSecretRef` | string `DUMP_OPTIONS` (use quando contém token) |
| `notifications.slack.webhookSecretRef` | URL do Slack incoming webhook |
| `notifications.discord.webhookSecretRef` | URL do Discord webhook |
| `notifications.teams.webhookSecretRef` | URL do MS Teams webhook |
| `notifications.webhook.urlSecretRef` | URL do webhook genérico (PagerDuty, Opsgenie, n8n) |
| `notifications.webhook.authHeaderSecretRef` | valor raw do header `Authorization` |

**Shape** (Go: `SecretKeyRef`):
```yaml
slack:
  webhookSecretRef:
    name: slack-webhook        # Secret name (mesmo namespace do CR)
    key:  url                  # key dentro do Secret
```

**Exemplo de Secret**:
```sh
kubectl create secret generic slack-webhook -n backups \
  --from-literal=url='https://hooks.slack.com/services/T.../B.../...'
```

---

## Multi-key — `database.credentialsSecretRef` (`DBCredentialsSecretRef`)

Username + password em um Secret. Defaults `username` / `password`,
overridáveis.

```yaml
database:
  credentialsSecretRef:
    name: pg-prod-creds
    # usernameKey: user      # default: "username"
    # passwordKey: pw        # default: "password"
```

Secret esperado:
```sh
kubectl create secret generic pg-prod-creds -n backups \
  --from-literal=username=backup \
  --from-literal=password=s3cret
```

---

## Multi-key — `storage.s3.credentialsSecretRef` (`S3CredentialsSecretRef`)

AWS access keys estáticas. Omitir quando estiver usando IRSA.

```yaml
storage:
  s3:
    bucket: my-bucket
    credentialsSecretRef:
      name: aws-creds
      # accessKeyIDKey:     ID                  # default: "AWS_ACCESS_KEY_ID"
      # secretAccessKeyKey: SECRET              # default: "AWS_SECRET_ACCESS_KEY"
      # sessionTokenKey:    TOKEN               # opt-in (STS)
```

Secret esperado:
```sh
kubectl create secret generic aws-creds -n backups \
  --from-literal=AWS_ACCESS_KEY_ID=AKIA... \
  --from-literal=AWS_SECRET_ACCESS_KEY=...
```

---

## Multi-key — `storage.azure.credentialsSecretRef` (`AzureCredentialsSecretRef`)

Shared Key OU SAS token (escolha um).

```yaml
storage:
  azure:
    account: prodbackups
    container: dumps
    credentialsSecretRef:
      name: azure-creds
      sharedKeyKey: sharedKey      # OU
      # sasTokenKey: sasToken
```

---

## Multi-key — `storage.gcs.credentialsSecretRef` (`GCSCredentialsSecretRef`)

Service-Account JSON. **Montado como volume Secret read-only** em
`/var/run/gcs/<keyFile>` automaticamente — não vira env var.

```yaml
storage:
  gcs:
    bucket: my-bucket-gcs
    credentialsSecretRef:
      name: gcs-sa
      # keyFile: key.json          # default
```

Secret esperado:
```sh
kubectl create secret generic gcs-sa -n backups \
  --from-file=key.json=/path/to/sa-key.json
```

> Se você está em GKE com Workload Identity, **omita** este bloco —
> ADC resolve via metadata server.

---

## Por que `*SecretRef` e não `*Ref`?

A primeira API usava `*Ref` (`credentialsRef`, `webhookRef`, etc.). Foi
renomeada porque:
- `*Ref` pode confundir com qualquer `*ObjectReference` do K8s
- `*SecretRef` deixa **explícito** que é um `Secret`, alinhando com
  cert-manager / postgres-operator / Zalando convention

---

## O que **NÃO** é Secret (intencional)

Estes ficam plain string mesmo sendo identificadores — não são segredos
no modelo de ameaça padrão K8s:

- `database.host`, `database.name`, `database.port`, `database.type`
- `storage.s3.bucket`, `storage.s3.region`, `storage.s3.prefix`,
  `storage.s3.endpointURL`, `storage.s3.storageClass`,
  `storage.s3.roleARN`
- `storage.azure.account`, `storage.azure.container`,
  `storage.azure.prefix`, `storage.azure.endpoint`
- `storage.gcs.bucket`, `storage.gcs.prefix`, `storage.gcs.projectID`
- `notifications.slack.channel`, `notifications.slack.username`
- `notifications.discord.username`
- `notifications.notifySuccess`, `notifications.stdout`

> **Compliance estrita** (PCI/HIPAA): se hostname e bucket name forem
> considerados PII na sua organização, mova-os pra Secret manualmente
> via patches do CronJob gerado, ou abra issue pedindo `*From` opcional
> em todos os fields.

---

## Back

- [Operator overview](./README.md)
- [BackupSchedule reference](./backupschedule.md)
- [Restore reference](./restore.md)

# GCS native backend

Google Cloud Storage com auth nativa via **Application Default Credentials**.
Use este backend quando você está em **GKE com Workload Identity** ou em
qualquer infra onde o ADC resolve sozinho (gcloud, GCE metadata server).

`STORAGE_BACKEND=gcs`.

> Se você está fora de GCP ou prefere static keys, GCS também funciona via
> [S3-compat (HMAC)](./s3.md#google-cloud-storage-hmac) — mas você gerencia
> a rotação manualmente.

---

## Env vars

| Var | Default | Notas |
|---|---|---|
| `GCS_BUCKET` | — | required |
| `GCS_PREFIX` | falls back to `S3_PREFIX` | key prefix |
| `GCS_PROJECT_ID` | — | obrigatório só se a credencial não embute o project |
| `GCS_CREDENTIALS_FILE` | — | caminho pra Service-Account JSON (só pra dev local) |
| `GCS_ENDPOINT` | — | override pra fake-gcs-server em testes |

**Auth resolution order** (Application Default Credentials):
1. `GCS_CREDENTIALS_FILE` (se setado)
2. `GOOGLE_APPLICATION_CREDENTIALS` env (apontando pra SA JSON)
3. `gcloud auth application-default login` (dev local)
4. **GKE Workload Identity** (token injetado pelo K8s controller)
5. GCE metadata server (Compute Engine, Cloud Run, Cloud Functions)

---

## GKE com Workload Identity (recomendado)

### Setup one-time

```sh
# 1. Service Account no GCP
gcloud iam service-accounts create dumpscript-backup

# 2. Grant Object Admin no bucket
gcloud storage buckets add-iam-policy-binding gs://prod-backups-gcs \
  --member=serviceAccount:dumpscript-backup@PROJECT.iam.gserviceaccount.com \
  --role=roles/storage.objectAdmin

# 3. Vincula KSA ↔ GSA via Workload Identity
gcloud iam service-accounts add-iam-policy-binding \
  dumpscript-backup@PROJECT.iam.gserviceaccount.com \
  --role=roles/iam.workloadIdentityUser \
  --member='serviceAccount:PROJECT.svc.id.goog[backups/dumpscript-pg]'

# 4. KSA no cluster com a anotação
kubectl create serviceaccount dumpscript-pg -n backups
kubectl annotate sa dumpscript-pg -n backups \
  iam.gke.io/gcp-service-account=dumpscript-backup@PROJECT.iam.gserviceaccount.com
```

### CronJob env

```yaml
serviceAccountName: dumpscript-pg
env:
  - { name: STORAGE_BACKEND,  value: "gcs" }
  - { name: GCS_BUCKET,       value: "prod-backups-gcs" }
  - { name: GCS_PREFIX,       value: "pg" }
  - { name: GCS_PROJECT_ID,   value: "my-prod-project" }
  # zero credenciais — metadata server resolve via Workload Identity
```

---

## Service Account JSON file (dev local ou clusters não-GKE)

```sh
# Cria a key (use sparingly — keys long-lived são fricção de compliance)
gcloud iam service-accounts keys create /tmp/sa.json \
  --iam-account=dumpscript-backup@PROJECT.iam.gserviceaccount.com

podman run --rm \
  -v /tmp/sa.json:/secrets/sa.json:ro \
  -e STORAGE_BACKEND=gcs \
  -e GCS_BUCKET=my-bucket \
  -e GCS_CREDENTIALS_FILE=/secrets/sa.json \
  -e DB_TYPE=postgresql ...  \
  localhost/dumpscript:go-alpine dump
```

No K8s, o operator monta o Secret JSON como volume read-only em
`/var/run/gcs/key.json` automaticamente quando você setar
`storage.gcs.credentialsSecretRef`. Veja
[operator example](../../examples/operator/postgres-gcs-workload-identity.yaml).

---

## fake-gcs-server (testes locais)

```sh
podman run -d --name fake-gcs -p 4443:4443 \
  fsouza/fake-gcs-server -scheme http -port 4443

# Pre-create the bucket
curl -X POST 'http://localhost:4443/storage/v1/b' \
  -H 'Content-Type: application/json' \
  -d '{"name": "test-bucket"}'

podman run --rm --network=host \
  -e STORAGE_BACKEND=gcs \
  -e GCS_BUCKET=test-bucket \
  -e GCS_ENDPOINT=http://localhost:4443/storage/v1/ \
  ... localhost/dumpscript:go-alpine dump
```

`GCS_ENDPOINT` ativa `option.WithoutAuthentication` automaticamente — o
emulador aceita anônimo.

---

## Integrity check

GCS faz **CRC32C end-to-end** automático no upload (via SDK). O dumpscript
soma uma checagem de tamanho pós-upload (`Object.Attrs(ctx)`) pra detectar
truncagens raras. Veja [Verification](../features/verification.md).

---

## Comparação rápida — GCS nativo vs S3-compat HMAC

| | S3-compat HMAC | GCS nativo |
|---|---|---|
| `STORAGE_BACKEND` | `s3` | `gcs` |
| Auth | `AWS_ACCESS_KEY_ID/SECRET` (HMAC) | ADC / Workload Identity |
| Secret no K8s | sim | **não** (com WI) |
| Rotação | manual via Console | auto (token 1h) |
| Limite | 5 HMAC keys / SA | sem limite |
| Cliente Go | `aws-sdk-go-v2` | `cloud.google.com/go/storage` |
| GKE-friendly | OK | nativo |
| Multi-cloud | ótimo (mesma SDK que AWS) | só GCP |

---

## Back

- [Storage overview](./README.md)
- [S3 backend](./s3.md)
- [Azure backend](./azure.md)
- [Operator GCS sample](../../examples/operator/postgres-gcs-workload-identity.yaml)

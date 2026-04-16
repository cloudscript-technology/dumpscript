# Tests

Testes de integração do dumpscript contra os providers de storage reais.

## Estrutura

```
tests/
├── gcs/      # Google Cloud Storage via S3-compatible API (HMAC)
├── aws/      # AWS S3
└── azure/    # Azure Blob Storage
```

## Como usar

Os comandos são executados a partir da **raiz do repositório**.

### 1. Copie o .env.example para .env e preencha as credenciais

```bash
cp tests/gcs/.env.example tests/gcs/.env
cp tests/aws/.env.example tests/aws/.env
cp tests/azure/.env.example tests/azure/.env
```

### 2. Execute o teste do provider desejado

```bash
npm run test:gcs
npm run test:aws
npm run test:azure
```

### 3. Limpe os containers após o teste (opcional)

```bash
npm run test:gcs:down
npm run test:aws:down
npm run test:azure:down
```

---

## GCS

Usa a [API S3-compatível do GCS com HMAC keys](https://cloud.google.com/storage/docs/interoperability).

**Pré-requisitos:**
- Habilite a interoperabilidade em: GCP Console → Storage → Settings → Interoperability
- Crie um HMAC key para a Service Account desejada
- O bucket deve existir previamente

> `S3_STORAGE_CLASS` **não deve ser definido** para GCS. O GCS rejeita classes da AWS (`STANDARD_IA`, etc.) via S3-compat API.

---

## AWS S3

**Pré-requisitos:**
- IAM User com permissões: `s3:PutObject`, `s3:GetObject`, `s3:ListBucket`, `s3:DeleteObject`
- O bucket deve existir previamente

Para temporary credentials (STS), defina também `AWS_SESSION_TOKEN` no `.env`.

---

## Azure Blob Storage

**Pré-requisitos:**
- Storage Account criada no Azure
- Container criado previamente
- Storage Account Key **ou** SAS Token com permissões `Read`, `Write`, `List`, `Delete`

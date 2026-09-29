# Tests

Testes de integração do dumpscript contra os providers de storage reais.

## Estrutura

```
tests/
├── gcs/      # Google Cloud Storage via S3-compatible API (HMAC)
├── aws/        # AWS S3
├── azure/      # Azure Blob Storage
└── clickhouse/ # ClickHouse (backup nativo server-side) -> S3 local (RustFS), roda offline
```

## Como usar

Os comandos são executados a partir da **raiz do repositório**.

### 1. Copie o .env.example para .env e preencha as credenciais

```bash
cp tests/gcs/.env.example tests/gcs/.env
cp tests/aws/.env.example tests/aws/.env
cp tests/azure/.env.example tests/azure/.env
cp tests/clickhouse/.env.example tests/clickhouse/.env   # não precisa de credencial
```

### 2. Execute o teste do provider desejado

```bash
npm run test:gcs
npm run test:aws
npm run test:azure
npm run test:clickhouse
```

### 3. Limpe os containers após o teste (opcional)

```bash
npm run test:gcs:down
npm run test:aws:down
npm run test:azure:down
npm run test:clickhouse:down
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

---

## ClickHouse

Sobe `clickhouse/clickhouse-server` (tag em `CLICKHOUSE_VERSION`, padrão 26.8.10.6) com dados de exemplo
(`initdb/01-seed.sql`) e o usuário `backup` com os grants de produção (`initdb/02-backup-user.sh`),
mais um S3 local (RustFS — as imagens do MinIO deixaram de ser públicas). O dumpscript dispara
`BACKUP ... TO S3(...)` no servidor e faz o poll em `system.backups`. Nenhuma credencial real é necessária.

**Cenários** (edite `tests/clickhouse/.env`):
- `DB_NAME=demo` — um banco.
- `DB_NAME=` — instância inteira (bancos + users/roles/grants/named collections).

**Contra o GCS real**: descomente o bloco no fim do `.env` (endpoint `https://storage.googleapis.com`,
HMAC key, bucket existente). O upload é feito pelo servidor ClickHouse, então este é o caminho de produção.

**Validar o restore** (com o stack de pé, `docker compose ... up -d s3 clickhouse`):

```bash
docker compose -f tests/clickhouse/docker-compose.yml --env-file tests/clickhouse/.env exec clickhouse \
  clickhouse-client --user admin --password admin --query \
  "RESTORE DATABASE demo AS demo_restored FROM S3('http://s3:9000/dumpscript/clickhouse/demo/daily/<yyyy>/<mm>/<dd>/<arquivo>.tar.zst', 'minioadmin', 'minioadmin123')"
```

Compare `SELECT count(), sum(event_id) FROM demo.events` com `demo_restored.events`.

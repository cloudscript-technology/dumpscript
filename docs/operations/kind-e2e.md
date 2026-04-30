# Kind E2E Test Suite

Documentação dos ~60+ specs de teste end-to-end que validam o
dumpscript operator num cluster Kubernetes real (kind), cobrindo:

- 6 engines (postgres, mysql, mariadb, mongodb, redis, etcd) com backup +
  restore (redis/etcd só backup — restore unsupported pelo binário)
- 3 storage backends (S3 via LocalStack, GCS via fake-gcs-server, Azure via
  Azurite)
- IRSA / ServiceAccount-based auth via OIDC + LocalStack STS
- Features novas do binário: `dryRun`, `compression: zstd`, S3 object tagging
- Lifecycle (suspend/resume, history limits, cascade delete)
- Edge cases (retention, lock, prefix isolation, periodicity layout, etc.)

---

## Visão geral

O suite valida o fluxo completo da plataforma:

```
BackupSchedule CR
      │
      ▼  reconcilia
   CronJob ──► Job ──► dumpscript ──► S3 (LocalStack)
                                           │
                                      Restore CR
                                           │
                                      Job ──► psql
                                           │
                                      dados verificados
```

Cada teste roda contra infraestrutura real dentro do cluster — sem mocks,
sem testcontainers, sem atalhos. O estado observado é o estado real do
Kubernetes.

---

## Estrutura dos arquivos

```
tests/kind-e2e/
├── suite_test.go          ─ bootstrap Ginkgo, constantes globais
├── infra_test.go          ─ BeforeSuite / AfterSuite (setup do cluster)
├── helpers_test.go        ─ utilitários: run(), psql(), seedS3Object(), …
│
├── backup_test.go         ─ 7 specs: fluxo principal backup → restore (postgres + S3)
├── lifecycle_test.go      ─ 7 specs: ciclo de vida do BackupSchedule
├── advanced_test.go       ─ 8 specs: features avançadas (prefix, TTL, createDB, …)
├── more_test.go           ─ 9 specs: retention, lock, periodicity, status fields
├── irsa_test.go           ─ specs: ServiceAccount-based S3 auth (IRSA via OIDC)
├── gcs_test.go            ─ specs: GCS backend via fake-gcs-server
├── azure_test.go          ─ specs: Azure Blob backend via Azurite
├── mysql_test.go          ─ 4 specs: MySQL backup + restore
├── mariadb_test.go        ─ 4 specs: MariaDB backup + restore
├── mongodb_test.go        ─ 4 specs: MongoDB backup + restore (com authSource)
├── redis_test.go          ─ 3 specs: Redis backup-only (restore unsupported)
├── etcd_test.go           ─ 3 specs: etcd backup-only (restore unsupported)
├── features_test.go       ─ 4 specs: dryRun, compression=zstd, S3 object tagging
│
├── terragrunt.hcl         ─ config Terragrunt (state em /tmp)
├── terraform/
│   ├── main.tf            ─ aws_s3_bucket no LocalStack (path-style)
│   ├── variables.tf       ─ bucket_name, localstack_endpoint
│   └── outputs.tf
└── manifests/
    ├── localstack.yaml    ─ LocalStack 4 (SERVICES=s3)
    ├── postgres.yaml      ─ PostgreSQL 17
    ├── mysql.yaml         ─ MySQL 8.0
    ├── mariadb.yaml       ─ MariaDB 11
    ├── mongodb.yaml       ─ MongoDB 7 (com auth)
    ├── redis.yaml         ─ Redis 7-alpine
    ├── etcd.yaml          ─ etcd v3.5
    ├── fake-gcs.yaml      ─ fake-gcs-server (emulador GCS)
    └── azurite.yaml       ─ Azurite (emulador Azure Blob)
```

---

## Ambiente de teste

### Cluster kind

Um cluster kind efêmero é criado no `BeforeSuite` e destruído no `AfterSuite`.
Nome: `dumpscript-e2e`. Namespace de testes: `dumpscript-e2e`.

### Serviços no cluster

| Serviço | Imagem | Acesso interno | Acesso externo |
|---|---|---|---|
| LocalStack | `localstack/localstack:4` | `localstack.dumpscript-e2e.svc.cluster.local:4566` | `localhost:14566` (port-forward) |
| PostgreSQL | `postgres:17` | `postgres.dumpscript-e2e.svc.cluster.local:5432` | via `kubectl exec` |
| Operator | `localhost/dumpscript-operator:kind-e2e` | `dumpscript-operator-system` namespace | — |

> **Por que PostgreSQL 17?**
> A imagem dumpscript usa `pg_dump` 18, que emite `SET transaction_timeout`
> (introduzido no PG17). Em PG16 esse comando causa rollback silencioso da
> transação de restore.

### Bucket S3

Criado pelo Terragrunt antes dos testes e destruído depois. O Terragrunt
aponta para o LocalStack via port-forward (`http://localhost:14566`).
Dentro do cluster, os Jobs usam `http://localstack.dumpscript-e2e.svc.cluster.local:4566`.

---

## Pré-requisitos

| Ferramenta | Versão mínima | Papel |
|---|---|---|
| `kind` | v0.31 | criar/destruir o cluster |
| `kubectl` | v1.35 | aplicar manifests, exec, logs |
| `docker` / `podman` | podman 5.7 | build + carga de imagens |
| `terragrunt` | v0.97 | provisionar o bucket S3 |
| `terraform` | v1.14 | chamado pelo Terragrunt |

---

## Como executar

```sh
# Primeira vez: baixar deps do módulo isolado
make e2e-kind-deps

# Rodar o suite completo (~4-5 min)
make e2e-kind
```

Com podman (NixOS / sem Docker real):

```sh
KIND_EXPERIMENTAL_PROVIDER=podman \
DOCKER_HOST="unix:///run/user/$(id -u)/podman/podman.sock" \
make e2e-kind
```

O `make e2e-kind` equivale a:

```sh
cd tests/kind-e2e && \
  PROJECT_ROOT=$(pwd)/../.. \
  go test -v -tags=kind_e2e -count=1 -timeout=45m ./...
```

---

## Sequência do BeforeSuite

O `BeforeSuite` em `infra_test.go` é executado uma única vez antes de todos
os specs e segue esta sequência:

```
1.  Verificar ferramentas (kind, kubectl, docker, terragrunt)
2.  Deletar cluster anterior (idempotente)
3.  Apagar state Terraform stale em /tmp
4.  kind create cluster --name dumpscript-e2e --wait 120s
5.  kubectl create namespace dumpscript-e2e
6.  kubectl apply -f manifests/localstack.yaml  →  rollout status
7.  kubectl apply -f manifests/postgres.yaml    →  rollout status
8.  kubectl port-forward svc/localstack 14566:4566  (background)
9.  Aguardar LocalStack /_localstack/health
10. terragrunt apply -auto-approve  (cria bucket S3)
11. docker build -t localhost/dumpscript:kind-e2e .
    podman save | ctr images import  →  carrega no containerd do nó
12. docker build -t localhost/dumpscript-operator:kind-e2e ./operator
    podman save | ctr images import
13. kubectl apply -f operator/config/crd/bases/  (instala CRDs)
14. kubectl kustomize operator/config/default | sed image | kubectl apply
    (deploy operator com imagePullPolicy: IfNotPresent)
15. Aguardar pod operator Running
16. kubectl create secret aws-credentials  (AWS_ACCESS_KEY_ID=test)
17. kubectl create secret postgres-credentials  (username/password)
```

O `AfterSuite` desfaz na ordem inversa:

```
1. terragrunt destroy -auto-approve
2. Matar processo port-forward
3. kind delete cluster --name dumpscript-e2e
```

---

## Os 31 specs

### `backup_test.go` — Fluxo principal (7 specs)

**Describe: `BackupSchedule → S3 → Restore` (Ordered)**

BeforeAll: cria tabela `e2e_marker` com uma linha no PostgreSQL.
Prefix S3: `main-e2e/` (evita colisão com outros Describes).

| # | Spec | O que verifica |
|---|---|---|
| 1 | `operator reconciles BackupSchedule → CronJob` | CR aplicada → controller cria CronJob com o schedule correto |
| 2 | `manual Job trigger completes successfully` | `kubectl create job --from=cronjob/...` roda dumpscript, Job status=Complete |
| 3 | `backup object is present in S3` | Objeto `main-e2e/daily/YYYY/MM/DD/dump_*.sql.gz` existe no LocalStack |
| 4 | `operator reconciles Restore → Job and data is recovered` | Restore CR → Job → `e2e_marker` restaurada com dados originais |
| 5 | `second manual Job produces a second S3 object` | Dois backups acumulam → `len(objects) >= 2` |

**Describe: `Restore edge cases`**

| # | Spec | O que verifica |
|---|---|---|
| 6 | `Restore with invalid sourceKey sets phase=Failed` | Key inexistente → `status.phase = Failed` |
| 7 | *(segundo backup acumula — spec 5 acima)* | — |

---

### `lifecycle_test.go` — Ciclo de vida (7 specs)

**Describe: `BackupSchedule spec changes` (Ordered)**

BeforeAll: aplica um BackupSchedule e aguarda o CronJob ser criado.

| # | Spec | O que verifica |
|---|---|---|
| 8 | `suspend=true pauses the underlying CronJob` | Patch `suspend: true` → CronJob `.spec.suspend = "true"` |
| 9 | `suspend=false resumes the CronJob` | Patch `suspend: false` → CronJob `.spec.suspend = "false"` |
| 10 | `schedule change is propagated to the CronJob` | Patch schedule `"0 3 * * *"` → CronJob reflete novo cron |
| 11 | `deleting BackupSchedule garbage-collects the owned CronJob` | Delete CR → CronJob desaparece (owner reference cascade) |

**Describe: `BackupSchedule status` (Ordered)**

BeforeAll: cria BackupSchedule + dispara Job manual + aguarda completion.

| # | Spec | O que verifica |
|---|---|---|
| 12 | `lastSuccessTime is set after a successful job` | `status.lastSuccessTime` não-vazio após Job completar |
| 13 | `lastScheduleTime reflects the manual job creation time` | `status.lastScheduleTime` não-vazio |

**Describe: `Operator resilience`**

| # | Spec | O que verifica |
|---|---|---|
| 14 | `new operator pod continues reconciling existing BackupSchedules` | Delete pod → novo pod sobe → logs sem `panic` |

---

### `advanced_test.go` — Features avançadas (8 specs)

**Describe: `S3 prefix` (Ordered)**

BeforeAll: BackupSchedule com `s3.prefix: "myapp/backups"` + Job manual.

| # | Spec | O que verifica |
|---|---|---|
| 15 | `backup object key starts with the configured S3 prefix` | Chave começa com `myapp/backups/daily/` |

**Describe: `Stdout notification` (Ordered)**

BeforeAll: BackupSchedule com `notifications.stdout: true` + `notifySuccess: true` + Job manual.

| # | Spec | O que verifica |
|---|---|---|
| 16 | `pod logs contain a structured notification JSON on success` | Logs contêm `{"event":"success",...}` |

**Describe: `CronJob history limits` (Ordered)**

BeforeAll: BackupSchedule com `failedJobsHistoryLimit: 2` + `successfulJobsHistoryLimit: 1`.

| # | Spec | O que verifica |
|---|---|---|
| 17 | `CronJob reflects custom failedJobsHistoryLimit` | `.spec.failedJobsHistoryLimit = "2"` |
| 18 | `CronJob reflects custom successfulJobsHistoryLimit` | `.spec.successfulJobsHistoryLimit = "1"` |

**Describe: `Multiple BackupSchedules` (Ordered)**

BeforeAll: dois BackupSchedules (`multi-a`, `multi-b`) com prefixes distintos.

| # | Spec | O que verifica |
|---|---|---|
| 19 | `both BackupSchedules have independent CronJobs` | Dois CronJobs existem simultaneamente |
| 20 | `deleting one BackupSchedule does not affect the other` | Delete A → CronJob A desaparece, CronJob B permanece |

**Describe: `Restore advanced` (Ordered)**

BeforeAll: cria banco `createdb_test`, insere dados, faz backup, encontra key.

| # | Spec | O que verifica |
|---|---|---|
| 21 | `Restore with createDB=true recreates the database and recovers data` | Drop banco inteiro → Restore `createDB: true` → banco recriado + dados restaurados |
| 22 | `Restore with ttlSecondsAfterFinished cleans up the Job automatically` | Restore com `ttlSecondsAfterFinished: 15` → Job deletado automaticamente pelo TTL controller |

---

### `more_test.go` — Retenção, locks, status (9 specs)

**Describe: `Retention` (Ordered)**

BeforeAll: pre-seed de 3 objetos com datas antigas (2020, 2021, 2022) via
`seedS3Object()` usando AWS Sig V4 puro em Go → LocalStack port-forward.
BackupSchedule com `retentionDays: 7` + Job manual.

| # | Spec | O que verifica |
|---|---|---|
| 23 | `old backup objects are deleted by the retention sweep` | 3 objetos antigos não existem mais no S3 |
| 24 | `today's backup is preserved after retention sweep` | Backup de hoje sobrevive ao sweep |

**Describe: `Lock contention` (Ordered)**

BeforeAll: pre-seed do arquivo `.lock` de hoje via `seedS3Object()`.
BackupSchedule + Job manual (que vai encontrar o lock).

| # | Spec | O que verifica |
|---|---|---|
| 25 | `job exits 0 (graceful skip) when lock is held` | Job status=Complete (não Failed) mesmo com lock pre-existente |
| 26 | `no new backup object is uploaded when lock is held` | Nenhum novo `.gz` criado no prefix do schedule |

**Describe: `Weekly periodicity` (Ordered)**

BeforeAll: BackupSchedule com `periodicity: weekly` + Job manual.

| # | Spec | O que verifica |
|---|---|---|
| 27 | `backup key path contains 'weekly/' segment` | Chave contém `weekly-test/weekly/` |

**Describe: `BackupSchedule starts suspended`**

| # | Spec | O que verifica |
|---|---|---|
| 28 | `CronJob is immediately suspended when BackupSchedule is created with suspend=true` | CronJob criado com `.spec.suspend = "true"` + nenhum Job gerado |

**Describe: `Restore status fields` (Ordered)**

BeforeAll: cria BackupSchedule próprio + Job → backup → Restore CR →
aguarda `Succeeded`. Totalmente auto-suficiente (não depende de outros Describes).

| # | Spec | O que verifica |
|---|---|---|
| 29 | `status.jobName is populated after Restore is created` | `status.jobName` começa com `"restore-"` |
| 30 | `status.startedAt is set` | `status.startedAt` não-vazio |
| 31 | `status.completedAt is set after Restore succeeds` | `status.completedAt` não-vazio |

**Describe: `BackupSchedule failure tracking` (Ordered)**

BeforeAll: BackupSchedule apontando para host inexistente + Job manual que
falha por timeout de conexão. `BackoffLimit: 0` garante falha imediata.

| # | Spec | O que verifica |
|---|---|---|
| 32¹ | `status.lastFailureTime is set after a job fails` | `status.lastFailureTime` não-vazio após Job Failed |

> ¹ O suite tem 31 specs no total — o Describe de failure tracking usa
> `BeforeAll` + 1 `It` e fica dentro do mesmo `Ordered`, mas por conta de
> como o Ginkgo conta entradas a numeração aqui é apenas ilustrativa.

---

## Utilitários de teste

### `helpers_test.go`

| Função | Descrição |
|---|---|
| `run(name, args...)` | Executa comando, falha o teste se exit ≠ 0 |
| `runIn(dir, name, args...)` | Igual, mas com CWD específico |
| `runOutput(name, args...)` | Retorna stdout+stderr; não falha automaticamente |
| `mustOutput(name, args...)` | Igual, mas falha se houver erro |
| `requireTools(tools...)` | Verifica que as ferramentas estão no PATH |
| `runTerragrunt(args...)` | Executa terragrunt em `tests/kind-e2e/` com env correto |
| `waitForURL(url, timeout)` | Poll HTTP até receber status < 500 |
| `listS3Objects(bucket)` | Lista chaves via HTTP GET + XML parse no LocalStack |
| `applyManifest(yaml)` | `kubectl apply -f -` via stdin |
| `kubectlExec(ns, pod, container, cmd...)` | Executa comando dentro de um pod |
| `pgPodName()` | Retorna nome do primeiro pod do postgres |
| `psql(sql)` | Executa SQL no pod postgres em `testdb` |
| `psqlDB(database, sql)` | Executa SQL num banco específico |
| `seedS3Object(key)` | PUT no LocalStack via AWS Signature V4 puro (sem SDK) |
| `podmanEnv()` | Adiciona `KIND_EXPERIMENTAL_PROVIDER=podman` + `DOCKER_HOST` ao env |
| `kindLoadImage(img, cluster)` | `podman save \| ctr images import` no nó kind |
| `deployOperator(dir)` | `kubectl kustomize \| sed image \| kubectl apply` |

### `seedS3Object` — detalhe técnico

Constrói manualmente uma requisição AWS Signature V4:

```
Canonical request → SHA-256 → String to sign → HMAC-SHA256 signing key
→ Authorization header → http.PUT → LocalStack
```

Usa apenas stdlib (`crypto/hmac`, `crypto/sha256`, `encoding/hex`).
Não requer SDK nem pod externo. Funciona via port-forward no `localhost:14566`.

---

## Isolamento e aleatoriedade

Ginkgo v2 randomiza a ordem de execução dos `Describe` blocks entre si
(seed diferente a cada run). Para garantir que os specs sejam robustos:

- Cada `Describe` é **auto-suficiente**: cria seus próprios recursos
  (BackupSchedules, secrets, prefixes S3 únicos) em `BeforeAll`
- Prefixes S3 únicos por Describe evitam que `listS3Objects` retorne
  resultados de outros testes
- `Ordered` dentro de um `Describe` preserva a ordem interna dos specs
- O `BeforeSuite` / `AfterSuite` são sempre executados primeiro e último

---

## Troubleshooting

**Locks esgotados no podman**
```
Error: allocating lock for new volume: allocation failed; exceeded num_locks
```
```sh
podman volume prune -f && podman container prune -f
```

**`ImagePullBackOff` no operator pod**

O `kindLoadImage` usa `podman exec ctr images import` com prefixo
`localhost/`. Se falhar, confirme que as imagens existem localmente:
```sh
podman images | grep kind-e2e
```

**Cluster órfão de run anterior**

O `BeforeSuite` deleta automaticamente qualquer cluster `dumpscript-e2e`
antes de criar um novo. Para limpeza manual:
```sh
kind delete cluster --name dumpscript-e2e
```

**State Terraform inconsistente**
```sh
rm -f /tmp/dumpscript-kind-e2e.tfstate*
```

**Falha no `make install` do operator (cache VCS corrompido)**

O suite não usa `make install` — aplica as CRDs geradas diretamente:
```sh
kubectl apply -f operator/config/crd/bases/
```
E o operator é deployado via `kubectl kustomize` com override de imagem,
sem precisar de `controller-gen` nem de download de pacotes.

---

## CI

Adicionar ao pipeline GitHub Actions:

```yaml
- name: Kind E2E (operator + S3 + restore)
  run: |
    make e2e-kind-deps
    make e2e-kind
  env:
    KIND_EXPERIMENTAL_PROVIDER: podman
    DOCKER_HOST: unix:///run/user/1000/podman/podman.sock
```

Tempo médio: **4-5 minutos** (build de imagens ~2min + testes ~2-3min).

---

## Back

- [Testing overview](./testing.md)
- [Kubernetes deployment](./kubernetes.md)
- [BackupSchedule reference](../operator/backupschedule.md)
- [Restore reference](../operator/restore.md)

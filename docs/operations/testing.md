# Testing

Duas camadas de testes:

1. **Unit tests** — Go puro, rápidos, sem dependência externa
2. **End-to-end (e2e)** — testcontainers-go, sobe DBs reais + storage real

Ambos via `make`.

---

## Unit tests

```sh
make test          # roda ./internal/... ./cmd/...
make test-race     # com race detector (recomendado em CI)
make cover         # coverage summary por pacote
make cover-html    # gera coverage.html navegável
```

Cobertura atual aproximada (depende do crescimento da base):

| Pacote | Cobertura |
|---|---|
| `internal/clock` | 100% |
| `internal/retention` | 100% |
| `internal/verifier` | ~97% |
| `internal/restorer` | ~95% |
| `internal/config` | ~94% |
| `internal/notify` | ~91% |
| `internal/pipeline` | ~91% |
| `internal/dumper` | ~90% |
| `internal/lock` | ~89% |
| `internal/storage` | ~75% (httptest mocks dos backends + integrity_test.go) |
| `internal/awsauth` | ~31% (gaps no IRSA flow — TODO) |
| `internal/cli` | ~21% (gaps no Cobra wiring — TODO) |

Suite completa passa com `-race -count=1` em ~30s.

---

## E2E tests

E2E usa [testcontainers-go](https://golang.testcontainers.org/) — sobe
containers reais do DB + MinIO/Azurite + roda a imagem `dumpscript`
contra eles.

### Setup

Requer Docker ou Podman ativo. Em macOS+Podman:

```sh
podman machine start
make e2e          # auto-detecta DOCKER_HOST via podman machine inspect
```

Linux/Docker é zero-config (`make e2e` direto).

### Targets

| Target | Cobre |
|---|---|
| `make e2e` | Build da imagem + suite completa |
| `make e2e-quick` | Suite completa **sem** rebuildar a imagem |
| `make e2e-postgres` | Apenas a matriz Postgres (13 → 18) |
| `make e2e-engines` | Todo engine exceto MySQL 5.7 (lento via amd64 emulation) |
| `make e2e-features` | Azure / Lock / Retention / Slack |
| `make e2e-one NAME=TestMongo` | Um único teste por nome |

### Cobertura E2E

| Teste | Backends reais subidos |
|---|---|
| `TestPostgres/{13..18}` | PG 13, 14, 15, 16, 17, 18 + MinIO |
| `TestPostgresCluster` | PG 16 (multi-DB) + MinIO |
| `TestMariaDB` | MariaDB 11.4 + MinIO |
| `TestMySQL57` / `TestMySQL80` | MySQL 5.7, 8.0 + MinIO |
| `TestMongo` | Mongo 7 + MinIO |
| `TestCockroach` | CockroachDB v24.2.4 + MinIO |
| `TestRedis` | Redis 7-alpine + MinIO |
| `TestSQLite` | volume + MinIO |
| `TestEtcd` | etcd v3.5.13 + MinIO |
| `TestElasticsearch` | ES 8.13.0 + MinIO |
| `TestAzure` | Azurite + PG + Azure CLI helper |
| `TestLockContention` | PG + MinIO com lock pré-seeded |
| `TestRetention` | MinIO com objetos antigos seeded |
| `TestSlackNotification` | Python httpd como webhook fake |

Cada engine passa por **dump + restore roundtrip real** (ou dump-only
quando restore é unsupported) e valida que os dados voltam íntegros.

---

## Cleanup de containers

Os testcontainers ficam órfãos se o teste é interrompido (`Ctrl+C`).
Ryuk reaper resolve isso, mas em alguns setups Podman ele falha — daí
`TESTCONTAINERS_RYUK_DISABLED=true` no `make e2e`.

Pra limpar manualmente:

```sh
podman rm -f $(podman ps -aq --filter label=org.testcontainers=true) 2>/dev/null
podman volume prune -f
```

Em macOS, se o disco do podman machine encher:

```sh
podman machine rm -f
podman machine init
podman machine start
```

---

## CI

Setup recomendado pra CI (GitHub Actions exemplo):

```yaml
- name: Unit tests
  run: make test-race

- name: E2E (engines, sem MySQL 5.7 que é lento amd64)
  run: make e2e-engines

- name: Cover report
  run: make cover-html
- uses: actions/upload-artifact@v4
  with: { name: coverage, path: coverage.html }
```

Tempo médio de CI:
- `make test-race`: ~30s
- `make e2e-engines`: ~5min
- `make e2e` completo: ~10min

---

## Operator tests

Em `/operator`:

```sh
cd operator
go test ./api/... ./internal/...    # unit (precisa setup-envtest pro suite_test)
make test                           # roda envtest binaries automaticamente
make test-e2e                       # requer cluster Kind ativo
```

Os tests do controller requerem o `setup-envtest` baixar binários
locais do K8s — `make` os instala automaticamente em `operator/bin/`.

---

## Kind E2E (fluxo completo de operador)

Testa o fluxo de ponta a ponta num cluster Kubernetes real:

```
kind (cluster local) → operator → BackupSchedule CR → CronJob
  → dumpscript Job → upload S3 (LocalStack via Terragrunt)
  → Restore CR → Job → PostgreSQL restaurado → dados verificados
```

### Pré-requisitos

| Ferramenta | Versão testada | Instalação |
|---|---|---|
| `kind` | v0.31+ | `nix profile add nixpkgs#kind` |
| `kubectl` | v1.35+ | `nix profile add nixpkgs#kubectl` |
| `docker` / `podman` | podman 5.7+ | geralmente pré-instalado |
| `terragrunt` | v0.97+ | `nix profile add nixpkgs#terragrunt` |
| `terraform` | v1.14+ | `nix profile add nixpkgs#terraform` |

### Executar

```sh
# Primeira vez: baixar dependências Go do módulo isolado
make e2e-kind-deps

# Rodar o suite completo (~3-5 min)
make e2e-kind
```

Com podman (sem Docker real):

```sh
KIND_EXPERIMENTAL_PROVIDER=podman \
DOCKER_HOST="unix:///run/user/$(id -u)/podman/podman.sock" \
make e2e-kind
```

> O `Makefile` detecta automaticamente se `docker` é um alias para `podman`
> e injeta as variáveis corretas via `podmanEnv()` no código Go.

### O que é testado

| Spec | Verifica |
|---|---|
| `operator reconciles BackupSchedule → CronJob` | CR aplicada → controller cria CronJob com spec correta |
| `manual Job trigger completes successfully` | `kubectl create job --from=cronjob/...` roda dumpscript até completion |
| `backup object is present in S3` | Objeto `daily/YYYY/MM/DD/dump_*.sql.gz` existe no LocalStack |
| `operator reconciles Restore → Job and data is recovered` | Restore CR → Job → tabela restaurada no PostgreSQL |

### Arquitetura do ambiente

```
┌─────────────────────── kind cluster ───────────────────────┐
│                                                             │
│  dumpscript-e2e namespace                                   │
│  ┌──────────┐   ┌──────────────────────────────────┐       │
│  │ postgres │   │ localstack (S3 :4566)            │       │
│  └──────────┘   └──────────────────────────────────┘       │
│        ▲                  ▲                                 │
│        │     dumpscript   │ AWS_S3_ENDPOINT_URL             │
│        └────── Job ───────┘                                 │
│                                                             │
│  dumpscript-operator-system namespace                       │
│  ┌────────────────────────────────────┐                     │
│  │ operator (controller-manager pod) │                     │
│  └────────────────────────────────────┘                     │
└─────────────────────────────────────────────────────────────┘
         │ port-forward :14566
         ▼
  host: terragrunt apply → aws_s3_bucket no LocalStack
```

### Infraestrutura como código (Terragrunt)

O bucket S3 é provisionado pelo Terragrunt antes dos testes e destruído depois:

```
tests/kind-e2e/
├── terragrunt.hcl        ← estado em /tmp; source = ./terraform
└── terraform/
    ├── main.tf           ← provider aws → LocalStack (path-style)
    ├── variables.tf      ← bucket_name, localstack_endpoint
    └── outputs.tf
```

O endpoint do LocalStack é passado via `TF_VAR_localstack_endpoint` apontando
para o port-forward ativo (`http://localhost:14566`). Dentro do cluster, os
Jobs usam `http://localstack.dumpscript-e2e.svc.cluster.local:4566`.

### Troubleshooting

**Locks esgotados no podman**

```
Error: allocating lock for new volume: allocation failed; exceeded num_locks
```

```sh
podman volume prune -f
podman container prune -f
```

**Imagem não encontrada no nó kind (ImagePullBackOff)**

O carregamento usa `podman exec ctr images import` com prefixo `localhost/`.
Confirme que a imagem existe localmente:

```sh
podman images | grep kind-e2e
```

**Cluster órfão de execução anterior**

O `BeforeSuite` deleta automaticamente qualquer cluster `dumpscript-e2e`
existente antes de criar um novo. Para limpeza manual:

```sh
kind delete cluster --name dumpscript-e2e
```

**Estado Terraform inconsistente**

O `BeforeSuite` deleta `/tmp/dumpscript-kind-e2e.tfstate` antes de cada run.
Se o `AfterSuite` não rodou (kill abrupto), delete manualmente:

```sh
rm -f /tmp/dumpscript-kind-e2e.tfstate*
```

---

## CI

Setup recomendado pra CI (GitHub Actions exemplo):

```yaml
- name: Unit tests
  run: make test-race

- name: E2E (engines, sem MySQL 5.7 que é lento amd64)
  run: make e2e-engines

- name: Kind E2E (operador + S3 + restore)
  run: make e2e-kind

- name: Cover report
  run: make cover-html
- uses: actions/upload-artifact@v4
  with: { name: coverage, path: coverage.html }
```

Tempo médio de CI:
- `make test-race`: ~30s
- `make e2e-engines`: ~5min
- `make e2e` completo: ~10min
- `make e2e-kind`: ~3-5min

---

## Back

- [Docker image](./docker_image.md)
- [Kubernetes deployment](./kubernetes.md)
- [Adding an engine](../development/adding_an_engine.md)

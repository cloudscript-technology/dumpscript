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

## Back

- [Docker image](./docker_image.md)
- [Kubernetes deployment](./kubernetes.md)
- [Adding an engine](../development/adding_an_engine.md)

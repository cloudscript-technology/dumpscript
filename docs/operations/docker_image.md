# Docker / Podman image

A imagem do `dumpscript` é multi-stage Alpine, parametrizável, ~180 MiB
final. Cobre todos os engines core via clientes forward-compatible.

---

## Build

```sh
# Default: Alpine edge + PG 18 client (cobre PG server 9.2 → 18)
make image
# OU manualmente:
podman build -f docker/Dockerfile -t dumpscript:go-alpine .
```

```sh
# Variante mais conservadora: Alpine 3.22 + PG 17 client
make image-stable
# OU:
podman build -f docker/Dockerfile \
  --build-arg ALPINE_TAG=3.22 \
  --build-arg PG_CLIENT=postgresql17-client \
  -t dumpscript:stable .
```

---

## Build args

| Arg | Default | Notas |
|---|---|---|
| `ALPINE_TAG` | `edge` | `edge` traz o pg_dump 18; `3.22` ou `3.21` cap em pg_dump 17 |
| `PG_CLIENT` | `postgresql18-client` | Pareie com a `ALPINE_TAG` — em Alpine 3.22 só vai até pg17 |

> **Pin `alpine:edge` por digest em produção** pra evitar surpresas de
> rolling updates. Exemplo:
>
> ```dockerfile
> FROM alpine:edge@sha256:<digest>
> ```

---

## CLIs incluídas

A imagem default vem com:

| Tool | Apk package | Cobre |
|---|---|---|
| `pg_dump`, `psql` | `postgresql18-client` | PostgreSQL 9.2 → 18, CockroachDB |
| `mariadb-dump`, `mariadb` | `mariadb-client` | MySQL 5.7/8.0, MariaDB 10/11 |
| `mongodump`, `mongorestore` | `mongodb-tools` | MongoDB 4.0 → 7.0+ |
| `redis-cli` | `redis` | Redis 6, 7 |
| `sqlite3` | `sqlite` | SQLite 3.x (formato estável) |
| `etcdctl` | `etcd-ctl` | etcd v3.4, v3.5 |
| Go static binary | (built-in) | Elasticsearch (HTTP scroll, sem dep externa) |

---

## Engines que precisam de imagem custom

Estes não cabem na imagem Alpine default (deps proprietárias / JVM /
Python). Use a imagem default + estende:

| Engine | Por que precisa custom | Dica |
|---|---|---|
| **SQL Server** | `mssql-scripter` (Python) + `sqlcmd` — Microsoft só publica em Debian | Multi-stage com Debian base, copia o Go binary do dumpscript |
| **Oracle** | Oracle Instant Client (proprietário, ~1 GB) | Use `oraclelinux:8-slim` como base + microdnf install instantclient |
| **Neo4j** | `neo4j-admin` (JVM, ~400 MB) | `FROM neo4j:5-community` + copy do binary |
| **ClickHouse** | `clickhouse-client` (~500 MB binário) | Multi-stage, copia só o `clickhouse` static |

Cada engine tem um sketch de Dockerfile no doc respectivo:
- [docs/engines/sqlserver.md](../engines/sqlserver.md)
- [docs/engines/oracle.md](../engines/oracle.md)
- [docs/engines/neo4j.md](../engines/neo4j.md)
- [docs/engines/clickhouse.md](../engines/clickhouse.md)

---

## Tamanho

```
~180 MiB  default (edge + PG 18 + mariadb-dump 11.8 + mongo-tools + redis + sqlite + etcdctl + Go binary)
```

Breakdown aproximado:
- Alpine edge base: ~7 MiB
- `postgresql18-client` + libpq + zstd: ~50 MiB
- `mariadb-client` + connector-c: ~40 MiB
- `mongodb-tools`: ~60 MiB
- `redis`: ~5 MiB
- `sqlite`: ~2 MiB
- `etcd-ctl`: ~25 MiB
- Go binary (CGO_ENABLED=0, stripped): ~12 MiB

---

## Multi-arch

A imagem builda nativamente em `linux/amd64` e `linux/arm64` (Alpine
edge tem ambos). Pra cross-build:

```sh
podman build --platform=linux/amd64,linux/arm64 \
  -f docker/Dockerfile -t dumpscript:multi .
```

---

## Push pra registry

```sh
podman tag dumpscript:go-alpine ghcr.io/cloudscript-technology/dumpscript:0.x
podman push ghcr.io/cloudscript-technology/dumpscript:0.x
```

CronJobs em produção devem **pinar a tag** (`:0.x` ou `@sha256:...`),
não usar `:latest`.

---

## Layer caching

Pra builds rápidos durante dev:

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dumpscript ./cmd/dumpscript
```

Mudanças em `*.go` invalidam só a layer do build (não o `go mod download`),
desde que `go.mod`/`go.sum` não mudem.

---

## Back

- [Quick start](../quickstart.md)
- [Kubernetes deployment](./kubernetes.md)
- [Testing](./testing.md)

# syntax=docker/dockerfile:1.7
#
# Single-image build: one tag covers every supported database version.
#
# Rationale — the newest client is always >= older servers, so we ship just
# one client per engine and it dumps/restores every supported server version:
#   - pg_dump 18      → covers Postgres 9.2 → 18
#   - mariadb-dump 11 → covers MySQL 5.7/8.0 and MariaDB 10.x/11.x
#   - mongodb-tools   → covers MongoDB 4.0 → 7.0+
#
# Default uses alpine:edge for Postgres 18 (only edge carries postgresql18-client
# today). To prefer alpine stable at the cost of being stuck at PG 17, build with:
#   --build-arg ALPINE_TAG=3.22 --build-arg PG_CLIENT=postgresql17-client
#
# Pin `edge` by digest in production to avoid surprises from rolling updates.

ARG ALPINE_TAG=edge
ARG PG_CLIENT=postgresql18-client

# --- Build stage ---
FROM mirror.gcr.io/library/golang:1.25-alpine AS builder
WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/dumpscript ./cmd/dumpscript

# --- Runtime stage ---
FROM mirror.gcr.io/library/alpine:${ALPINE_TAG}

# Re-declare ARG inside stage so RUN can see it.
ARG PG_CLIENT

RUN apk add --no-cache \
        ca-certificates \
        tzdata \
        ${PG_CLIENT} \
        mariadb-client \
        mariadb-connector-c \
        mongodb-tools \
        redis \
        sqlite \
        etcd-ctl \
 && update-ca-certificates

COPY --from=builder /out/dumpscript /usr/local/bin/dumpscript

# Default workdir matches the binary's WORK_DIR default.
WORKDIR /dumpscript

# OCI metadata — discoverable by ops tooling (skopeo inspect, image scanners).
LABEL org.opencontainers.image.title="dumpscript" \
      org.opencontainers.image.description="Database dump and restore tool for S3, GCS, and Azure Blob" \
      org.opencontainers.image.source="https://github.com/cloudscript-technology/dumpscript" \
      org.opencontainers.image.licenses="Apache-2.0"

# Optional liveness check for daemon-mode deployments. Only applies when
# METRICS_LISTEN is set; otherwise (CronJob mode) the binary exits before
# any probe runs and Kubernetes ignores the directive.
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD wget -q --spider http://localhost:9090/healthz 2>/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/dumpscript"]
CMD ["dump"]

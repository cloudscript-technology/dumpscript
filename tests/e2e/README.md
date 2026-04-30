# E2E tests

Self-contained end-to-end tests for the single-image dumpscript build.
Spins up ephemeral databases + MinIO, runs `dumpscript dump`, verifies the
object landed in storage, then runs `dumpscript restore` and checks that
data was re-applied correctly.

No cloud credentials required — everything runs locally via podman/docker.

## Prerequisites

- `podman` (default) or `docker` (set `E2E_RUNTIME=docker`)
- ~500 MB free disk for images
- Network access to pull:
  - `postgres:16-alpine`
  - `mariadb:11.4`
  - `mongo:7`
  - `quay.io/minio/minio`, `quay.io/minio/mc`
  - `curlimages/curl:latest`

## Build the image

```sh
podman build -f docker/Dockerfile -t dumpscript:go-alpine .
```

## Run all engines

```sh
tests/e2e/run.sh
```

## Run one engine

```sh
E2E_ENGINES=postgres tests/e2e/run.sh
E2E_ENGINES=mariadb  tests/e2e/run.sh
E2E_ENGINES=mongo    tests/e2e/run.sh
```

Or any subset:

```sh
E2E_ENGINES="postgres mongo" tests/e2e/run.sh
```

## Override image / runtime

```sh
E2E_IMAGE=dumpscript:custom-tag tests/e2e/run.sh
E2E_RUNTIME=docker              tests/e2e/run.sh
```

## What each test does

1. **Starts** a database container with a known password and seed DB.
2. **Seeds** a small test table with synthetic rows (alice/bob/carol for
   SQL engines, 4 docs for Mongo).
3. **Creates** a MinIO bucket and runs `dumpscript dump` — artifact should
   land at `<prefix>/daily/YYYY/MM/DD/dump_*.(sql|archive).gz`.
4. **Verifies** the object exists in the bucket.
5. **Drops** the seeded table/collection to simulate disaster.
6. **Runs** `dumpscript restore` pointing at the uploaded object.
7. **Counts** rows/documents to confirm the expected data came back.
8. **Cleans up** every container via `trap EXIT`.

On failure the last log of the failing step is printed to stderr; successful
runs stay quiet.

## Failure output

Logs are written to `/tmp/e2e-*.log` (one per dumpscript invocation). On
failure the script prints the log of the failing step. Clean up manually
with `rm /tmp/e2e-*.log` if desired.

## Caveats

- The first run pulls ~800 MB of images; subsequent runs are fast.
- If MinIO fails to come up (rare), re-run — the script idempotently tears
  down any leftover containers with matching names on start.
- Podman on macOS requires a running machine: `podman machine start`.

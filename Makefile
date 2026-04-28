# dumpscript — development Makefile
# Run `make` (or `make help`) to see every target.

BINARY := dumpscript
IMAGE  ?= dumpscript:go-alpine
DOCKER ?= $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)
GO     ?= go

# Auto-detect podman machine socket on macOS so `make e2e` works without
# manual DOCKER_HOST export.
ifeq ($(shell uname -s),Darwin)
  PODMAN_SOCK := $(shell podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}' 2>/dev/null)
  ifneq ($(PODMAN_SOCK),)
    export DOCKER_HOST := unix://$(PODMAN_SOCK)
  endif
endif

# E2E defaults — podman stores local images prefixed with `localhost/`.
export E2E_IMAGE ?= localhost/$(IMAGE)
# Ryuk reaper misbehaves on some podman setups; disabled by default.
export TESTCONTAINERS_RYUK_DISABLED ?= true

.DEFAULT_GOAL := help

##@ General

.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n"} \
	     /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } \
	     /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Build

.PHONY: build
build: ## Compile the dumpscript binary to ./bin/.
	@mkdir -p bin
	$(GO) build -trimpath -ldflags="-s -w" -o bin/$(BINARY) ./cmd/dumpscript

.PHONY: install
install: ## Install dumpscript into $GOBIN (or $GOPATH/bin).
	$(GO) install ./cmd/dumpscript

.PHONY: image
image: ## Build the runtime container image (default: Alpine edge + PG 18).
	$(DOCKER) build -f docker/Dockerfile -t $(IMAGE) .

.PHONY: image-stable
image-stable: ## Build image pinned to Alpine 3.22 + PG 17 client.
	$(DOCKER) build -f docker/Dockerfile \
		--build-arg ALPINE_TAG=3.22 \
		--build-arg PG_CLIENT=postgresql17-client \
		-t $(IMAGE) .

##@ Code quality

.PHONY: fmt
fmt: ## gofmt -s across the repo.
	gofmt -s -w .

.PHONY: vet
vet: ## go vet (includes e2e tag).
	$(GO) vet ./...
	$(GO) vet -tags=e2e ./tests/e2e/...

.PHONY: tidy
tidy: ## go mod tidy.
	$(GO) mod tidy

.PHONY: check
check: fmt vet test ## fmt + vet + unit tests.

##@ Testing

.PHONY: test
test: ## Run unit tests (no e2e).
	$(GO) test ./... -count=1

.PHONY: test-race
test-race: ## Run unit tests with the race detector.
	$(GO) test -race ./... -count=1

.PHONY: cover
cover: ## Unit tests with coverage summary.
	$(GO) test -cover ./... -count=1

.PHONY: cover-html
cover-html: ## Generate coverage.html for browsing.
	$(GO) test -coverprofile=coverage.out ./... -count=1
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "coverage.html generated"

##@ End-to-end (requires podman/docker)

.PHONY: e2e
e2e: image ## Build image + run full e2e suite.
	$(GO) test -tags=e2e -v -count=1 ./tests/e2e/...

.PHONY: e2e-quick
e2e-quick: ## Run e2e suite without rebuilding the image.
	$(GO) test -tags=e2e -v -count=1 ./tests/e2e/...

.PHONY: e2e-postgres
e2e-postgres: ## E2E Postgres — every supported server version (13-18).
	$(GO) test -tags=e2e -v -count=1 -run "^TestPostgres$$" ./tests/e2e/...

.PHONY: e2e-engines
e2e-engines: ## E2E engines: postgres/mariadb/mysql80/mongo. Skips mysql57 (slow amd64 emulation).
	$(GO) test -tags=e2e -v -count=1 \
		-run "^TestPostgres$$|^TestPostgresCluster$$|^TestMariaDB$$|^TestMySQL80$$|^TestMongo$$" ./tests/e2e/...

.PHONY: e2e-features
e2e-features: ## E2E Azure/lock/retention/slack scenarios.
	$(GO) test -tags=e2e -v -count=1 \
		-run "^TestAzure$$|^TestLockContention$$|^TestRetention$$|^TestSlackNotification$$" ./tests/e2e/...

.PHONY: e2e-one
e2e-one: ## Run a single e2e test. Usage: make e2e-one NAME=TestMongo
	@if [ -z "$(NAME)" ]; then echo "error: set NAME=<TestName>"; exit 2; fi
	$(GO) test -tags=e2e -v -count=1 -run "^$(NAME)$$" ./tests/e2e/...

.PHONY: e2e-kind
e2e-kind: ## Kind cluster e2e: spins up kind, operator, LocalStack (via Terragrunt) + PostgreSQL, tests full backup/restore flow.
	cd tests/kind-e2e && \
		PROJECT_ROOT=$(CURDIR) $(GO) test -v -tags=kind_e2e -count=1 -timeout=45m ./...

.PHONY: e2e-kind-deps
e2e-kind-deps: ## Download Go module deps for the kind e2e module (run once or when go.mod changes).
	cd tests/kind-e2e && $(GO) mod tidy

##@ Housekeeping

.PHONY: clean
clean: ## Remove local build artefacts.
	rm -rf bin coverage.out coverage.html

.PHONY: deps
deps: ## Print direct dependencies.
	@$(GO) list -m -f '{{if not .Indirect}}{{.Path}} {{.Version}}{{end}}' all 2>/dev/null | grep -v '^$$'

.PHONY: loc
loc: ## Count lines per Go file (top 20).
	@find . -name "*.go" -not -path "./.git/*" | xargs wc -l | sort -n | tail -20

.PHONY: podman-socket
podman-socket: ## Print the DOCKER_HOST env var detected for this machine.
	@echo "DOCKER_HOST=$(DOCKER_HOST)"

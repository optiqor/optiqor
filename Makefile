.DEFAULT_GOAL := help

# ---------------- Config ----------------
GO              ?= go
GOFLAGS         ?=
LDFLAGS         ?= -s -w -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BIN_DIR         := bin
PKG             := ./...
DOCKER_COMPOSE  ?= docker compose

# ---------------- Help ----------------
.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make <target>\n\nTargets:\n"} \
		/^[a-zA-Z0-9_.-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ---------------- Tooling ----------------
.PHONY: install-tools
install-tools: ## Install dev tools (golangci-lint, gosec, govulncheck, goose, gitleaks)
	@echo "==> installing dev tools"
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GO) install github.com/securego/gosec/v2/cmd/gosec@latest
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	$(GO) install github.com/pressly/goose/v3/cmd/goose@latest

# ---------------- Build ----------------
.PHONY: build build-api build-worker build-agent
build: build-api build-worker build-agent ## Build all binaries

build-api: ## Build the api binary
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/api ./cmd/api

build-worker: ## Build the worker binary
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/worker ./cmd/worker

build-agent: ## Build the agent binary
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/agent ./cmd/agent

# ---------------- Run (local) ----------------
.PHONY: run-api run-worker run-agent
run-api: ## Run api locally
	$(GO) run ./cmd/api

run-worker: ## Run worker locally
	$(GO) run ./cmd/worker

run-agent: ## Run agent locally
	$(GO) run ./cmd/agent

# ---------------- Quality ----------------
.PHONY: lint test test-race vet fmt fmt-check security
fmt: ## gofmt -w
	$(GO) fmt $(PKG)

fmt-check: ## fail if gofmt would change anything
	@diff=$$(gofmt -l . | grep -v '^vendor/' || true); \
	if [ -n "$$diff" ]; then echo "gofmt would rewrite:"; echo "$$diff"; exit 1; fi

vet: ## go vet
	$(GO) vet $(PKG)

lint: ## golangci-lint run
	golangci-lint run

test: ## go test (no race)
	$(GO) test $(PKG)

test-race: ## go test -race
	$(GO) test -race $(PKG)

security: ## gosec + govulncheck
	gosec ./...
	govulncheck ./...

# ---------------- Local stack ----------------
.PHONY: docker-up docker-down docker-logs
docker-up: ## Bring up postgres + redis + temporal locally
	$(DOCKER_COMPOSE) up -d
	@echo "==> waiting for services to be healthy..."
	@sleep 3

docker-down: ## Tear down local stack
	$(DOCKER_COMPOSE) down -v

docker-logs: ## Tail logs from local stack
	$(DOCKER_COMPOSE) logs -f

# ---------------- DB ----------------
.PHONY: migrate migrate-down migrate-status migrate-create seed
migrate: ## Run pending migrations
	@goose -dir ./migrations postgres "$$SEVRO_POSTGRES_DSN" up

migrate-down: ## Roll back the last migration
	@goose -dir ./migrations postgres "$$SEVRO_POSTGRES_DSN" down

migrate-status: ## Show migration status
	@goose -dir ./migrations postgres "$$SEVRO_POSTGRES_DSN" status

migrate-create: ## Create a new migration: make migrate-create name=add_foo_to_bar
	@goose -dir ./migrations create $(name) sql

seed: ## Seed local dev data
	./scripts/seed.sh

# ---------------- Terraform ----------------
.PHONY: tf-fmt tf-validate-dev tf-plan-dev
tf-fmt: ## Format terraform
	terraform -chdir=infra/terraform fmt -recursive

tf-validate-dev: ## terraform validate dev
	terraform -chdir=infra/terraform/envs/dev init -backend=false
	terraform -chdir=infra/terraform/envs/dev validate

tf-plan-dev: ## terraform plan dev
	terraform -chdir=infra/terraform/envs/dev plan

# ---------------- Composite ----------------
.PHONY: dev ci
dev: docker-up migrate ## docker-up + migrate (one-shot dev bootstrap)
	@echo "==> dev stack ready. run \`make run-api\` in another terminal."

ci: fmt-check vet lint test-race security ## what CI runs

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html

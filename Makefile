.DEFAULT_GOAL := dev

COMPOSE ?= docker compose
DEPLOY_DIR ?= deploy
ROOT_COMPOSE_FILE ?= $(DEPLOY_DIR)/docker-compose.yaml
APP_DOCKERFILE ?= $(DEPLOY_DIR)/Dockerfile
IMAGE_REPOSITORY ?= ghcr.io/opensoha/soha
IMAGE_TAG ?= local
GOPROXY ?= https://proxy.golang.org,direct
SOHA_WEB_DIR ?= ../soha-web
SOHA_CONTRACTS_DIR ?= $(abspath ../soha-contracts)
WEB_DIST_DIR ?= internal/staticassets/web/dist
REMOTE_DEV_CONTEXT ?= kubernetes-admin@kubernetes
REMOTE_DEV_URL ?= https://ops.popicorns.com
REMOTE_DEV_SOHA_REF ?= main
REMOTE_DEV_WEB_REF ?= main
REMOTE_DEV_CONTRACTS_REF ?= main

ROOT_COMPOSE = $(COMPOSE) -f $(ROOT_COMPOSE_FILE)
IMAGE_BUILD_TAGS = -t $(IMAGE_REPOSITORY):$(IMAGE_TAG)

.PHONY: help init init-go init-db dev dev-api dev-web remote-dev-check remote-dev-up remote-dev-sync remote-dev-down remote-dev-status remote-dev-logs build build-web clean deploy-image test lint complexity-check complexity-report

help: ## Show available make targets.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-28s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Development
init: init-go init-db ## Install core dependencies and start local database.

init-go: ## Tidy Go modules.
	@echo "Tidying Go modules..."
	go mod tidy

init-db: ## Start local PostgreSQL from deploy/docker-compose.yaml.
	@echo "Starting PostgreSQL..."
	@$(ROOT_COMPOSE) up -d postgres; \
		printf "Waiting for PostgreSQL"; \
		until $(ROOT_COMPOSE) exec -T postgres pg_isready -U pgsql -d soha >/dev/null 2>&1; do \
			printf "."; \
			sleep 2; \
		done; \
		printf " done\n"

dev-api: ## Run the Go API server locally.
	go run ./cmd/server

dev-web: ## Run the Vite frontend locally.
	@test -d "$(SOHA_WEB_DIR)" || (echo "Missing $(SOHA_WEB_DIR). Clone github.com/opensoha/soha-web or set SOHA_WEB_DIR." >&2; exit 1)
	cd $(SOHA_WEB_DIR) && npm run dev

dev: init-db ## Start local API and frontend development servers.
	@echo "Starting api and web..."
	@$(MAKE) dev-api & api_pid=$$!; \
	$(MAKE) dev-web & web_pid=$$!; \
	trap 'kill $$api_pid $$web_pid 2>/dev/null || true' INT TERM EXIT; \
	wait $$api_pid $$web_pid

remote-dev-check: ## Validate the remote development scripts and Kustomize manifest.
	sh -n deploy/remote-dev/source-sync.sh deploy/remote-dev/backend.sh deploy/remote-dev/web.sh deploy/remote-dev/manage.sh deploy/remote-dev/local-sync.sh
	kubectl kustomize deploy/remote-dev >/dev/null
	kubectl kustomize deploy/remote-dev-local >/dev/null

remote-dev-up: remote-dev-check ## Route ops.popicorns.com to pushed development refs.
	@KUBE_CONTEXT="$(REMOTE_DEV_CONTEXT)" BASE_URL="$(REMOTE_DEV_URL)" SOHA_REF="$(REMOTE_DEV_SOHA_REF)" SOHA_WEB_REF="$(REMOTE_DEV_WEB_REF)" SOHA_CONTRACTS_REF="$(REMOTE_DEV_CONTRACTS_REF)" sh deploy/remote-dev/manage.sh up

remote-dev-sync: remote-dev-check ## Mirror local source files into ops.popicorns.com with DevSpace.
	@command -v devspace >/dev/null || { echo "DevSpace is required: https://www.devspace.sh/docs/getting-started/installation" >&2; exit 1; }
	@KUBE_CONTEXT="$(REMOTE_DEV_CONTEXT)" BASE_URL="$(REMOTE_DEV_URL)" sh deploy/remote-dev/local-sync.sh

remote-dev-down: ## Restore the Helm deployment behind ops.popicorns.com.
	@KUBE_CONTEXT="$(REMOTE_DEV_CONTEXT)" BASE_URL="$(REMOTE_DEV_URL)" sh deploy/remote-dev/manage.sh down

remote-dev-status: ## Show the active ops.popicorns.com development backend.
	@KUBE_CONTEXT="$(REMOTE_DEV_CONTEXT)" BASE_URL="$(REMOTE_DEV_URL)" sh deploy/remote-dev/manage.sh status

remote-dev-logs: ## Follow all remote development container logs.
	@KUBE_CONTEXT="$(REMOTE_DEV_CONTEXT)" sh deploy/remote-dev/manage.sh logs

# Build
build-web: ## Build the frontend artifact from soha-web and stage it for embedding.
	@test -d "$(SOHA_WEB_DIR)" || (echo "Missing $(SOHA_WEB_DIR). Clone github.com/opensoha/soha-web or set SOHA_WEB_DIR." >&2; exit 1)
	cd $(SOHA_WEB_DIR) && npm run build
	rm -rf $(WEB_DIST_DIR)
	mkdir -p $(WEB_DIST_DIR)
	cp -R $(SOHA_WEB_DIR)/dist/. $(WEB_DIST_DIR)/

build: build-web ## Build the embedded soha server binary.
	CGO_ENABLED=0 go build -tags embedassets -o bin/soha ./cmd/server

deploy-image: build-web ## Build the single-project application image.
	docker build --build-arg GOPROXY=$(GOPROXY) --build-context contracts=$(SOHA_CONTRACTS_DIR) -f $(APP_DOCKERFILE) $(IMAGE_BUILD_TAGS) .

# Test
test: ## Run Go tests.
	go test ./...

lint: ## Run the configured Go quality and security linters.
	golangci-lint run ./...

complexity-check: ## Reject production functions with cyclomatic complexity above 20.
	@output="$$(GOWORK=off go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 20 internal 2>/dev/null)"; status=$$?; \
		{ test $$status -eq 0 || test -n "$$output"; } || exit $$status; \
		output="$$(printf '%s\n' "$$output" | awk '!/_test\.go/ && $$0 !~ /\(\*PVEAdapter\)\.ResizeVM|validateSourceRepositoryInput|disabledModuleMenuIDs|filterSeedMenusByModules|\(\*Service\)\.executeVMAction|\(\*Service\)\.executeApplicationDeliveryAction/')"; \
		test -z "$$output" || { printf '%s\n' "$$output"; exit 1; }

complexity-report: ## Report complexity, duplication, function length, and maintainability findings.
	-golangci-lint run --enable-only cyclop,dupl,funlen,maintidx ./...

# Clean
clean: ## Remove local build outputs.
	rm -rf bin/ $(WEB_DIST_DIR)

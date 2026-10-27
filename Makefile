# Local host guard; standalone clones and GitHub Actions keep their existing toolchain.
LOCAL_VERIFY :=
LOCAL_DOCKER_BUILD := docker build
ifneq ($(GITHUB_ACTIONS),true)
ifneq ($(shell command -v opensoha-verify 2>/dev/null)$(OPENSOHA_LOCAL_VERIFY_REQUIRED),)
LOCAL_VERIFY := opensoha-verify run --
LOCAL_DOCKER_BUILD := opensoha-verify build-image --
endif
endif

.DEFAULT_GOAL := dev

COMPOSE ?= docker compose
DEPLOY_DIR ?= deploy
ROOT_COMPOSE_FILE ?= $(DEPLOY_DIR)/docker-compose.yaml
APP_DOCKERFILE ?= $(DEPLOY_DIR)/Dockerfile
IMAGE_REPOSITORY ?= ghcr.io/opensoha/soha
IMAGE_TAG ?= local
SOHA_BUILD_VERSION ?= $(shell (git describe --tags --abbrev=0 2>/dev/null || echo v0.1.0) | sed s/^v//)
SOHA_VERSION_LDFLAGS = -X github.com/opensoha/soha/internal/application/plugin.currentSohaVersion=$(SOHA_BUILD_VERSION)
GOPROXY ?= https://proxy.golang.org,direct
SOHA_WEB_DIR ?= ../soha-web
SOHA_CONTRACTS_DIR ?= $(abspath ../soha-contracts)
SOHA_CONFIG_FILE ?= configs/config.yaml
export SOHA_CONFIG_FILE
DEV_DB_MODE ?= external
WEB_DIST_DIR ?= internal/staticassets/web/dist
REMOTE_DEV_CONTEXT ?= kubernetes-admin@kubernetes
REMOTE_DEV_URL ?= https://ops.popicorns.com
REMOTE_DEV_SOHA_REF ?= main
REMOTE_DEV_WEB_REF ?= main
REMOTE_DEV_CONTRACTS_REF ?= main

ROOT_COMPOSE = $(COMPOSE) -f $(ROOT_COMPOSE_FILE)
IMAGE_BUILD_TAGS = -t $(IMAGE_REPOSITORY):$(IMAGE_TAG)

.PHONY: help init init-config init-go init-db dev dev-local dev-remote dev-api dev-web remote-dev-check remote-dev-up remote-dev-down remote-dev-status remote-dev-logs build build-web clean deploy-image test lint complexity-check complexity-report

help: ## Show available make targets.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-28s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Development
init: init-go init-db ## Install dependencies and prepare the selected database mode.

init-config: ## Copy public defaults once; local config is never overwritten.
	@if [ ! -f "$(SOHA_CONFIG_FILE)" ]; then \
		if [ "$(SOHA_CONFIG_FILE)" != "configs/config.yaml" ]; then \
			echo "Missing config: $(SOHA_CONFIG_FILE)" >&2; exit 1; \
		fi; \
		cp configs/config.example.yaml "$(SOHA_CONFIG_FILE)"; \
		echo "Created $(SOHA_CONFIG_FILE); configure your database before starting."; \
	fi

init-go: ## Tidy Go modules.
	@echo "Tidying Go modules..."
	$(LOCAL_VERIFY) go mod tidy

init-db: init-config ## Use an external database, or start Docker with DEV_DB_MODE=local.
	@case "$(DEV_DB_MODE)" in \
		external) echo "Using the external database configured in $(SOHA_CONFIG_FILE)." ;; \
		local) \
			$(ROOT_COMPOSE) up -d postgres || exit $$?; \
			printf "Waiting for PostgreSQL"; attempts=0; \
			until $(ROOT_COMPOSE) exec -T postgres pg_isready -U pgsql -d soha >/dev/null 2>&1; do \
				attempts=$$((attempts + 1)); \
				if [ $$attempts -ge 60 ]; then echo " PostgreSQL did not become ready" >&2; exit 1; fi; \
				printf "."; sleep 2; \
			done; printf " done\n" ;; \
		*) echo "Invalid DEV_DB_MODE: $(DEV_DB_MODE); use external or local" >&2; exit 1 ;; \
	esac

dev-api: init-config ## Run the Go API server locally.
	go run -ldflags="$(SOHA_VERSION_LDFLAGS)" ./cmd/server

dev-web: ## Run the Vite frontend locally.
	@test -d "$(SOHA_WEB_DIR)" || (echo "Missing $(SOHA_WEB_DIR). Clone github.com/opensoha/soha-web or set SOHA_WEB_DIR." >&2; exit 1)
	cd $(SOHA_WEB_DIR) && npm run dev

dev: init-db ## Start local API and frontend development servers.
	@echo "Starting api and web..."
	@$(MAKE) dev-api & api_pid=$$!; \
	$(MAKE) dev-web & web_pid=$$!; \
	trap 'kill $$api_pid $$web_pid 2>/dev/null || true' INT TERM EXIT; \
	wait $$api_pid $$web_pid

dev-local: DEV_DB_MODE=local
dev-local: dev ## Start local PostgreSQL, API, and frontend.

dev-remote: DEV_DB_MODE=external
dev-remote: dev ## Run API and frontend against an existing external database.

remote-dev-check: ## Validate the remote development scripts and Kustomize manifest.
	sh -n deploy/remote-dev/source-sync.sh deploy/remote-dev/backend.sh deploy/remote-dev/web.sh deploy/remote-dev/manage.sh
	kubectl kustomize deploy/remote-dev >/dev/null

remote-dev-up: remote-dev-check ## Route ops.popicorns.com to pushed development refs.
	@KUBE_CONTEXT="$(REMOTE_DEV_CONTEXT)" BASE_URL="$(REMOTE_DEV_URL)" SOHA_REF="$(REMOTE_DEV_SOHA_REF)" SOHA_WEB_REF="$(REMOTE_DEV_WEB_REF)" SOHA_CONTRACTS_REF="$(REMOTE_DEV_CONTRACTS_REF)" sh deploy/remote-dev/manage.sh up

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

build: build-web ## Build the management, network-control, and ingest binaries.
	CGO_ENABLED=0 $(LOCAL_VERIFY) go build -tags embedassets -ldflags="$(SOHA_VERSION_LDFLAGS)" -o bin/soha ./cmd/server
	CGO_ENABLED=0 $(LOCAL_VERIFY) go build -o bin/network-control ./cmd/network-control
	CGO_ENABLED=0 $(LOCAL_VERIFY) go build -o bin/ingest ./cmd/ingest

deploy-image: build-web ## Build the application and network runtime image.
	$(LOCAL_DOCKER_BUILD) --build-arg SOHA_BUILD_VERSION=$(SOHA_BUILD_VERSION) --build-arg GOPROXY=$(GOPROXY) --build-context contracts=$(SOHA_CONTRACTS_DIR) -f $(APP_DOCKERFILE) $(IMAGE_BUILD_TAGS) .

.PHONY: verify-image
verify-image: build-web ## Validate the application image and remove this run's tag.
	opensoha-verify image -- --build-arg SOHA_BUILD_VERSION=$(SOHA_BUILD_VERSION) --build-arg GOPROXY=$(GOPROXY) --build-context contracts=$(SOHA_CONTRACTS_DIR) -f $(APP_DOCKERFILE) .

# Test
test: ## Run Go tests.
	$(LOCAL_VERIFY) go test ./...

lint: ## Run the configured Go quality and security linters.
	$(LOCAL_VERIFY) golangci-lint run ./...

complexity-check: ## Reject production functions with cyclomatic complexity above 20.
	@output="$$(GOWORK=off $(LOCAL_VERIFY) go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 20 internal 2>/dev/null)"; status=$$?; \
		{ test $$status -eq 0 || test -n "$$output"; } || exit $$status; \
		output="$$(printf '%s\n' "$$output" | awk '!/_test\.go/ && $$0 !~ /\(\*PVEAdapter\)\.ResizeVM|validateSourceRepositoryInput|disabledModuleMenuIDs|filterSeedMenusByModules|\(\*Service\)\.executeVMAction|\(\*Service\)\.executeApplicationDeliveryAction/')"; \
		test -z "$$output" || { printf '%s\n' "$$output"; exit 1; }

complexity-report: ## Report complexity, duplication, function length, and maintainability findings.
	$(LOCAL_VERIFY) sh -c 'golangci-lint run --enable-only cyclop,dupl,funlen,maintidx ./... || true'

# Clean
clean: ## Remove local build outputs.
	rm -rf bin/ $(WEB_DIST_DIR)

.DEFAULT_GOAL := dev

COMPOSE ?= docker compose
DEPLOY_DIR ?= deploy
ROOT_COMPOSE_FILE ?= $(DEPLOY_DIR)/docker-compose.yaml
APP_DOCKERFILE ?= $(DEPLOY_DIR)/Dockerfile
HELM_CHART ?= $(DEPLOY_DIR)/chart
SOHA_AGENT_CHART ?= $(SOHA_AGENT_DIR)/deploy/charts/soha-agent
SOHA_HERMES_AGENT_CHART ?= $(SOHA_AGENT_DIR)/deploy/charts/soha-hermes-agent
HELM_CHARTS ?= $(HELM_CHART) $(SOHA_AGENT_CHART) $(SOHA_HERMES_AGENT_CHART)
KUSTOMIZE_DIR ?= $(DEPLOY_DIR)
IMAGE_REPOSITORY ?= yshanchui/soha
IMAGE_TAG ?= local
IMAGE_PLATFORMS ?= linux/amd64,linux/arm64
GOPROXY ?= https://proxy.golang.org,direct
PUSH_LATEST ?= 0
HELM_PACKAGE_DIR ?= dist/helm-packages
HELM_REPO_DIR ?= dist/helm-repo
HELM_REPO_URL ?= https://opensoha.github.io/soha-helm
HELM_LINT_AGENT_TOKEN ?= test-agent-token-123456
HELM_LINT_RUNNER_TOKEN ?= test-runner-token-123456
HERMES_CONTROL_PLANE_URL ?= http://host.docker.internal:8080
HERMES_RUNTIME_ENDPOINT ?= http://127.0.0.1:18080
SOHA_WEB_DIR ?= ../soha-web
SOHA_DOCS_DIR ?= ../soha-docs
SOHA_AGENT_DIR ?= $(abspath ../soha-agent)
SOHA_CONTRACTS_DIR ?= $(abspath ../soha-contracts)
WEB_DIST_DIR ?= internal/staticassets/web/dist

ROOT_COMPOSE = $(COMPOSE) -f $(ROOT_COMPOSE_FILE)
IMAGE_BUILD_TAGS = -t $(IMAGE_REPOSITORY):$(IMAGE_TAG)
ifeq ($(PUSH_LATEST),1)
IMAGE_BUILD_TAGS += -t $(IMAGE_REPOSITORY):latest
endif

.PHONY: help init init-go init-web init-docs init-db init-cluster init-hermes dev dev-api dev-web dev-docs build build-web build-docs clean deploy-image deploy-image-push deploy-compose-up deploy-compose-down deploy-compose-config deploy-compose-smoke deploy-kustomize-render deploy-helm-lint deploy-helm-template deploy-helm-template-all deploy-helm-package deploy-helm-repo test-api test-web

help: ## Show available make targets.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-28s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Development
init: init-go init-db init-cluster ## Install core dependencies and start local database plus k3s.

init-go: ## Tidy Go modules.
	@echo "Tidying Go modules..."
	go mod tidy

init-web: ## Install frontend dependencies.
	@echo "Installing web dependencies..."
	@test -d "$(SOHA_WEB_DIR)" || (echo "Missing $(SOHA_WEB_DIR). Clone github.com/opensoha/soha-web or set SOHA_WEB_DIR." >&2; exit 1)
	cd $(SOHA_WEB_DIR) && npm install

init-docs: ## Install docs dependencies.
	@echo "Installing docs dependencies..."
	@test -d "$(SOHA_DOCS_DIR)" || (echo "Missing $(SOHA_DOCS_DIR). Clone github.com/opensoha/soha-docs or set SOHA_DOCS_DIR." >&2; exit 1)
	cd $(SOHA_DOCS_DIR) && npm install

init-db: ## Start local PostgreSQL from deploy/docker-compose.yaml.
	@echo "Starting PostgreSQL..."
	@$(ROOT_COMPOSE) up -d postgres
	@printf "Waiting for PostgreSQL"; \
	until $(ROOT_COMPOSE) exec -T postgres pg_isready -U pgsql -d soha >/dev/null 2>&1; do \
		printf "."; \
		sleep 2; \
	done; \
	printf " done\n"

init-cluster: ## Start local k3s and write .dev/k3s/kubeconfig.yaml.
	@mkdir -p .dev/k3s
	@echo "Starting local K3s..."
	@if ! docker container inspect soha-k3s >/dev/null 2>&1; then \
		docker network disconnect -f soha_default soha-k3s >/dev/null 2>&1 || true; \
	fi
	@$(ROOT_COMPOSE) up -d k3s
	@printf "Waiting for K3s"; \
	until $(ROOT_COMPOSE) exec -T k3s kubectl get nodes >/dev/null 2>&1; do \
		printf "."; \
		sleep 2; \
	done; \
	printf " done\n"
	@$(ROOT_COMPOSE) cp k3s:/etc/rancher/k3s/k3s.yaml .dev/k3s/kubeconfig.yaml >/dev/null
	@perl -0pi -e 's#server: https://[^\n]+#server: https://127.0.0.1:6443#' .dev/k3s/kubeconfig.yaml
	@chmod 644 .dev/k3s/kubeconfig.yaml
	@echo "K3s kubeconfig written to .dev/k3s/kubeconfig.yaml"

init-hermes: ## Start Hermes Agent Runtime runner for local AI workbench testing.
	@echo "Starting Hermes Agent Runtime runner..."
	@test -d "$(SOHA_AGENT_DIR)" || (echo "Missing $(SOHA_AGENT_DIR). Clone github.com/opensoha/soha-agent or set SOHA_AGENT_DIR." >&2; exit 1)
	@test -d "$(SOHA_CONTRACTS_DIR)" || (echo "Missing $(SOHA_CONTRACTS_DIR). Clone github.com/opensoha/soha-contracts or set SOHA_CONTRACTS_DIR." >&2; exit 1)
	@SOHA_AGENT_DIR="$(SOHA_AGENT_DIR)" \
		SOHA_CONTRACTS_DIR="$(SOHA_CONTRACTS_DIR)" \
		SOHA_CONTROL_PLANE_URL="$(HERMES_CONTROL_PLANE_URL)" \
		SOHA_HERMES_RUNTIME_ENDPOINT="$(HERMES_RUNTIME_ENDPOINT)" \
		$(ROOT_COMPOSE) up -d --no-deps --build hermes-agent-runner

dev-api: ## Run the Go API server locally.
	go run ./cmd/server

dev-web: ## Run the Vite frontend locally.
	@test -d "$(SOHA_WEB_DIR)" || (echo "Missing $(SOHA_WEB_DIR). Clone github.com/opensoha/soha-web or set SOHA_WEB_DIR." >&2; exit 1)
	cd $(SOHA_WEB_DIR) && npm run dev

dev-docs: ## Run the Docusaurus docs site locally.
	@test -d "$(SOHA_DOCS_DIR)" || (echo "Missing $(SOHA_DOCS_DIR). Clone github.com/opensoha/soha-docs or set SOHA_DOCS_DIR." >&2; exit 1)
	cd $(SOHA_DOCS_DIR) && npm run dev

dev: init-db init-cluster ## Start local API and frontend development servers.
	@echo "Starting api and web..."
	@trap 'kill 0' INT TERM EXIT; \
	$(MAKE) dev-api & \
	$(MAKE) dev-web & \
	wait

# Build
build-web: ## Build the frontend artifact from soha-web and stage it for embedding.
	@test -d "$(SOHA_WEB_DIR)" || (echo "Missing $(SOHA_WEB_DIR). Clone github.com/opensoha/soha-web or set SOHA_WEB_DIR." >&2; exit 1)
	cd $(SOHA_WEB_DIR) && npm run build
	rm -rf $(WEB_DIST_DIR)
	mkdir -p $(WEB_DIST_DIR)
	cp -R $(SOHA_WEB_DIR)/dist/. $(WEB_DIST_DIR)/

build-docs: ## Build the docs site in soha-docs.
	@test -d "$(SOHA_DOCS_DIR)" || (echo "Missing $(SOHA_DOCS_DIR). Clone github.com/opensoha/soha-docs or set SOHA_DOCS_DIR." >&2; exit 1)
	cd $(SOHA_DOCS_DIR) && npm run build

build: build-web ## Build the embedded soha server binary.
	CGO_ENABLED=0 go build -tags embedassets -o bin/soha ./cmd/server

deploy-image: build-web ## Build the single-project application image.
	docker build --build-arg GOPROXY=$(GOPROXY) --build-context contracts=$(SOHA_CONTRACTS_DIR) -f $(APP_DOCKERFILE) $(IMAGE_BUILD_TAGS) .

deploy-image-push: build-web ## Build and push the multi-arch application image to the configured registry.
	@test "$(IMAGE_TAG)" != "local" || (echo "Set IMAGE_TAG to a release version before pushing." >&2; exit 1)
	docker buildx build --platform $(IMAGE_PLATFORMS) --build-arg GOPROXY=$(GOPROXY) --build-context contracts=$(SOHA_CONTRACTS_DIR) -f $(APP_DOCKERFILE) $(IMAGE_BUILD_TAGS) --push .

deploy-compose-up: build-web ## Start the local single-project compose stack.
	$(ROOT_COMPOSE) up -d --build

deploy-compose-down: ## Stop the local single-project compose stack.
	$(ROOT_COMPOSE) down

deploy-compose-config: ## Render the local single-project compose config.
	$(ROOT_COMPOSE) config

deploy-compose-smoke: build-web ## Run the app-container cold-start smoke against PostgreSQL and embedded web assets.
	./scripts/smoke-compose.sh

deploy-kustomize-render: ## Render the raw Kubernetes baseline through Kustomize.
	kubectl kustomize $(KUSTOMIZE_DIR)

deploy-helm-lint: ## Lint the Helm chart.
	@test -d "$(HELM_CHART)" || (echo "Missing Helm chart: $(HELM_CHART)" >&2; exit 1)
	@test -d "$(SOHA_AGENT_CHART)" || (echo "Missing Helm chart: $(SOHA_AGENT_CHART)" >&2; exit 1)
	@test -d "$(SOHA_HERMES_AGENT_CHART)" || (echo "Missing Helm chart: $(SOHA_HERMES_AGENT_CHART)" >&2; exit 1)
	helm lint "$(HELM_CHART)"
	helm lint "$(SOHA_AGENT_CHART)" \
		--set-string secrets.agentBearerToken="$(HELM_LINT_AGENT_TOKEN)" \
		--set-string secrets.controlPlaneBearerToken="$(HELM_LINT_RUNNER_TOKEN)"
	helm lint "$(SOHA_HERMES_AGENT_CHART)" \
		--set-string secrets.controlPlaneBearerToken="$(HELM_LINT_RUNNER_TOKEN)"

deploy-helm-template: ## Render the Helm chart locally.
	helm template soha $(HELM_CHART)

deploy-helm-template-all: ## Render all publishable Helm charts locally with non-secret test values.
	helm template soha "$(HELM_CHART)" >/tmp/soha-chart.yaml
	helm template soha-agent "$(SOHA_AGENT_CHART)" \
		--set-string secrets.agentBearerToken="$(HELM_LINT_AGENT_TOKEN)" \
		--set-string secrets.controlPlaneBearerToken="$(HELM_LINT_RUNNER_TOKEN)" \
		>/tmp/soha-agent-chart.yaml
	helm template soha-hermes-agent "$(SOHA_HERMES_AGENT_CHART)" \
		--set-string secrets.controlPlaneBearerToken="$(HELM_LINT_RUNNER_TOKEN)" \
		>/tmp/soha-hermes-agent-chart.yaml

deploy-helm-package: ## Package the Helm chart into dist/helm-packages.
	mkdir -p $(HELM_PACKAGE_DIR)
	@for chart in $(HELM_CHARTS); do \
		test -d "$$chart" || (echo "Missing Helm chart: $$chart" >&2; exit 1); \
		helm package "$$chart" --destination $(HELM_PACKAGE_DIR); \
	done

deploy-helm-repo: deploy-helm-package ## Build a static Helm repository index under dist/helm-repo.
	mkdir -p $(HELM_REPO_DIR)
	cp $(HELM_PACKAGE_DIR)/*.tgz $(HELM_REPO_DIR)/
	@if [ -f "$(HELM_CHART)/artifacthub-repo.yml" ]; then cp "$(HELM_CHART)/artifacthub-repo.yml" "$(HELM_REPO_DIR)/artifacthub-repo.yml"; fi
	@if [ -f "$(HELM_REPO_DIR)/index.yaml" ]; then \
		helm repo index $(HELM_REPO_DIR) --url $(HELM_REPO_URL) --merge $(HELM_REPO_DIR)/index.yaml; \
	else \
		helm repo index $(HELM_REPO_DIR) --url $(HELM_REPO_URL); \
	fi

# Test
test-api: ## Run Go tests.
	go test ./...

test-web: ## Run frontend tests.
	@test -d "$(SOHA_WEB_DIR)" || (echo "Missing $(SOHA_WEB_DIR). Clone github.com/opensoha/soha-web or set SOHA_WEB_DIR." >&2; exit 1)
	cd $(SOHA_WEB_DIR) && npm run test

# Clean
clean: ## Remove local build outputs.
	rm -rf bin/ $(WEB_DIST_DIR)

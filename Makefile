.DEFAULT_GOAL := dev

COMPOSE ?= docker compose
DEPLOY_DIR ?= deploy
ROOT_COMPOSE_FILE ?= $(DEPLOY_DIR)/docker-compose.yaml
APP_DOCKERFILE ?= $(DEPLOY_DIR)/Dockerfile
HELM_CHART ?= $(DEPLOY_DIR)/chart
HERMES_CONTROL_PLANE_URL ?= http://host.docker.internal:8080
HERMES_RUNTIME_ENDPOINT ?= http://127.0.0.1:18080

ROOT_COMPOSE = $(COMPOSE) -f $(ROOT_COMPOSE_FILE)

.PHONY: help init init-go init-web init-docs init-db init-cluster init-hermes dev dev-api dev-web dev-docs build build-web build-docs clean deploy-image deploy-compose-up deploy-compose-down deploy-compose-config deploy-helm-lint test-api test-web

help: ## Show available make targets.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-28s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Development
init: init-go init-web init-docs init-db init-cluster ## Install dependencies and start local database plus k3s.

init-go: ## Tidy Go modules.
	@echo "Tidying Go modules..."
	go mod tidy

init-web: ## Install frontend dependencies.
	@echo "Installing web dependencies..."
	cd web && npm install

init-docs: ## Install docs dependencies.
	@echo "Installing docs dependencies..."
	cd docs && npm install

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
	@SOHA_CONTROL_PLANE_URL="$(HERMES_CONTROL_PLANE_URL)" \
		SOHA_HERMES_RUNTIME_ENDPOINT="$(HERMES_RUNTIME_ENDPOINT)" \
		$(ROOT_COMPOSE) up -d --no-deps --build hermes-agent-runner

dev-api: ## Run the Go API server locally.
	go run ./cmd/server

dev-web: ## Run the Vite frontend locally.
	cd web && npm run dev

dev-docs: ## Run the Docusaurus docs site locally.
	cd docs && npm run dev

dev: init-db init-cluster ## Start local API and frontend development servers.
	@echo "Starting api and web..."
	@trap 'kill 0' INT TERM EXIT; \
	$(MAKE) dev-api & \
	$(MAKE) dev-web & \
	wait

# Build
build-web: ## Build the frontend.
	cd web && npm run build

build-docs: ## Build the docs site.
	cd docs && npm run build

build: build-web build-docs ## Build the embedded soha server binary.
	CGO_ENABLED=0 go build -tags embedassets -o bin/soha ./cmd/server

deploy-image: ## Build the single-project application image.
	docker build -f $(APP_DOCKERFILE) -t soha:single-project .

deploy-compose-up: ## Start the local single-project compose stack.
	$(ROOT_COMPOSE) up -d --build

deploy-compose-down: ## Stop the local single-project compose stack.
	$(ROOT_COMPOSE) down

deploy-compose-config: ## Render the local single-project compose config.
	$(ROOT_COMPOSE) config

deploy-helm-lint: ## Lint the Helm chart.
	helm lint $(HELM_CHART)

# Test
test-api: ## Run Go tests.
	go test ./...

test-web: ## Run frontend tests.
	cd web && npm run test

# Clean
clean: ## Remove local build outputs.
	rm -rf bin/ web/dist/ docs/build/ docs/.docusaurus/

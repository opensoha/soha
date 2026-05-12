.DEFAULT_GOAL := dev

.PHONY: init init-go init-web init-docs init-db init-cluster dev dev-api dev-web dev-docs dev-all build build-web build-docs clean deploy-image deploy-compose-up deploy-compose-down deploy-compose-config deploy-helm-lint

COMPOSE ?= docker compose
ROOT_COMPOSE_FILE ?= docker-compose.yaml

# Development
init: init-go init-web init-docs init-db init-cluster

init-go:
	@echo "Tidying Go modules..."
	go mod tidy

init-web:
	@echo "Installing web dependencies..."
	cd web && npm install

init-docs:
	@echo "Installing docs dependencies..."
	cd docs && npm install

init-db:
	@echo "Starting PostgreSQL..."
	@$(COMPOSE) -f $(ROOT_COMPOSE_FILE) up -d postgres
	@printf "Waiting for PostgreSQL"; \
	until $(COMPOSE) -f $(ROOT_COMPOSE_FILE) exec -T postgres pg_isready -U pgsql -d kubecrux >/dev/null 2>&1; do \
		printf "."; \
		sleep 2; \
	done; \
	printf " done\n"

init-cluster:
	@mkdir -p .dev/k3s
	@echo "Starting local K3s..."
	@$(COMPOSE) -f $(ROOT_COMPOSE_FILE) up -d k3s
	@printf "Waiting for K3s"; \
	until $(COMPOSE) -f $(ROOT_COMPOSE_FILE) exec -T k3s kubectl get nodes >/dev/null 2>&1; do \
		printf "."; \
		sleep 2; \
	done; \
	printf " done\n"
	@$(COMPOSE) -f $(ROOT_COMPOSE_FILE) cp k3s:/etc/rancher/k3s/k3s.yaml .dev/k3s/kubeconfig.yaml >/dev/null
	@perl -0pi -e 's#server: https://[^\n]+#server: https://127.0.0.1:6443#' .dev/k3s/kubeconfig.yaml
	@chmod 644 .dev/k3s/kubeconfig.yaml
	@echo "K3s kubeconfig written to .dev/k3s/kubeconfig.yaml"

dev-api:
	go run ./cmd/server

dev-web:
	cd web && npm run dev

dev-docs:
	cd docs && npm run dev

dev-all: init-db init-cluster
	@echo "Starting api and web..."
	@trap 'kill 0' INT TERM EXIT; \
	$(MAKE) dev-api & \
	$(MAKE) dev-web & \
	wait

dev: dev-all

# Build
build-web:
	cd web && npm run build

build-docs:
	cd docs && npm run build

build: build-web build-docs
	CGO_ENABLED=0 go build -tags embedassets -o bin/kubecrux ./cmd/server

deploy-image:
	docker build -t kubecrux:single-project .

deploy-compose-up:
	docker compose -f $(ROOT_COMPOSE_FILE) up -d --build

deploy-compose-down:
	docker compose -f $(ROOT_COMPOSE_FILE) down

deploy-compose-config:
	docker compose -f $(ROOT_COMPOSE_FILE) config

deploy-helm-lint:
	helm lint chart

# Test
test-api:
	go test ./...

test-web:
	cd web && npm run test

# Clean
clean:
	rm -rf bin/ web/dist/ docs/build/ docs/.docusaurus/

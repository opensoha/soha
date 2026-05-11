.DEFAULT_GOAL := dev

.PHONY: init dev dev-api dev-web dev-docs dev-all build build-web build-docs clean deploy-image deploy-compose-up deploy-compose-down deploy-compose-config deploy-helm-lint

COMPOSE ?= docker compose
ROOT_COMPOSE_FILE ?= docker-compose.yaml

# Development
init:
	@$(COMPOSE) -f $(ROOT_COMPOSE_FILE) up -d postgres
	@printf "Waiting for PostgreSQL"; \
	until $(COMPOSE) -f $(ROOT_COMPOSE_FILE) exec -T postgres pg_isready -U pgsql -d kubecrux >/dev/null 2>&1; do \
		printf "."; \
		sleep 2; \
	done; \
	printf " done\n"

dev-api:
	go run ./cmd/server

dev-web:
	cd web && npm run dev

dev-docs:
	cd docs && npm run dev

dev-all: init
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
	CGO_ENABLED=0 go build -o bin/kubecrux ./cmd/server

deploy-image:
	docker build -t kubecrux:single-project .

deploy-compose-up:
	docker compose -f $(ROOT_COMPOSE_FILE) up -d --build

deploy-compose-down:
	docker compose -f $(ROOT_COMPOSE_FILE) down

deploy-compose-config:
	docker compose -f $(ROOT_COMPOSE_FILE) config

deploy-helm-lint:
	helm lint deploy/helm/kubecrux

# Test
test-api:
	go test ./...

test-web:
	cd web && npm run test

# Clean
clean:
	rm -rf bin/ web/dist/ docs/build/ docs/.docusaurus/

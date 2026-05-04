.PHONY: dev-api dev-web dev-docs build build-web build-docs clean deploy-image deploy-compose-up deploy-compose-down deploy-compose-config deploy-helm-lint

# Development
dev-api:
	go run ./cmd/server

dev-web:
	cd web && npm run dev

dev-docs:
	cd docs && npm run dev

dev-all:
	@echo "Starting api, web and docs..."
	@trap 'kill 0' INT TERM EXIT; \
	$(MAKE) dev-api & \
	$(MAKE) dev-web & \
	wait

# Build
build-web:
	cd web && npm run build

build-docs:
	cd docs && npm run build

build: build-web build-docs
	CGO_ENABLED=0 go build -o bin/kubecrux ./cmd/server

deploy-image:
	docker build -f deploy/docker/Dockerfile.single-project -t kubecrux:single-project .

deploy-compose-up:
	docker compose -f deploy/compose/docker-compose.single-project.yml up -d --build

deploy-compose-down:
	docker compose -f deploy/compose/docker-compose.single-project.yml down

deploy-compose-config:
	docker compose -f deploy/compose/docker-compose.single-project.yml config

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

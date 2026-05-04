.PHONY: dev-api dev-web dev-docs build build-web build-docs clean

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

# Test
test-api:
	go test ./...

test-web:
	cd web && npm run test

# Clean
clean:
	rm -rf bin/ web/dist/ docs/build/ docs/.docusaurus/

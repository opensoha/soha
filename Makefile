.PHONY: dev-api dev-web dev-docs build build-web build-docs clean

# Development
dev-api:
	go run ./cmd/server

dev-web:
	cd web && npm run dev

dev-docs:
	cd docs && npm run docs:dev

# Build
build-web:
	cd web && npm run build

build-docs:
	cd docs && npm run docs:build

build: build-web build-docs
	CGO_ENABLED=0 go build -o bin/kubecrux ./cmd/server

# Test
test-api:
	go test ./...

test-web:
	cd web && npm run test

# Clean
clean:
	rm -rf bin/ web/dist/ docs/.vitepress/dist/

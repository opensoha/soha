.DEFAULT_GOAL := help
# Local host guard; standalone clones and GitHub Actions keep their existing toolchain.
LOCAL_VERIFY :=
LOCAL_DOCKER_BUILD := docker build
ifneq ($(GITHUB_ACTIONS),true)
ifneq ($(shell command -v opensoha-verify 2>/dev/null)$(OPENSOHA_LOCAL_VERIFY_REQUIRED),)
LOCAL_VERIFY := opensoha-verify run --
LOCAL_DOCKER_BUILD := opensoha-verify build-image --
endif
endif

IMAGE_TAG ?= local
IMAGE_PLATFORMS ?= linux/amd64,linux/arm64
GOPROXY ?= https://proxy.golang.org,direct
PUSH_LATEST ?= 0
SOHA_CONTRACTS_DIR ?= $(abspath ../soha-contracts)
AGENT_IMAGE_REPOSITORY ?= ghcr.io/opensoha/soha-agent
HERMES_IMAGE_REPOSITORY ?= ghcr.io/opensoha/soha-hermes-agent

AGENT_IMAGE_TAGS = -t $(AGENT_IMAGE_REPOSITORY):$(IMAGE_TAG)
HERMES_IMAGE_TAGS = -t $(HERMES_IMAGE_REPOSITORY):$(IMAGE_TAG)
ifeq ($(PUSH_LATEST),1)
AGENT_IMAGE_TAGS += -t $(AGENT_IMAGE_REPOSITORY):latest
HERMES_IMAGE_TAGS += -t $(HERMES_IMAGE_REPOSITORY):latest
endif

.PHONY: help build deploy-agent-image deploy-hermes-image deploy-images deploy-agent-image-push deploy-hermes-image-push deploy-images-push
.PHONY: test vet verify-images

test: ## Run isolated Go tests with local capacity checks.
	GOWORK=off $(LOCAL_VERIFY) go test ./...

vet: ## Run Go vet with local capacity checks.
	GOWORK=off $(LOCAL_VERIFY) go vet ./...

verify-images: ## Validate all runtime images and remove this run's tags.
	opensoha-verify image -- --build-arg GOPROXY=$(GOPROXY) --build-context contracts=$(SOHA_CONTRACTS_DIR) -f deploy/Dockerfile .
	opensoha-verify image -- --build-arg GOPROXY=$(GOPROXY) --build-context contracts=$(SOHA_CONTRACTS_DIR) -f deploy/Dockerfile --target buildpacks-runtime .
	opensoha-verify image -- --build-arg GOPROXY=$(GOPROXY) --build-context contracts=$(SOHA_CONTRACTS_DIR) -f deploy/Dockerfile --target buildpacks-daemonless-runtime .
	opensoha-verify image -- --build-arg GOPROXY=$(GOPROXY) --build-context contracts=$(SOHA_CONTRACTS_DIR) -f deploy/Dockerfile.hermes-agent-runner .

help:
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-28s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the soha-agent binary.
	CGO_ENABLED=0 $(LOCAL_VERIFY) go build -o bin/soha-agent ./cmd/agent

deploy-agent-image: ## Build the generic soha-agent image.
	$(LOCAL_DOCKER_BUILD) --build-arg GOPROXY=$(GOPROXY) --build-context contracts=$(SOHA_CONTRACTS_DIR) -f deploy/Dockerfile $(AGENT_IMAGE_TAGS) .

deploy-hermes-image: ## Build the Hermes runner image.
	$(LOCAL_DOCKER_BUILD) --build-arg GOPROXY=$(GOPROXY) --build-context contracts=$(SOHA_CONTRACTS_DIR) -f deploy/Dockerfile.hermes-agent-runner $(HERMES_IMAGE_TAGS) .

deploy-images: deploy-agent-image deploy-hermes-image ## Build both agent images.

deploy-agent-image-push: ## Build and push the generic soha-agent image.
	@test "$(IMAGE_TAG)" != "local" || (echo "Set IMAGE_TAG to a release version before pushing." >&2; exit 1)
	docker buildx build --platform $(IMAGE_PLATFORMS) --build-arg GOPROXY=$(GOPROXY) --build-context contracts=$(SOHA_CONTRACTS_DIR) -f deploy/Dockerfile $(AGENT_IMAGE_TAGS) --push .

deploy-hermes-image-push: ## Build and push the Hermes runner image.
	@test "$(IMAGE_TAG)" != "local" || (echo "Set IMAGE_TAG to a release version before pushing." >&2; exit 1)
	docker buildx build --platform $(IMAGE_PLATFORMS) --build-arg GOPROXY=$(GOPROXY) --build-context contracts=$(SOHA_CONTRACTS_DIR) -f deploy/Dockerfile.hermes-agent-runner $(HERMES_IMAGE_TAGS) --push .

deploy-images-push: deploy-agent-image-push deploy-hermes-image-push ## Build and push both agent images.

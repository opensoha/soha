.DEFAULT_GOAL := help
# Component commands use the same Root source workspace; distribution checks are explicit.
LOCAL_VERIFY :=
LOCAL_DOCKER_BUILD := docker build
ifneq ($(GITHUB_ACTIONS),true)
ifneq ($(shell command -v opensoha-verify 2>/dev/null)$(OPENSOHA_LOCAL_VERIFY_REQUIRED),)
LOCAL_VERIFY := opensoha-verify run --
LOCAL_DOCKER_BUILD := opensoha-verify build-image --
endif
endif

COMPONENT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
SOURCE_ROOT := $(abspath $(COMPONENT_DIR)/..)
SOURCE_GOWORK := $(SOURCE_ROOT)/go.work
SOURCE_COMMIT := $(shell git -C "$(SOURCE_ROOT)" rev-parse HEAD)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION ?=
BUILD_OUTPUT ?= bin/soha-agent
IMAGE_METADATA = --build-arg COMMIT=$(SOURCE_COMMIT) --build-arg DATE=$(BUILD_DATE) $(if $(VERSION),--build-arg VERSION=$(VERSION))
IMAGE_TAG ?= local
IMAGE_PLATFORMS ?= linux/amd64,linux/arm64
GOPROXY ?= https://proxy.golang.org,direct
PUSH_LATEST ?= 0
BUILD_LDFLAGS = -X github.com/opensoha/soha/agent/internal/agent/buildinfo.Commit=$(SOURCE_COMMIT) -X github.com/opensoha/soha/agent/internal/agent/buildinfo.Date=$(BUILD_DATE) $(if $(VERSION),-X github.com/opensoha/soha/agent/internal/agent/buildinfo.Version=$(VERSION))
AGENT_IMAGE_REPOSITORY ?= ghcr.io/opensoha/soha-agent
HERMES_IMAGE_REPOSITORY ?= ghcr.io/opensoha/soha-hermes-agent

AGENT_IMAGE_TAGS = -t $(AGENT_IMAGE_REPOSITORY):$(IMAGE_TAG)
HERMES_IMAGE_TAGS = -t $(HERMES_IMAGE_REPOSITORY):$(IMAGE_TAG)
ifeq ($(PUSH_LATEST),1)
AGENT_IMAGE_TAGS += -t $(AGENT_IMAGE_REPOSITORY):latest
HERMES_IMAGE_TAGS += -t $(HERMES_IMAGE_REPOSITORY):latest
endif

.PHONY: help build deploy-agent-image deploy-hermes-image deploy-images deploy-agent-image-push deploy-hermes-image-push deploy-images-push
.PHONY: test vet test-declared verify-images

test: ## Run isolated Go tests with local capacity checks.
	GOWORK=$(SOURCE_GOWORK) $(LOCAL_VERIFY) go test ./...

vet: ## Run Go vet with local capacity checks.
	GOWORK=$(SOURCE_GOWORK) $(LOCAL_VERIFY) go vet ./...

verify-images: ## Validate all runtime images and remove this run's tags.
	opensoha-verify image -- --build-arg GOPROXY=$(GOPROXY) $(IMAGE_METADATA) -f $(COMPONENT_DIR)/deploy/Dockerfile $(SOURCE_ROOT)
	opensoha-verify image -- --build-arg GOPROXY=$(GOPROXY) $(IMAGE_METADATA) -f $(COMPONENT_DIR)/deploy/Dockerfile --target buildpacks-runtime $(SOURCE_ROOT)
	opensoha-verify image -- --build-arg GOPROXY=$(GOPROXY) $(IMAGE_METADATA) -f $(COMPONENT_DIR)/deploy/Dockerfile --target buildpacks-daemonless-runtime $(SOURCE_ROOT)
	opensoha-verify image -- --build-arg GOPROXY=$(GOPROXY) $(IMAGE_METADATA) -f $(COMPONENT_DIR)/deploy/Dockerfile.hermes-agent-runner $(SOURCE_ROOT)
	opensoha-verify image -- --build-arg GOPROXY=$(GOPROXY) $(IMAGE_METADATA) -f $(COMPONENT_DIR)/deploy/Dockerfile --target volume-reader-runtime $(SOURCE_ROOT)
	opensoha-verify image -- $(IMAGE_METADATA) -f $(COMPONENT_DIR)/deploy/Dockerfile.dsh-runtime $(SOURCE_ROOT)

test-declared: ## Check the declared external SDK separately from Root source mode.
	GOWORK=off $(LOCAL_VERIFY) go test ./...

help:
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-28s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the soha-agent binary.
	GOWORK=$(SOURCE_GOWORK) CGO_ENABLED=0 $(LOCAL_VERIFY) go build -trimpath -ldflags="$(BUILD_LDFLAGS)" -o "$(BUILD_OUTPUT)" ./cmd/agent

deploy-agent-image: ## Build the generic soha-agent image.
	$(LOCAL_DOCKER_BUILD) --build-arg GOPROXY=$(GOPROXY) $(IMAGE_METADATA) -f $(COMPONENT_DIR)/deploy/Dockerfile $(AGENT_IMAGE_TAGS) $(SOURCE_ROOT)

deploy-hermes-image: ## Build the Hermes runner image.
	$(LOCAL_DOCKER_BUILD) --build-arg GOPROXY=$(GOPROXY) $(IMAGE_METADATA) -f $(COMPONENT_DIR)/deploy/Dockerfile.hermes-agent-runner $(HERMES_IMAGE_TAGS) $(SOURCE_ROOT)

deploy-images: deploy-agent-image deploy-hermes-image ## Build both agent images.

deploy-agent-image-push: ## Build and push the generic soha-agent image.
	@test "$(IMAGE_TAG)" != "local" || (echo "Set IMAGE_TAG to a release version before pushing." >&2; exit 1)
	docker buildx build --platform $(IMAGE_PLATFORMS) --build-arg GOPROXY=$(GOPROXY) $(IMAGE_METADATA) -f $(COMPONENT_DIR)/deploy/Dockerfile $(AGENT_IMAGE_TAGS) --push $(SOURCE_ROOT)

deploy-hermes-image-push: ## Build and push the Hermes runner image.
	@test "$(IMAGE_TAG)" != "local" || (echo "Set IMAGE_TAG to a release version before pushing." >&2; exit 1)
	docker buildx build --platform $(IMAGE_PLATFORMS) --build-arg GOPROXY=$(GOPROXY) $(IMAGE_METADATA) -f $(COMPONENT_DIR)/deploy/Dockerfile.hermes-agent-runner $(HERMES_IMAGE_TAGS) --push $(SOURCE_ROOT)

deploy-images-push: deploy-agent-image-push deploy-hermes-image-push ## Build and push both agent images.

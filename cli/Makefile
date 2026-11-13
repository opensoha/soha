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

IMAGE_REPOSITORY ?= ghcr.io/opensoha/soha-cli
IMAGE_TAG ?= local
IMAGE_PLATFORMS ?= linux/amd64,linux/arm64
GOPROXY ?= https://proxy.golang.org,direct
PUSH_LATEST ?= 0

IMAGE_BUILD_TAGS = -t $(IMAGE_REPOSITORY):$(IMAGE_TAG)
ifeq ($(PUSH_LATEST),1)
IMAGE_BUILD_TAGS += -t $(IMAGE_REPOSITORY):latest
endif

.PHONY: help build deploy-image deploy-image-push
.PHONY: test vet verify-image

test: ## Run isolated Go tests with local capacity checks.
	GOWORK=off $(LOCAL_VERIFY) go test ./...

vet: ## Run Go vet with local capacity checks.
	GOWORK=off $(LOCAL_VERIFY) go vet ./...

verify-image: ## Validate the tool image and remove this run's tag.
	opensoha-verify image -- --build-arg GOPROXY=$(GOPROXY) -f Dockerfile .

help:
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-28s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the soha CLI binary.
	CGO_ENABLED=0 $(LOCAL_VERIFY) go build -o bin/soha ./cmd/soha

deploy-image: ## Build the soha-cli tool image.
	$(LOCAL_DOCKER_BUILD) --build-arg GOPROXY=$(GOPROXY) -f Dockerfile $(IMAGE_BUILD_TAGS) .

deploy-image-push: ## Build and push the multi-arch soha-cli tool image.
	@test "$(IMAGE_TAG)" != "local" || (echo "Set IMAGE_TAG to a release version before pushing." >&2; exit 1)
	docker buildx build --platform $(IMAGE_PLATFORMS) --build-arg GOPROXY=$(GOPROXY) -f Dockerfile $(IMAGE_BUILD_TAGS) --push .

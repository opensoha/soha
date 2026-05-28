.DEFAULT_GOAL := dev

COMPOSE ?= docker compose
DEPLOY_DIR ?= deploy
ROOT_COMPOSE_FILE ?= $(DEPLOY_DIR)/docker-compose.yaml
APP_DOCKERFILE ?= $(DEPLOY_DIR)/Dockerfile
HELM_CHART ?= $(DEPLOY_DIR)/chart
KUBEVIRT_COMPOSE_FILE ?= configs/k3s/docker-compose.kubevirt.yaml
PVE_DOCKER_COMPOSE_FILE ?= configs/proxmox/docker-compose.pve.yaml
KUBECTL ?= kubectl
KUBECONFIG ?= .dev/k3s/kubeconfig.yaml
KUBEVIRT_VERSION ?= stable
CDI_VERSION ?= latest
PVE_VM_MANIFEST ?= configs/kubevirt/pve-vm.yaml
PVE_MOCK_MANIFEST ?= configs/kubevirt/pve-mock.yaml
PVE_DOCKER_IMAGE ?= ghcr.io/longqt-sea/proxmox-ve:latest
PVE_DOCKER_SERVICE ?= pve-docker
PVE_DOCKER_PASSWORD ?= soha
PVE_DOCKER_UI_PORT ?= 8006
PVE_DOCKER_SSH_PORT ?= 2222
PVE_DOCKER_SPICE_PORT ?= 3128
PVE_DOCKER_URL ?= https://127.0.0.1:$(PVE_DOCKER_UI_PORT)

ROOT_COMPOSE = $(COMPOSE) -f $(ROOT_COMPOSE_FILE)
KUBEVIRT_COMPOSE = $(COMPOSE) -f $(ROOT_COMPOSE_FILE) -f $(KUBEVIRT_COMPOSE_FILE)
PVE_DOCKER_COMPOSE = PVE_DOCKER_IMAGE="$(PVE_DOCKER_IMAGE)" PVE_DOCKER_PASSWORD="$(PVE_DOCKER_PASSWORD)" PVE_DOCKER_UI_PORT="$(PVE_DOCKER_UI_PORT)" PVE_DOCKER_SSH_PORT="$(PVE_DOCKER_SSH_PORT)" PVE_DOCKER_SPICE_PORT="$(PVE_DOCKER_SPICE_PORT)" $(COMPOSE) -f $(PVE_DOCKER_COMPOSE_FILE)

.PHONY: help init init-go init-web init-docs init-db init-cluster init-cluster-kubevirt init-kubevirt init-cdi init-kubevirt-lab init-virtualization-lab init-pve-vm deploy-pve-vm delete-pve-vm deploy-pve-mock delete-pve-mock fix-kubevirt-mounts enable-kubevirt-emulation pve-vm-boot-root pve-vm-status pve-docker-up pve-docker-down pve-docker-restart pve-docker-logs pve-docker-shell pve-docker-status pve-docker-config dev dev-api dev-web dev-docs build build-web build-docs clean deploy-image deploy-compose-up deploy-compose-down deploy-compose-config deploy-helm-lint deploy-hermes-setup deploy-hermes-runner-up deploy-hermes-runner-down deploy-hermes-runner-config test-api test-web

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

init-cluster-kubevirt: ## Start local k3s with KubeVirt-friendly device and mount settings.
	@mkdir -p .dev/k3s
	@echo "Starting local K3s with KubeVirt host device exposure..."
	@$(KUBEVIRT_COMPOSE) up -d k3s
	@printf "Waiting for K3s"; \
	until $(KUBEVIRT_COMPOSE) exec -T k3s kubectl get nodes >/dev/null 2>&1; do \
		printf "."; \
		sleep 2; \
	done; \
	printf " done\n"
	@$(KUBEVIRT_COMPOSE) exec -T k3s mount --make-rshared /
	@$(KUBEVIRT_COMPOSE) cp k3s:/etc/rancher/k3s/k3s.yaml .dev/k3s/kubeconfig.yaml >/dev/null
	@perl -0pi -e 's#server: https://[^\n]+#server: https://127.0.0.1:6443#' .dev/k3s/kubeconfig.yaml
	@chmod 644 .dev/k3s/kubeconfig.yaml
	@echo "K3s kubeconfig written to .dev/k3s/kubeconfig.yaml"

init-kubevirt: ## Install KubeVirt into the current KUBECONFIG cluster.
	@version="$(KUBEVIRT_VERSION)"; \
	if [ "$$version" = "stable" ]; then \
		version="$$(curl -fsSL https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)"; \
	fi; \
	echo "Installing KubeVirt $$version..."; \
	KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) apply -f "https://github.com/kubevirt/kubevirt/releases/download/$$version/kubevirt-operator.yaml"; \
	KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) apply -f "https://github.com/kubevirt/kubevirt/releases/download/$$version/kubevirt-cr.yaml"; \
	KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n kubevirt wait kv kubevirt --for condition=Available --timeout=600s

init-cdi: ## Install CDI into the current KUBECONFIG cluster.
	@base="https://github.com/kubevirt/containerized-data-importer/releases/latest/download"; \
	if [ "$(CDI_VERSION)" != "latest" ]; then \
		base="https://github.com/kubevirt/containerized-data-importer/releases/download/$(CDI_VERSION)"; \
	fi; \
	echo "Installing CDI $(CDI_VERSION)..."; \
	KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) apply -f "$$base/cdi-operator.yaml"; \
	KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) apply -f "$$base/cdi-cr.yaml"; \
	KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n cdi wait cdi cdi --for condition=Available --timeout=600s

init-kubevirt-lab: init-cluster-kubevirt init-kubevirt init-cdi ## Start local k3s and install KubeVirt plus CDI.

init-virtualization-lab: init-kubevirt-lab pve-docker-up ## Start KubeVirt lab and Docker-based PVE lab.

deploy-pve-vm: ## Deploy an installer-backed Proxmox VE VM into KubeVirt.
	@echo "Deploying Proxmox VE lab VM into KubeVirt..."
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) apply -f $(PVE_VM_MANIFEST)
	@echo "PVE API will be reachable from the host at https://127.0.0.1:8006 after the installer finishes and PVE boots."
	@echo "If soha runs in the compose app container, use endpoint https://k3s:30006 instead."
	@echo "Use: KUBECONFIG=$(KUBECONFIG) virtctl -n virt-lab vnc pve-lab"

init-pve-vm: init-kubevirt-lab deploy-pve-vm ## Start KubeVirt lab and deploy the PVE installer VM.

pve-vm-boot-root: ## Switch the KubeVirt PVE VM boot order from ISO to root disk.
	@echo "Switching PVE VM boot order from installer ISO to root disk..."
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n virt-lab patch vm pve-lab --type=merge -p '{"spec":{"template":{"spec":{"domain":{"devices":{"disks":[{"name":"rootdisk","disk":{"bus":"virtio"},"bootOrder":1},{"name":"installercdrom","cdrom":{"bus":"sata","readonly":true}}]}}}}}}'
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n virt-lab delete vmi pve-lab --ignore-not-found=true

pve-vm-status: ## Show KubeVirt PVE VM lab resources.
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n virt-lab get vm,vmi,dv,pvc,svc

delete-pve-vm: ## Delete the KubeVirt PVE VM lab resources.
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) delete -f $(PVE_VM_MANIFEST) --ignore-not-found=true

deploy-pve-mock: ## Deploy the lightweight PVE API mock into local k3s.
	@echo "Deploying PVE API mock into local k3s..."
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n virt-lab delete vm pve-lab --ignore-not-found=true
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n virt-lab delete vmi pve-lab --ignore-not-found=true
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n virt-lab delete dv pve-installer-iso pve-rootdisk --ignore-not-found=true
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n virt-lab delete pvc pve-installer-iso pve-rootdisk --ignore-not-found=true
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n virt-lab delete svc pve-lab --ignore-not-found=true
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) apply -f $(PVE_MOCK_MANIFEST)
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n virt-lab rollout status deployment/pve-mock --timeout=180s
	@echo "PVE mock endpoint for host-run soha: http://127.0.0.1:8006"
	@echo "PVE mock endpoint for compose-run soha: http://k3s:30006"

delete-pve-mock: ## Delete the lightweight PVE API mock.
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) delete -f $(PVE_MOCK_MANIFEST) --ignore-not-found=true

pve-docker-up: ## Start a Docker-based Proxmox VE lab container.
	@$(PVE_DOCKER_COMPOSE) up -d
	@$(MAKE) pve-docker-status

pve-docker-down: ## Stop and remove the Docker-based PVE lab container.
	@$(PVE_DOCKER_COMPOSE) down

pve-docker-restart: ## Restart the Docker-based PVE lab container.
	@$(PVE_DOCKER_COMPOSE) restart $(PVE_DOCKER_SERVICE)

pve-docker-logs: ## Follow Docker-based PVE lab logs.
	@$(PVE_DOCKER_COMPOSE) logs -f $(PVE_DOCKER_SERVICE)

pve-docker-shell: ## Open a shell inside the Docker-based PVE lab container.
	@$(PVE_DOCKER_COMPOSE) exec $(PVE_DOCKER_SERVICE) bash

pve-docker-status: ## Show Docker-based PVE lab status and endpoints.
	@$(PVE_DOCKER_COMPOSE) ps
	@echo "PVE Web UI: $(PVE_DOCKER_URL)"
	@echo "PVE SSH: 127.0.0.1:$(PVE_DOCKER_SSH_PORT)"
	@echo "Default root password: $(PVE_DOCKER_PASSWORD)"
	@echo "From compose-run soha, use endpoint https://host.docker.internal:$(PVE_DOCKER_UI_PORT)"

pve-docker-config: ## Render the Docker-based PVE lab compose config.
	@$(PVE_DOCKER_COMPOSE) config

fix-kubevirt-mounts: ## Fix shared mount propagation for local KubeVirt-on-k3s labs.
	@echo "Marking the local k3s root mount as rshared for KubeVirt..."
	@$(KUBEVIRT_COMPOSE) exec -T k3s mount --make-rshared /
	@echo "Restarting virt-handler pods..."
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n kubevirt delete pod -l kubevirt.io=virt-handler --ignore-not-found=true

enable-kubevirt-emulation: ## Enable KubeVirt software emulation on lab hosts without KVM.
	@echo "Enabling KubeVirt software emulation for lab hosts without /dev/kvm..."
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n kubevirt patch kubevirt kubevirt --type=merge -p '{"spec":{"configuration":{"developerConfiguration":{"useEmulation":true}}}}'
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n kubevirt delete pod -l kubevirt.io=virt-handler --ignore-not-found=true

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

deploy-hermes-setup: ## Run one-time Hermes Agent Runtime setup.
	$(ROOT_COMPOSE) --profile hermes-setup run --rm hermes-agent-setup

deploy-hermes-runner-up: ## Start the Hermes Agent Runtime runner.
	$(ROOT_COMPOSE) --profile hermes up -d --build hermes-agent-runner

deploy-hermes-runner-down: ## Stop and remove the Hermes Agent Runtime runner.
	$(ROOT_COMPOSE) --profile hermes stop hermes-agent-runner
	$(ROOT_COMPOSE) --profile hermes rm -f hermes-agent-runner

deploy-hermes-runner-config: ## Render Hermes runner compose config.
	$(ROOT_COMPOSE) --profile hermes --profile hermes-setup config

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

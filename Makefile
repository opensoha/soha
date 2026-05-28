.DEFAULT_GOAL := dev

.PHONY: init init-go init-web init-docs init-db init-cluster init-cluster-kubevirt init-kubevirt init-cdi init-pve-vm deploy-pve-vm delete-pve-vm deploy-pve-mock delete-pve-mock fix-kubevirt-mounts enable-kubevirt-emulation pve-vm-boot-root pve-vm-status dev dev-api dev-web dev-docs build build-web build-docs clean deploy-image deploy-compose-up deploy-compose-down deploy-compose-config deploy-helm-lint

COMPOSE ?= docker compose
ROOT_COMPOSE_FILE ?= docker-compose.yaml
KUBEVIRT_COMPOSE_FILE ?= configs/k3s/docker-compose.kubevirt.yaml
KUBECTL ?= kubectl
KUBECONFIG ?= .dev/k3s/kubeconfig.yaml
KUBEVIRT_VERSION ?= stable
CDI_VERSION ?= latest
PVE_VM_MANIFEST ?= configs/kubevirt/pve-vm.yaml
PVE_MOCK_MANIFEST ?= configs/kubevirt/pve-mock.yaml

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
	until $(COMPOSE) -f $(ROOT_COMPOSE_FILE) exec -T postgres pg_isready -U pgsql -d soha >/dev/null 2>&1; do \
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

init-cluster-kubevirt:
	@mkdir -p .dev/k3s
	@echo "Starting local K3s with KubeVirt host device exposure..."
	@$(COMPOSE) -f $(ROOT_COMPOSE_FILE) -f $(KUBEVIRT_COMPOSE_FILE) up -d k3s
	@printf "Waiting for K3s"; \
	until $(COMPOSE) -f $(ROOT_COMPOSE_FILE) -f $(KUBEVIRT_COMPOSE_FILE) exec -T k3s kubectl get nodes >/dev/null 2>&1; do \
		printf "."; \
		sleep 2; \
	done; \
	printf " done\n"
	@$(COMPOSE) -f $(ROOT_COMPOSE_FILE) -f $(KUBEVIRT_COMPOSE_FILE) exec -T k3s mount --make-rshared /
	@$(COMPOSE) -f $(ROOT_COMPOSE_FILE) -f $(KUBEVIRT_COMPOSE_FILE) cp k3s:/etc/rancher/k3s/k3s.yaml .dev/k3s/kubeconfig.yaml >/dev/null
	@perl -0pi -e 's#server: https://[^\n]+#server: https://127.0.0.1:6443#' .dev/k3s/kubeconfig.yaml
	@chmod 644 .dev/k3s/kubeconfig.yaml
	@echo "K3s kubeconfig written to .dev/k3s/kubeconfig.yaml"

init-kubevirt:
	@version="$(KUBEVIRT_VERSION)"; \
	if [ "$$version" = "stable" ]; then \
		version="$$(curl -fsSL https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)"; \
	fi; \
	echo "Installing KubeVirt $$version..."; \
	KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) apply -f "https://github.com/kubevirt/kubevirt/releases/download/$$version/kubevirt-operator.yaml"; \
	KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) apply -f "https://github.com/kubevirt/kubevirt/releases/download/$$version/kubevirt-cr.yaml"; \
	KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n kubevirt wait kv kubevirt --for condition=Available --timeout=600s

init-cdi:
	@base="https://github.com/kubevirt/containerized-data-importer/releases/latest/download"; \
	if [ "$(CDI_VERSION)" != "latest" ]; then \
		base="https://github.com/kubevirt/containerized-data-importer/releases/download/$(CDI_VERSION)"; \
	fi; \
	echo "Installing CDI $(CDI_VERSION)..."; \
	KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) apply -f "$$base/cdi-operator.yaml"; \
	KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) apply -f "$$base/cdi-cr.yaml"; \
	KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n cdi wait cdi cdi --for condition=Available --timeout=600s

deploy-pve-vm:
	@echo "Deploying Proxmox VE lab VM into KubeVirt..."
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) apply -f $(PVE_VM_MANIFEST)
	@echo "PVE API will be reachable from the host at https://127.0.0.1:8006 after the installer finishes and PVE boots."
	@echo "If soha runs in the compose app container, use endpoint https://k3s:30006 instead."
	@echo "Use: KUBECONFIG=$(KUBECONFIG) virtctl -n virt-lab vnc pve-lab"

init-pve-vm: init-cluster-kubevirt init-kubevirt init-cdi deploy-pve-vm

pve-vm-boot-root:
	@echo "Switching PVE VM boot order from installer ISO to root disk..."
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n virt-lab patch vm pve-lab --type=merge -p '{"spec":{"template":{"spec":{"domain":{"devices":{"disks":[{"name":"rootdisk","disk":{"bus":"virtio"},"bootOrder":1},{"name":"installercdrom","cdrom":{"bus":"sata","readonly":true}}]}}}}}}'
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n virt-lab delete vmi pve-lab --ignore-not-found=true

pve-vm-status:
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n virt-lab get vm,vmi,dv,pvc,svc

delete-pve-vm:
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) delete -f $(PVE_VM_MANIFEST) --ignore-not-found=true

deploy-pve-mock:
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

delete-pve-mock:
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) delete -f $(PVE_MOCK_MANIFEST) --ignore-not-found=true

fix-kubevirt-mounts:
	@echo "Marking the local k3s root mount as rshared for KubeVirt..."
	@$(COMPOSE) -f $(ROOT_COMPOSE_FILE) -f $(KUBEVIRT_COMPOSE_FILE) exec -T k3s mount --make-rshared /
	@echo "Restarting virt-handler pods..."
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n kubevirt delete pod -l kubevirt.io=virt-handler --ignore-not-found=true

enable-kubevirt-emulation:
	@echo "Enabling KubeVirt software emulation for lab hosts without /dev/kvm..."
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n kubevirt patch kubevirt kubevirt --type=merge -p '{"spec":{"configuration":{"developerConfiguration":{"useEmulation":true}}}}'
	@KUBECONFIG="$(KUBECONFIG)" $(KUBECTL) -n kubevirt delete pod -l kubevirt.io=virt-handler --ignore-not-found=true

dev-api:
	go run ./cmd/server

dev-web:
	cd web && npm run dev

dev-docs:
	cd docs && npm run dev

dev: init-db init-cluster
	@echo "Starting api and web..."
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
	CGO_ENABLED=0 go build -tags embedassets -o bin/soha ./cmd/server

deploy-image:
	docker build -t soha:single-project .

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

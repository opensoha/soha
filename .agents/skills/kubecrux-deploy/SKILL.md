---
name: kubecrux-deploy
description: >-
  Prepare kubecrux single-project deployment artifacts across container build,
  Docker Compose, raw Kubernetes YAML, and Helm. Use when packaging local,
  demo, or small-environment deployments; creating or updating Dockerfiles;
  wiring backend config through files or Secrets; or changing service
  exposure, ingress, image tags, and embedded frontend or docs delivery. This
  skill assumes the current repo can ship as one application container because
  `cmd/server` serves embedded SPA and docs assets when `web/dist` and
  `docs/build` are present at build time, and the canonical deployment assets
  live under the repo-root `deploy/` directory.
---

# Kubecrux Deploy

## Overview

Use the repo deployment assets under `deploy/` to run kubecrux as a single-project runtime: one application container serving API, SPA, and docs, plus PostgreSQL as the required durable dependency. This matches the current codebase better than deploying the Vite dev server separately.

## Workflow

1. Confirm the target is a single-project deployment, not a multi-service platform split.
2. Start from `deploy/docker/Dockerfile.single-project` so `web/dist` and `docs/build` are baked into the server binary during image build.
3. Copy and adapt `deploy/config/config.api.single-project.yaml` for the target environment.
4. Choose one delivery form:
   - `deploy/compose/docker-compose.single-project.yml` for local or VM-style runs
   - `deploy/k8s/kubecrux-single-project.yaml` for raw-cluster delivery
   - `deploy/helm/kubecrux/` for repeatable cluster installs
5. Verify `KC_CONFIG_FILE`, database settings, OIDC redirect URLs, and ingress hostnames before rollout.
6. Smoke test `/healthz`, `/readyz`, `/`, and `/docs/` after deployment.

## Non-Negotiables

- Do not deploy the Vite dev server for production-like installs.
- Prefer the embedded single-container runtime unless the user explicitly wants API and web split apart.
- Keep app config file-driven. The server expects `KC_CONFIG_FILE` or the default config path.
- Treat PostgreSQL as required for this starter deployment set.
- Keep real credentials out of committed plain-text manifests. Replace the example values with Secrets, sealed secrets, or an external secret manager before real use.
- If the deployment needs direct-cluster access, provide kubeconfig or cluster registration data explicitly. The starter assets do not magically register Kubernetes clusters.

## Deployment Map

- `deploy/docker/Dockerfile.single-project`: multi-stage image build for embedded SPA and docs.
- `deploy/config/config.api.single-project.yaml`: starter backend config for same-origin single-project installs.
- `deploy/compose/docker-compose.single-project.yml`: local stack with app plus PostgreSQL.
- `deploy/k8s/kubecrux-single-project.yaml`: raw Kubernetes manifest for namespace, Secret, app, PostgreSQL, Service, and Ingress.
- `deploy/helm/kubecrux/`: Helm chart for the same topology.

## Read These References When Needed

- `references/deployment-modes.md`: when to choose compose, raw YAML, or Helm, plus the embedded-runtime rationale.
- `references/runtime-checklist.md`: rollout checklist, image-build expectations, and smoke-test prompts.

## Done Criteria

- The selected deployment asset matches the intended environment.
- App config, ingress hostnames, and auth callback URLs are coherent.
- The deployment serves the console and API from one origin unless the user asked for a split topology.
- Health endpoints and initial page load are verified after render or rollout.

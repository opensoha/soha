---
name: soha-deploy
description: >-
  Prepare soha single-project deployment artifacts across container build,
  Docker Compose, raw Kubernetes YAML, and Helm. Use when packaging local,
  demo, or small-environment deployments; creating or updating Dockerfiles;
  wiring backend config through files or Secrets; or changing service
  exposure, ingress, image tags, and embedded frontend or docs delivery. This
  skill assumes the current repo can ship as one application container because
  `cmd/server` serves embedded SPA and docs assets when `web/dist` and
  `docs/build` are present at build time, and the canonical deployment assets
  now live under `deploy/`.
---

# Soha Deploy

## Overview

Use the `deploy/` assets to run soha as a single-project runtime: one application container serving API, SPA, and docs, plus PostgreSQL as the required durable dependency. This matches the current codebase better than deploying the Vite dev server separately.

## Workflow

1. Confirm the target is a single-project deployment, not a multi-service platform split.
2. Start from `deploy/Dockerfile` so `web/dist` and `docs/build` are baked into the server binary during image build.
3. Reuse `configs/config.yaml` for local and container defaults, or provide overrides through environment variables or mounted config files when deployment needs differ.
4. Choose one delivery form:
   - `deploy/docker-compose.yaml` for local or VM-style runs
   - `deploy/deployment.yaml` for raw-cluster delivery
   - `deploy/chart/` for repeatable cluster installs
5. Verify `SOHA_CONFIG_FILE`, database settings, OIDC redirect URLs, and ingress hostnames before rollout.
6. Smoke test `/healthz`, `/readyz`, `/`, and `/docs/` after deployment.

## Non-Negotiables

- Do not deploy the Vite dev server for production-like installs.
- Prefer the embedded single-container runtime unless the user explicitly wants API and web split apart.
- Keep app config file-driven. The server expects `SOHA_CONFIG_FILE` or the default config path.
- Treat PostgreSQL as required for this starter deployment set.
- Keep real credentials out of committed plain-text manifests. Replace the example values with Secrets, sealed secrets, or an external secret manager before real use.
- If the deployment needs direct-cluster access, provide kubeconfig or cluster registration data explicitly. The starter assets do not magically register Kubernetes clusters.

## Deployment Map

- `deploy/Dockerfile`: multi-stage image build for embedded SPA and docs.
- `deploy/Dockerfile.hermes-agent-runner`: optional Hermes Agent Runtime runner image.
- `deploy/docker-compose.yaml`: local stack with app plus PostgreSQL, optional local `k3s`, and optional Hermes runner services.
- `configs/config.yaml`: default backend config for local and container startup.
- `deploy/deployment.yaml`: raw Kubernetes manifest for namespace, Secret, app, PostgreSQL, Service, and Ingress.
- `deploy/chart/`: Helm chart for the same topology.

## Repo Reality Checks

- Keep the single-project deployment story coherent across `deploy/Dockerfile`, `deploy/docker-compose.yaml`, `deploy/deployment.yaml`, and `deploy/chart/`.
- If `deploy/docker-compose.yaml` also carries local cluster helpers such as `k3s` or optional Hermes runner services, preserve them unless the user explicitly wants a simpler compose file.
- When changing deployment docs or examples, update `README.md`, `README-cn.md`, `docs/operations/deployment.md`, and `docs/en/operations/deployment.md` together.
- When auth callback URLs or external login provider assumptions change, re-check deployment-facing hostname, ingress, and `SOHA_CONFIG_FILE` examples so redirect URLs stay coherent.

## Read These References When Needed

- `references/deployment-modes.md`: when to choose compose, raw YAML, or Helm, plus the embedded-runtime rationale.
- `references/runtime-checklist.md`: rollout checklist, image-build expectations, and smoke-test prompts.

## Done Criteria

- The selected deployment asset matches the intended environment.
- App config, ingress hostnames, and auth callback URLs are coherent.
- The deployment serves the console and API from one origin unless the user asked for a split topology.
- Health endpoints and initial page load are verified after render or rollout.

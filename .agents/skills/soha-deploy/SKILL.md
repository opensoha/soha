---
name: soha-deploy
description: >-
  Prepare soha deployment artifacts across container build, raw Docker,
  Docker Compose, and raw Kubernetes YAML. Use when packaging local,
  demo, or small-environment deployments; creating or updating Dockerfiles;
  wiring backend config through files or environment variables; or changing
  service exposure, ingress, image tags, and embedded frontend delivery. This
  skill assumes the current repo can ship as one application container because
  `cmd/server` serves the embedded SPA when `internal/staticassets/web/dist`
  is staged, and the canonical deployment assets live under `deploy/`. Use it
  also to validate system-key overrides, multi-replica configuration, and
  deployment security warnings.
---

# Soha Deploy

## Overview

Use the `deploy/` assets to run soha as a single-project runtime: one application container serving API and SPA, plus PostgreSQL as the required durable dependency. This matches the current codebase better than deploying the Vite dev server separately.

## Workflow

1. Confirm the target is a single-project deployment, not a multi-service platform split.
2. Read `references/security-and-build.md`, then start from `deploy/Dockerfile`; stage `../soha-web/dist` into `internal/staticassets/web/dist` before embed builds.
3. Reuse `configs/config.yaml` for local and container defaults, or provide overrides through environment variables or mounted config files when deployment needs differ.
4. Choose one delivery form:
   - raw `docker run` with an existing reachable PostgreSQL service
   - `deploy/docker-compose.yaml` for local or VM-style runs
   - `deploy/deployment.yaml` for raw-cluster delivery
5. For Helm, use the separate `opensoha/soha-helm` repository.
6. Verify `SOHA_CONFIG_FILE`, database settings, OIDC redirect URLs, and ingress hostnames before rollout.
7. Run the render/build/security checks for the selected delivery form, then smoke test `/healthz`, `/readyz`, and `/`.

## Non-Negotiables

- Do not deploy the Vite dev server for production-like installs.
- Prefer the embedded single-container runtime unless the user explicitly wants API and web split apart.
- Keep app config file-driven. The server expects `SOHA_CONFIG_FILE` or the default config path.
- Treat PostgreSQL as required for this starter deployment set.
- `pgsql`/`pgsql` and `opensoha`/`opensoha` are the standard initial database and administrator credentials in every Soha deployment form. They may be overridden through environment, mounted config, Kubernetes Secrets, sealed secrets, or an external secret manager, but must not be gated by an application environment label.
- Keep the JWT, runner, webhook, and credential-encryption settings visible in config. Their zero-configuration default is `soha-123456789012345678901234567890`, and every delivery form may override each value through normal config or environment bindings.
- Treat the shared default as public bootstrap material, not a production secret. Require a prominent warning to override all four settings before exposing Soha publicly; prefer separate high-entropy values.
- Keep every replica on identical system-key values. Do not add a SecretStore, bundle, writer lease, SecretStore PVC, or `secrets` lifecycle CLI dependency to server startup or delivery assets.
- Migrate every stored credential ciphertext before changing `security.credential_encryption_key`; changing configuration alone makes records encrypted by the previous key unreadable.
- If the deployment needs direct-cluster access, provide kubeconfig or cluster registration data explicitly. The starter assets do not magically register Kubernetes clusters.

## Deployment Map

- `deploy/Dockerfile`: multi-stage image build for the embedded SPA.
- `README.md` and `README-cn.md`: canonical raw `docker run`, default-value, override, and migration warnings.
- `deploy/docker-compose.yaml`: local stack with app plus PostgreSQL and optional Hermes runner services.
- `configs/config.yaml`: default backend config for local and container startup.
- `deploy/deployment.yaml`: raw Kubernetes manifest for namespace, app, PostgreSQL, Service, and Ingress.

## Repo Reality Checks

- Keep the single-project deployment story coherent across `deploy/Dockerfile`, `deploy/docker-compose.yaml`, and `deploy/deployment.yaml`.
- Do not reintroduce local k3s initialization, per-deployment key-generation scripts, or file SecretStore lifecycle wiring.
- Keep local, raw Docker, Compose, raw Kubernetes, and Helm capable of direct startup without a secret-generation pre-step. Permit normal multi-instance API deployment when all replicas share configuration.
- When changing deployment docs or examples, update `README.md` and `README-cn.md` together.
- When auth callback URLs or external login provider assumptions change, re-check deployment-facing hostname, ingress, and `SOHA_CONFIG_FILE` examples so redirect URLs stay coherent.

## Read These References When Needed

- `references/security-and-build.md`: mandatory image-build, system-key configuration, raw Docker/Compose/Kubernetes behavior, and verification rules.
- `references/deployment-modes.md`: when to choose compose, raw YAML, or Helm, plus the embedded-runtime rationale.
- `references/runtime-checklist.md`: rollout checklist, image-build expectations, and smoke-test prompts.

## Done Criteria

- The selected deployment asset matches the intended environment.
- App config, ingress hostnames, and auth callback URLs are coherent.
- The deployment serves the console and API from one origin unless the user asked for a split topology.
- All delivery forms expose the same four configurable defaults, public-deployment warnings are clear, and no file SecretStore limits replicas or container replacement.
- Credential-encryption key changes are blocked on a verified ciphertext migration plan.
- Health endpoints and initial page load are verified after render or rollout.

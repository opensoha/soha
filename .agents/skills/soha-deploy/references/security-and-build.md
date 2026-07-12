# Deployment Security and Build Standards

Apply this reference to Docker, Compose, raw Kubernetes, Kustomize, Helm-facing,
configuration, and release-image changes. Also read `$soha-backend` when the
binary, configuration loader, bootstrap, or credential-encryption behavior changes.

## Image Construction

- Build the frontend first and stage `../soha-web/dist` under `internal/staticassets/web/dist` before compiling with `-tags embedassets`.
- Build a static server with `CGO_ENABLED=0`. Keep `/app/soha` as the explicit runtime executable.
- Use the sibling `soha-contracts` repository only as the Docker build context expected by `deploy/Dockerfile`.
- Keep runtime images minimal. Do not ship the Vite server, Go toolchain, source tree, or build caches in the final stage.
- Treat `deploy/Dockerfile`, `deploy/docker-compose.yaml`, `deploy/deployment.yaml`, `deploy/kustomization.yaml`, config, and README commands as one compatibility surface.

## System-Key Contract

Keep these values explicit in `configs/config.yaml`:

```yaml
runtime:
  execution_runner_token: soha-123456789012345678901234567890
auth:
  jwt:
    secret: soha-123456789012345678901234567890
monitoring:
  webhook_token: soha-123456789012345678901234567890
security:
  credential_encryption_key: soha-123456789012345678901234567890
```

- Use this shared public value as the zero-configuration project default across local, raw Docker, Compose, raw Kubernetes, and Helm delivery.
- Permit each value to be overridden through config, environment variables, Kubernetes Secrets, sealed secrets, or an external secret manager. Do not reject the project default at startup.
- Warn prominently that unchanged public deployments permit token forgery and credential decryption. Require operators to override all four values before public exposure and prefer separate high-entropy values.
- Keep every API replica on identical configured values. Give runner consumers the exact execution-runner token configured on the control plane.
- Never log key values or place them in runtime status, audit details, operation payloads, or support bundles.
- Treat `pgsql`/`pgsql` and `opensoha`/`opensoha` as standard initial credentials that may also be overridden.

## Topology and Key Changes

- Start the server without a SecretStore path, generated bundle, writer lease, SecretStore volume, or SecretStore PVC.
- Permit raw Docker, Compose, raw Kubernetes, and Helm to start directly without an initialization script or secret lifecycle command.
- Permit multiple Soha API instances against the same database when every replica uses the same configuration. Do not force `Recreate` or a single replica because of key storage.
- Change JWT, runner, and webhook values through coordinated configuration rollout; expect existing sessions or consumers using an old value to stop authenticating.
- Before changing `security.credential_encryption_key`, migrate every managed ciphertext to the replacement key and verify it is decryptable. Never switch the configured key first: the old ciphertext will become unreadable.
- Do not add a `secrets` CLI, offline rotation procedure, or SecretStore backup flow to deployment documentation. Back up PostgreSQL according to the normal database recovery plan.

## Verification by Delivery Form

Always run the backend Go gate in `$soha-backend` when Go or module files change.
For deployment artifacts, add the applicable checks:

- Image: build the real Docker image and verify its configured entrypoint and normal server startup.
- Raw Docker: verify startup with the explicit environment example, container replacement without a secret volume, and two instances on different host ports against the same database.
- Compose: run `docker compose config`, start an isolated project, verify health, and clean temporary containers, networks, and volumes.
- Raw Kubernetes/Kustomize: parse all YAML documents, verify the four Secret keys and absence of a SecretStore PVC, then render Kustomize if its files changed.
- Helm: run lint and template for `soha`, `soha-agent`, and `soha-hermes-agent` when shared values or contracts change; verify all four configurable defaults and multi-replica rendering.
- Runtime: check `/healthz`, `/readyz`, `/`, login, PostgreSQL bootstrap, and runner authentication without printing secret values.

Never report deployment success from YAML parsing alone when the change affects
startup, persistence, image layout, authentication, or credential encryption.

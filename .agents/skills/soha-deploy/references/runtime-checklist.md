# Runtime Checklist

## Before Build

- Confirm `../soha-web/dist/index.html` is staged into `internal/staticassets/web/dist/index.html` before image builds.
- Confirm the repo has a valid backend config strategy for the target environment, whether that is `configs/config.yaml`, a mounted replacement, or environment overrides.
- Decide whether OIDC stays disabled or needs production callback URLs.

## Before Rollout

- Confirm whether the installation uses the standard `pgsql` and `opensoha` initial credentials or explicit overrides.
- Confirm the JWT, runner, webhook, and credential-encryption settings are present. The project default for all four is `soha-123456789012345678901234567890`; override every one before public exposure, preferably with separate high-entropy values.
- Confirm every Soha replica uses identical system-key values and every runner uses the control plane's configured runner token. Do not require a SecretStore volume, bundle, writer lease, or SecretStore PVC.
- Before changing the credential-encryption key on an existing installation, verify that all stored ciphertext has been migrated to the replacement key and remains decryptable.
- Confirm PostgreSQL persistence size and storage class expectations.
- Confirm the ingress host or external URL that the SPA will use.
- Confirm `SOHA_CONFIG_FILE` points at the mounted config file when deployment does not rely purely on env overrides.

## Smoke Tests

- `GET /healthz`
- `GET /readyz`
- load `/` and confirm the SPA shell renders
- log in with the intended auth mode
- confirm the app can talk to PostgreSQL and bootstrap menus, roles, and default data
- when replicas are enabled, confirm at least two API pods can become ready against the same database

## Common Adjustments

- For external PostgreSQL, override database config through env, mounted config, or platform Secrets before rollout.
- For stricter security, supply system-key overrides through your platform's Secret mechanism or external secret manager.
- For cluster access, mount kubeconfig or seed cluster connection metadata after startup.

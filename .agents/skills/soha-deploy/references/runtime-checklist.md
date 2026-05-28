# Runtime Checklist

## Before Build

- Confirm `web/package-lock.json` and `docs/package-lock.json` are present so the image build can use deterministic installs.
- Confirm the repo has a valid backend config strategy for the target environment, whether that is `configs/config.yaml`, a mounted replacement, or environment overrides.
- Decide whether OIDC stays disabled or needs production callback URLs.

## Before Rollout

- Replace placeholder passwords, JWT secrets, and webhook tokens.
- Confirm PostgreSQL persistence size and storage class expectations.
- Confirm the ingress host or external URL that the SPA will use.
- Confirm `SOHA_CONFIG_FILE` points at the mounted config file when deployment does not rely purely on env overrides.

## Smoke Tests

- `GET /healthz`
- `GET /readyz`
- load `/` and confirm the SPA shell renders
- load `/docs/` and confirm the docs site is reachable
- log in with the intended auth mode
- confirm the app can talk to PostgreSQL and bootstrap menus, roles, and default data

## Common Adjustments

- For external PostgreSQL, edit the config Secret or values before rollout.
- For stricter security, move example Secret material into your platform's secret manager.
- For cluster access, mount kubeconfig or seed cluster connection metadata after startup.

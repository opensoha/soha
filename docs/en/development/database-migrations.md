# Database Migrations

SQL migrations now use a driver-scoped layout under `migrations/<driver>/`.

- PostgreSQL bootstrap migration: `migrations/postgres/0001_init.sql`
- Legacy fallback file: `migrations/0001_init.sql`

## Rules

- migrations are append-only
- schema changes should be backward-compatible during rollout
- JSON is used for flexible policy and event payloads (portable baseline)

## Initial Migration

The bootstrap migration creates:

- identity tables
- access and policy tables
- cluster registry tables
- audit and event tables
- future build, deploy, notification, and preference tables

It does not seed the default login account. The bootstrap user is created by backend startup from `auth.dev_principal`, and the current repository baseline is `admin / kubecrux` only.

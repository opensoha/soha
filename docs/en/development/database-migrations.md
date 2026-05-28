# Database Migrations

SQL migrations now use a driver-scoped layout under `migrations/<driver>/`.

- Canonical PostgreSQL bootstrap migration: `migrations/postgres/0001_init.sql`

## Rules

- the current repository baseline is consolidated into one PostgreSQL bootstrap file
- fresh database initialization should succeed from `migrations/postgres/0001_init.sql` alone
- `schema_migrations` still tracks executed filenames, so future additive files may be reintroduced when needed
- schema changes should still remain backward-compatible during rollout

## Initial Migration

The bootstrap migration creates:

- identity tables
- access and policy tables
- cluster registry tables
- audit and event tables
- AI workbench tables
- delivery orchestration tables
- announcement receipt and port-forward tables
- future build, deploy, notification, and preference tables

It does not seed the default login account. The bootstrap user is created by backend startup from `auth.dev_principal`, and the current repository baseline is `admin / soha` only.

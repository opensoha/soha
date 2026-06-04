# Database Migrations

SQL migrations now use a driver-scoped layout under `migrations/<driver>/`.

- Canonical PostgreSQL bootstrap migration: `migrations/postgres/0001_init.sql`
- Incremental PostgreSQL migrations: `migrations/postgres/0002_*.sql` and later
- PostgreSQL compatibility baseline: 18.4

## Rules

- `0001_init.sql` owns the fresh-install schema baseline
- fresh database initialization should succeed by executing the full sorted `migrations/postgres/*.sql` history
- the bootstrap file and the full `migrations/postgres/*.sql` history should remain executable against PostgreSQL 18.4
- `schema_migrations` tracks executed filenames, so existing databases receive only new incremental files
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
- build, deploy, notification, and runtime support tables

It does not seed the default login account. The bootstrap user is created by backend startup from `auth.dev_principal`, and the current repository baseline is `admin / soha` only.

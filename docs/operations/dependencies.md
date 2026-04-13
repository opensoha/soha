# Dependencies

## PostgreSQL

PostgreSQL is the durable store for:

- users, teams, projects
- roles, policies, policy bindings
- clusters and credential metadata
- audit logs and operation logs
- event stream and future delivery records
- saved views and user preferences

## Redis

Redis is used for:

- session and token blacklist
- hot resource cache
- distributed locks
- event push buffer
- real-time subscription state
- temporary pagination and filter cache

# Database Schema

## PostgreSQL Tables

### users

Core identity profile.

Suggested JSONB fields:

- `tags`
- `preferences`

### teams

Organizational unit with optional JSONB metadata.

### projects

Project scope bound to teams and environments.

### roles

System and custom roles with JSONB capability descriptors.

### policies

ABAC policy definitions. JSONB fits:

- subject matcher
- target matcher
- conditions
- action list

### policy_bindings

Maps policies to users, teams, projects, or roles.

### clusters

Cluster registry metadata and health snapshot.

JSONB fits:

- labels
- capabilities
- health_snapshot

### cluster_credentials_meta

Credential metadata only, not raw secret material in phase 1.

JSONB fits:

- provider metadata
- auth plugin settings

### audit_logs

Append-only durable audit store.

JSONB fits:

- request_meta
- target_meta
- decision_meta

### operation_logs

Operational task records for mutating workflows.

### event_stream

Unified event envelope persistence.

JSONB fits:

- resource_ref
- payload
- correlation data

### build_records

Reserved for CI build history.

### deploy_records

Reserved for release and deploy history.

### notification_channels

Reserved for email, webhook, chat, or incident channel settings.

### saved_views

User or team saved filters, tables, and dashboards.

### user_preferences

Persistent UI preferences and defaults.

## PostgreSQL vs Redis

Use PostgreSQL when:

- durability matters
- relational search matters
- retention matters
- auditability matters

Use Redis when:

- data is short-lived
- low latency fanout matters
- cache invalidation can be event-driven
- distributed lock semantics are needed

# Audit Model

## Audit Categories

- login audit
- access audit
- operation audit
- permission deny audit
- high-risk action audit

## Audit Dimensions

Every audit record should capture:

- actor: user id, roles, teams
- request: path, method, source IP, user agent, request id
- target: cluster, namespace, resource kind, resource name
- action: view, list, update, delete, scale, restart, logs, exec
- result: allow, deny, success, failure
- summary: concise human-readable description
- metadata: JSONB for policy match, changed fields, integration context
- timestamp

## Storage Strategy

- PostgreSQL stores append-only records for retention and search
- short-lived event fanout is process-local and not the source of truth

## High-Risk Actions

Examples:

- delete workload in production
- exec into privileged pod
- scale stateful workloads in production
- change access policy
- modify cluster credentials

High-risk operations should include expanded metadata and optional approval state.

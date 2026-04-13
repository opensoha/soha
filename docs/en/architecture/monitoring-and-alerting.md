# Monitoring And Alerting

## Goal

kubecrux should become the unified alert ingress and routing plane for the platform.

The target model is:

1. alerts arrive from Prometheus Alertmanager, Grafana Alerting, or future third-party systems
2. kubecrux normalizes them into one internal alert envelope
3. platform policies decide ownership, routing, suppression, grouping, and escalation
4. kubecrux dispatches notifications to downstream channels

This keeps alert governance inside the platform instead of scattering logic across multiple tools.

## Current Implemented Surface

The repository now has a real monitoring ingress baseline, not just placeholders.

- inbound webhook: `POST /api/v1/integrations/alerts/webhook`
- summary API: `GET /api/v1/monitoring/summary`
- alert inventory API: `GET /api/v1/alerts`
- notification channel APIs:
  - `GET /api/v1/notification-channels`
  - `POST /api/v1/notification-channels`
  - `PUT /api/v1/notification-channels/:channelID`
- frontend pages:
  - `/observability/monitoring`
  - `/observability/alerts`
  - `/observability/notifications`
  - `/observability/oncall`
  - `/observability/events`
  - notification channels, routes, and silences are grouped under `/observability/notifications`

Current persistence behavior:

- normalized alerts are written to `alert_instances`
- notification channel definitions are written to `notification_channels`
- every accepted alert ingest also emits a normalized record into `event_stream`

This means the platform already owns alert ingestion and downstream channel registration, even though richer routing and fan-out work is still reserved for the next step.

## Recommended Modules

### Backend

- `internal/application/monitoring`
  - alert ingestion orchestration
  - alert normalization
  - silence, ack, assign, resolve workflow
- `internal/application/notification`
  - channel dispatch orchestration
  - retry and delivery status
- `internal/infrastructure/integration/observability`
  - Alertmanager adapter
  - Grafana Alerting adapter
  - Prometheus query adapter
- `internal/repository/alert`
  - alert instances and notification channels
- future: `internal/repository/alerts`
  - silences, routing rules, delivery logs

### Frontend

- `web/src/features/observability/monitoring-pages.tsx`
  - monitoring summary
  - alerts list
  - notification tabs
  - on-call placeholder
  - events feed
- future:
  - richer alert detail surface
  - explicit delivery-history workbench
  - extracted notification management components

## Data Model Direction

PostgreSQL should hold:

- alert_instances
- alert_rules_shadow
- alert_routes
- alert_silences
- alert_delivery_logs
- notification_channels

Redis should hold:

- short-lived dedup windows
- burst buffering for inbound alerts
- dispatch retry state
- active websocket/subscription state

## Integration Strategy

### Inbound

- Alertmanager webhook receiver
- Grafana webhook receiver
- future platform-native rule engine receiver

### Outbound

- email
- webhook
- enterprise chat bots
- incident systems

## Policy Direction

Alert routing should combine:

- cluster
- namespace
- project
- severity
- environment
- owner team
- maintenance window

Authorization should reuse existing RBAC + ABAC.

## Next Step Reserve

The next increment should add:

- route matching from `alert_routes`
- deeper silence and acknowledgement workflow
- delivery worker and retry state
- downstream fan-out logs in `alert_delivery_logs`
- ownership, assignment, and escalation views in the frontend

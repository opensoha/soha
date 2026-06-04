# Monitoring And Alerting

## Goal

soha should become the unified alert ingress and routing plane for the platform.

The target model is:

1. alerts arrive from Prometheus Alertmanager, Grafana Alerting, or future third-party systems
2. soha normalizes them into one internal alert envelope
3. platform policies decide ownership, routing, suppression, grouping, and escalation
4. soha dispatches notifications to downstream channels

This keeps alert governance inside the platform instead of scattering logic across multiple tools.

当前信息架构已经从旧的“告警中心”收敛成“监控工作台”：

- `/monitoring-workbench`
- `/monitoring-workbench/overview`
- `/monitoring-workbench/integrations`
- `/monitoring-workbench/alerts`
- `/monitoring-workbench/rules`
- `/monitoring-workbench/notifications`
- `/monitoring-workbench/healing`
- `/monitoring-workbench/oncall`
- `/monitoring-workbench/events`

旧的 `/observability/*` 路径仍保留兼容跳转。

## Current Implemented Surface

The repository now has a real monitoring ingress baseline, not just placeholders.

- inbound webhook: `POST /api/v1/integrations/alerts/webhook`
- registered integration webhook: `POST /api/v1/integrations/alerts/:integrationID/webhook`
- alert integration management APIs:
  - `GET /api/v1/alert-integrations`
  - `GET /api/v1/alert-integrations/:integrationID`
  - `POST /api/v1/alert-integrations`
  - `PUT /api/v1/alert-integrations/:integrationID`
  - `POST /api/v1/alert-integrations/test`
- summary API: `GET /api/v1/monitoring/summary`
- alert inventory API: `GET /api/v1/alerts`
- alert governance APIs:
  - `POST /api/v1/alerts/:alertID/acknowledge`
  - `PUT /api/v1/alerts/:alertID/ownership`
- notification channel APIs:
  - `GET /api/v1/notification-channels`
  - `POST /api/v1/notification-channels`
  - `PUT /api/v1/notification-channels/:channelID`
- alert silence APIs:
  - `GET /api/v1/alert-silences`
  - `POST /api/v1/alert-silences`
  - `PUT /api/v1/alert-silences/:silenceID`
- alert delivery history API:
  - `GET /api/v1/alert-delivery-logs`
- frontend pages:
  - `/monitoring-workbench/overview`
  - `/monitoring-workbench/integrations`
  - `/monitoring-workbench/alerts`
  - `/monitoring-workbench/notifications`
  - `/monitoring-workbench/oncall`
  - `/monitoring-workbench/events`
  - notification channels, routes, and silences are grouped under `/monitoring-workbench/notifications`

Current persistence behavior:

- registered alert sources are written to `alert_integrations`
- normalized alerts are written to `alert_events`
- notification channel definitions are written to `notification_channels`
- silence windows are written to `alert_silences`
- downstream delivery attempts are written to `alert_delivery_logs`
- every accepted alert ingest also emits a normalized record into `event_stream`

This means the platform now owns:

- alert ingestion
- alert integration registration for Alertmanager, Grafana Alerting, and Generic Webhook payloads
- route matching
- downstream fan-out logging
- silence-based suppression
- acknowledgement
- owner and assignee state
- notification channel registration

监控工作台与 AI 工作台当前边界：

- 监控工作台负责告警治理、通知、自愈、值班与事件流
- AI 工作台负责调查、分析与证据归并
- 监控工作台可以把 `alertId`、cluster、namespace、workload、timeRange 通过标准 handoff scope 传给 AI 工作台
- AI 工作台只回链原始告警或事件，不在聊天面板里直接承接治理动作

## Recommended Modules

### Backend

- `internal/application/monitoring`
  - alert ingestion orchestration
  - alert normalization
  - alert integration registration and payload adapter orchestration
  - silence, ack, assign, resolve workflow
- `internal/application/notification`
  - channel dispatch orchestration
  - retry and delivery status
- `internal/repository/alert`
  - alert integrations, alert events, notification policies, and notification channels
- future: `internal/repository/alerts`
  - silences, routing rules, delivery logs

### Frontend

- `web/src/features/observability/monitoring-pages.tsx`
  - monitoring summary
  - alert integration registry and payload normalization tester
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

- alert_integrations
- alert_events
- alert_rules_shadow
- notification_policies
- alert_silences
- alert_delivery_logs
- notification_channels

Short-lived alert processing state should stay in backend-owned runtime structures or durable rows:

- dedup windows derived from alert fingerprints and timestamps
- bounded in-process burst buffers for inbound alerts
- dispatch retry state persisted in delivery or notification records
- active websocket/subscription state held in process memory

## Integration Strategy

### Inbound

- keep the legacy normalized webhook for compatibility: `/integrations/alerts/webhook`
- register explicit Alertmanager, Grafana Alerting, or Generic Webhook integrations
- use integration-specific tokens and `/integrations/alerts/:integrationID/webhook`
- normalize provider-native payloads into Soha `IngestAlert` records before route resolution, notification, self-healing, on-call, and event fan-out
- keep future platform-native rule evaluation in Soha, but do not reimplement a full external alerting engine inside the console

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

- delivery retry state
- escalation policies
- maintenance window orchestration beyond simple silences
- incident grouping and owner views at a higher level than single alerts

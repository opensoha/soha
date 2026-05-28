# Event Model

## Event Sources

- Kubernetes events
- release events
- build events
- alert events
- integration callbacks
- future MCP invocation events

## Unified Event Envelope

```json
{
  "id": "evt_01J...",
  "source": "kubernetes",
  "category": "resource.lifecycle",
  "severity": "warning",
  "cluster_id": "local",
  "namespace": "default",
  "resource": {
    "kind": "Pod",
    "name": "api-7f4c6d9f8f-9xt2q"
  },
  "summary": "Back-off restarting failed container",
  "payload": {},
  "occurred_at": "2026-03-22T10:00:00Z",
  "correlation_id": "corr_123"
}
```

## Event Center Responsibilities

- normalize source-specific events
- persist durable event stream in PostgreSQL when historical search matters
- keep hot delivery buffers process-local for SSE consumers
- support correlation by cluster, namespace, project, release, or operation

## MVP Scope

Phase 1 keeps event-center lightweight:

- consume Kubernetes events through resource access or watch cache
- expose normalized list API
- reserve schema for build and release events

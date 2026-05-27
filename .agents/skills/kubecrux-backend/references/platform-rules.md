# Platform Rules

## Cluster Access

- Support both direct kubeconfig mode and agent mode.
- Prefer informer or cache-backed reads for high-frequency namespace-scoped list operations.
- Use explicit timeouts for live cluster reads.
- When cache is unavailable, use bounded live-query fallback rather than unbounded retries.

## API Shape

- Return flattened platform DTOs the web console can consume directly.
- Keep transport thin. Semantic interpretation belongs in application services.
- Expose unsupported agent capabilities as unsupported instead of behaving inconsistently.
- Keep all-namespaces aggregation on the backend when the UI needs cross-namespace data.
- Use stable kubecrux DTOs for KubeVirt, PVE, Docker, and AI evidence surfaces as well; avoid returning provider-native objects directly.
- If a config or module flag makes a capability visible before runtime support is complete, return an explicit unsupported or degraded response.

## Authorization and Logging

- Resolve the acting principal before business logic.
- Check explicit permission keys in backend services, not only in frontend buttons.
- Record audit logs for important read, write, or action flows.
- Record operation logs for durable mutation history when the action changes runtime or persisted state.
- Keep module visibility, visible menus, route permission keys, and service authorization aligned but independent.
- Redact credentials, bearer tokens, kubeconfigs, AI provider secrets, and datasource credentials from audit logs, operation logs, callback payloads, and AI artifacts.

## Runtime Operation Rules

- Long-running work should be represented by durable execution tasks, Docker operations, or virtualization tasks with logs and allowed actions.
- Claim/callback APIs must be token-protected and idempotent around terminal states.
- Callback-driven updates must keep parent business objects synchronized; do not leave bundles, builds, deployments, Docker projects, or VM tasks behind in stale states.
- Cancel and retry are control-plane actions. Retry must prevent stale callbacks from older attempts from overwriting the new attempt.

## Change Checklist

- Does the endpoint respect cluster and namespace scope?
- Does an empty namespace mean all namespaces where appropriate?
- Is the output a platform-facing DTO instead of a raw Kubernetes type?
- Did the change preserve or improve cache and aggregation behavior?
- Are module descriptors, seed menus, frontend route metadata, and permission keys aligned?
- Does any external runtime action go through a task or operation runner path?
- Are terminal statuses and related business records backfilled consistently?
- Were tests and memory updated alongside the contract change?

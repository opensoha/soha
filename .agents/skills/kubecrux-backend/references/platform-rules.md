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

## Authorization and Logging

- Resolve the acting principal before business logic.
- Check explicit permission keys in backend services, not only in frontend buttons.
- Record audit logs for important read, write, or action flows.
- Record operation logs for durable mutation history when the action changes runtime or persisted state.

## Change Checklist

- Does the endpoint respect cluster and namespace scope?
- Does an empty namespace mean all namespaces where appropriate?
- Is the output a platform-facing DTO instead of a raw Kubernetes type?
- Did the change preserve or improve cache and aggregation behavior?
- Were tests and memory updated alongside the contract change?

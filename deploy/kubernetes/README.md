# Soha Agent Kubernetes Deployment

This sample runs `soha-agent` in production mode with in-cluster ServiceAccount
authentication.

Before applying it, replace the `change-me` values in the Secret and update the
control-plane URLs in the ConfigMap. The placeholders intentionally fail
production config validation until real runtime tokens are supplied.

Logs are emitted as JSON to stdout; collect them with the cluster log pipeline.

High-risk action audit records are written to
`/var/log/soha-agent/actions.jsonl`. The sample keeps that path on a PVC so a
log shipper or sidecar can forward JSONL records without exposing token
material.

Runtime metrics are available at `GET /api/v1/runtime/metrics` and require the
agent bearer token. Scrape through an authenticated Prometheus job or an
internal auth proxy; do not expose the endpoint anonymously.

Render the sample with Kustomize from this repository:

```sh
kustomize build deploy/kubernetes/base
```

## Workbench permissions and upgrades

The starter enables the explicit workbench action set by default: resource CRUD,
node maintenance/drain, workload actions, Pod exec and port forwarding. Its
ClusterRole grants named workbench API groups/resources and verbs,
including `get`, `list` and `watch` on `apps/controllerrevisions` for StatefulSet
and DaemonSet history. To enable Helm mutations, list the individual actions such
as `platform.helm_releases.install` in the Agent allowlist. Core requires
cluster-wide authorization for the corresponding Helm release action.

Review the action and ClusterRole diff before reapplying this starter to an
existing installation. An image-only update does not add missing permissions.
This manifest does not merge an installation's old custom action list; retain
those explicit settings when updating it. For configurable workbench, custom and
read-only profiles with matching RBAC, use the `soha-agent` Helm chart. Its default
auto profile selects workbench for new installs and preserves explicit old action
lists, including empty lists.

Custom CRD API groups still need explicit target rules, and CRD definition deletion
requires a separate action and RBAC grant. The starter grants no arbitrary API
group wildcard, cluster-admin, impersonation, `bind` or `escalate`; RBAC changes
remain subject to Kubernetes escalation safeguards. Secret values remain protected
by Core's independent Secret-data permission. Outpost uses its own deployment and
does not receive this workbench role.

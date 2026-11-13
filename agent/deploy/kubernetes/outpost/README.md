# Soha Outpost

This artifact runs the standard `ghcr.io/opensoha/soha-agent` image published by the agent release workflow. Outpost is a forward-auth decision point; the protected application traffic continues through the ingress or reverse proxy. Check out an agent release that advertises Identity Outpost protocol `v1`, replace the control-plane URL, Outpost ID (`agent_id`), and pinned Ed25519 trust key in `soha-outpost.yaml`, and pin the image before applying it. Provision the existing `soha-outpost-state`
PVC in the same namespace before starting the deployment. The two-replica
rolling deployment requires ReadWriteMany storage writable by group 10001.
The Agent creates a private state directory for each Pod UID. Preserve that
claim across container restarts and upgrades. When migrating an existing
installation with durable state, stop the old owners and preserve their replay
floors under the approved migration procedure before replacing its state mount.

The sequence below assumes that the storage claim has already been provisioned
by the installer:

```bash
git checkout vX.Y.Z
cd deploy/kubernetes/outpost
kubectl create namespace soha-outpost
kubectl create secret generic soha-outpost-secrets \
  --namespace soha-outpost \
  --from-file=agent-token \
  --from-file=control-plane-token
kustomize edit set image ghcr.io/opensoha/soha-agent=ghcr.io/opensoha/soha-agent:vX.Y.Z
kubectl apply --namespace soha-outpost -k .
```

The readiness probe stays unavailable until a valid, signed and unexpired configuration has been claimed. Copy the saved configuration from the Outpost deployment panel; `agent_id` must be the created Outpost ID. Set the Outpost Forward Auth URL to the full address reachable by the edge proxy, including `/api/v1/outpost/forward-auth` (or the configured HTTP base path). This is distinct from the Core control-plane URL. Remote login redirects require an HTTPS public Soha URL.

Management health uses the last server-received heartbeat, applied configuration version, and lease. Heartbeats older than 45 seconds are degraded; older than 90 seconds are unavailable. Registration alone is not healthy. Token rotation clears observations and the old token stops authorizing requests; update the mounted control-plane token and restart the agent. Embedded Outposts use local readiness without fabricated heartbeats.

The ingress controller must authenticate its subrequests with either `Authorization: Bearer <agent-token>` or `X-Soha-Outpost-Token: <agent-token>`. Keep that token in the ingress controller's secret-management path; do not commit it in an Ingress or Middleware resource. The browser-facing edge must remove incoming `X-Soha-*` identity and internal-token headers before authentication, copy only verified identity headers from the auth response, and remove both internal tokens before sending to the application. Use the `soha_proxy_session` cookie for browser authentication; `X-Soha-Session-Token` is for trusted integrations only. For applications under a shared parent domain, configure the Proxy provider `cookieDomain` (for example `.example.com`) so the callback cookie reaches the protected host.

Generate application-specific templates from the saved Provider setup panel. Never use the Agent-to-Core `/identity/outposts/:id/check` URL as the edge auth endpoint.

Integration templates:

- [NGINX Ingress](examples/nginx-ingress.md)
- [Traefik ForwardAuth](examples/traefik.md)

Protocol compatibility:

| Component | Requirement |
| --- | --- |
| Soha control plane | Identity Outpost runtime API with protocol `v1` |
| soha-agent | Release containing Identity Outpost protocol `v1` |
| Configuration | `protocol_version: v1` and the matching pinned Ed25519 public key |

Server and agent versions do not need identical SemVer values. Protocol `v1`, the signed configuration key, and the public runtime API are the compatibility boundary.

Render and validate the artifact without a cluster:

```bash
kubectl kustomize deploy/kubernetes/outpost | go run github.com/yannh/kubeconform/cmd/kubeconform@v0.8.0 -kubernetes-version 1.34.1 -strict -summary -
```

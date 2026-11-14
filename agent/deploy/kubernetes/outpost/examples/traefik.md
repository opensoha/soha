# Traefik ForwardAuth

Traefik propagates the Outpost `2xx`, deny, and redirect responses. A chain can inject the internal Outpost token before ForwardAuth and remove it before the protected service receives the request.

```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: soha-outpost-token
spec:
  headers:
    customRequestHeaders:
      X-Soha-Outpost-Token: REPLACE_FROM_SECRET_MANAGER
      X-Soha-Session-Token: ""
      X-Soha-User: ""
      X-Soha-User-ID: ""
      X-Soha-Email: ""
      X-Soha-Roles: ""
      X-Soha-Teams: ""
      X-Soha-Groups: ""
      X-Soha-Projects: ""
      X-Soha-Tags: ""
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: soha-outpost-auth
spec:
  forwardAuth:
    trustForwardHeader: false
    address: http://soha-outpost.soha-outpost.svc.cluster.local/api/v1/outpost/forward-auth
    authRequestHeaders:
      - Cookie
      - X-Forwarded-Host
      - X-Forwarded-Proto
      - X-Forwarded-For
      - X-Forwarded-Method
      - X-Forwarded-Uri
      - X-Soha-Outpost-Token
    authResponseHeaders:
      - X-Soha-User
      - X-Soha-User-ID
      - X-Soha-Email
      - X-Soha-Roles
      - X-Soha-Teams
      - X-Soha-Groups
      - X-Soha-Projects
      - X-Soha-Tags
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: soha-outpost-token-strip
spec:
  headers:
    customRequestHeaders:
      X-Soha-Outpost-Token: ""
      X-Soha-Session-Token: ""
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: soha-outpost-chain
spec:
  chain:
    middlewares:
      - name: soha-outpost-token
      - name: soha-outpost-auth
      - name: soha-outpost-token-strip
```

Reference `soha-outpost-chain` from the protected IngressRoute. Render `REPLACE_FROM_SECRET_MANAGER` through a secret-aware deployment mechanism and verify the strip middleware remains after ForwardAuth so the internal token never reaches the application.

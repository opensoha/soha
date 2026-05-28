# Backend Structure

```text
.
  cmd/server
  configs/config.yaml
  internal/api
  internal/application
  internal/bootstrap
  internal/domain
  internal/infrastructure
  internal/policy
  internal/platform
  internal/repository
  migrations
```

## Module Responsibilities

- `api`: HTTP transport, middleware, response shaping
- `application`: orchestration and use-case services
- `bootstrap`: dependency graph and database seed
- `domain`: contracts and platform view models
- `infrastructure`: config, logger, postgres, kubernetes, informer, swagger, mcp
- `policy`: RBAC, ABAC, and scope calculation
- `repository`: durable persistence such as audit logs

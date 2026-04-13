# Add Resource Module

1. Add or extend platform DTOs under `internal/domain/resource`.
2. Extend Kubernetes access or cache reading under `internal/infrastructure/kubernetes` and `internal/infrastructure/informer`.
3. Implement the resource orchestration in `internal/application/resource`.
4. Add authorization hooks through `internal/application/access`.
5. Add API handlers and routes under `internal/api`.
6. Add frontend table, detail, or filter module against the aggregated DTO.

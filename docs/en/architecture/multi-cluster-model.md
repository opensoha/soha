# Multi-Cluster Model

## Cluster Registry Model

Each cluster has:

- durable metadata in PostgreSQL
- credential metadata in PostgreSQL
- secret material referenced externally or stored through future secret providers
- runtime clients managed by cluster-manager

## Connection Strategy

Current runtime supports two connection modes:

- `direct_kubeconfig`
  - bootstrapped from `config.yaml` or registered through the cluster API
  - supports explicit context selection
  - builds per-cluster typed, dynamic, and discovery clients
  - starts informer/cache readers dynamically after registration
- `agent`
  - persists remote endpoint and token metadata in PostgreSQL
  - lets the backend pull summary and resource data from a remote agent
  - lets the backend send controlled execution actions such as restart and scale

Future expansion can add:

- encrypted credential stores
- cloud provider auth plugins
- service account federation
- richer agent actions such as logs, exec, and rollout history

## Health and Capability Discovery

cluster-manager should periodically collect:

- API reachability
- Kubernetes version
- available API groups and resources
- optional metrics availability
- last successful sync time

## Client Lifecycle

cluster-manager maintains a per-cluster client bundle:

- `kubernetes.Interface`
- `dynamic.Interface`
- `discovery.DiscoveryInterface`
- shared informer factory handles

Lifecycle rules:

- lazy initialize on first use or bootstrap
- refresh when credentials change or a new direct kubeconfig cluster is registered
- surface last error state
- close caches when a cluster is removed

For agent clusters, soha keeps the durable registry in PostgreSQL and creates HTTP clients on demand instead of `client-go` bundles.

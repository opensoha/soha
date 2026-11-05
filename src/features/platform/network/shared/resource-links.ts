import { hasPermission } from '@/features/auth'
import { buildKubernetesResourcePath } from '@/features/platform/shared/resource-ref'
export const permissionsByKind: Record<string, string> = {
  Service: 'platform.network.services.view',
  Deployment: 'platform.deployment.view',
  ReplicaSet: 'platform.workloads.replica-sets.view',
  StatefulSet: 'platform.workloads.stateful-sets.view',
  DaemonSet: 'platform.workloads.daemon-sets.view',
  Job: 'platform.workloads.jobs.view',
  CronJob: 'platform.workloads.cron-jobs.view',
  ServiceAccount: 'platform.access-control.service-accounts.view',
  EndpointSlice: 'platform.network.endpoint-slices.view',
  Ingress: 'platform.network.ingresses.view',
  IngressClass: 'platform.network.ingress-classes.view',
  GatewayClass: 'platform.network.gateway-classes.view',
  Gateway: 'platform.network.gateways.view',
  HTTPRoute: 'platform.network.http-routes.view',
  GRPCRoute: 'platform.network.grpc-routes.view',
  Pod: 'platform.pods.view',
  Node: 'platform.nodes.view',
  ConfigMap: 'platform.configuration.config-maps.view',
  Secret: 'platform.configuration.secrets.view',
  PersistentVolumeClaim: 'platform.storage.persistent-volume-claims.view',
}

export function networkResourcePath(
  permissions: Parameters<typeof hasPermission>[0],
  ref: { clusterId: string; namespace?: string; kind: string; name: string },
) {
  if (
    !ref.name ||
    !permissionsByKind[ref.kind] ||
    !hasPermission(permissions, permissionsByKind[ref.kind])
  )
    return null
  const path = buildKubernetesResourcePath({
    ...ref,
    apiVersion: 'unknown',
    scopeMode: ref.namespace ? 'namespace' : 'cluster',
  })
  if (!path) return null
  const [base, raw = ''] = path.split('?'),
    params = new URLSearchParams(raw)
  params.set('clusterId', ref.clusterId)
  return `${base}?${params}`
}

import type { components } from '@opensoha/contracts/gen/ts/sohaapi'
import {
  buildNamespacedDetailQuery,
  buildWorkloadDetailPath,
} from '@/features/platform/workloads-model'

type KubernetesResourceRef = components['schemas']['KubernetesResourceRef']

function namespacedPath(base: string, ref: KubernetesResourceRef) {
  return `${base}/${encodeURIComponent(ref.name)}${buildNamespacedDetailQuery(ref.namespace)}`
}

export function buildKubernetesResourcePath(ref: KubernetesResourceRef) {
  const kind = ref.kind.replace(/[^a-z]/gi, '').toLowerCase()
  const workloadRoutes: Record<string, string> = {
    cronjob: 'cronjobs',
    daemonset: 'daemonsets',
    deployment: 'deployments',
    job: 'jobs',
    pod: 'pods',
    replicaset: 'replicasets',
    replicationcontroller: 'replicationcontrollers',
    statefulset: 'statefulsets',
  }
  const workloadRoute = workloadRoutes[kind]
  if (workloadRoute) {
    return buildWorkloadDetailPath(
      workloadRoute,
      ref.name,
      null,
      ref.namespace ?? '',
      ref.clusterId,
    )
  }

  switch (kind) {
    case 'node':
      return `/cluster-resources/nodes/${encodeURIComponent(ref.name)}?clusterId=${encodeURIComponent(ref.clusterId)}`
    case 'namespace':
      return '/cluster-resources/namespaces'
    case 'service':
      return namespacedPath('/network/services', ref)
    case 'ingress':
      return namespacedPath('/network/ingresses', ref)
    case 'endpointslice':
      return namespacedPath('/network/endpointslices', ref)
    case 'networkpolicy':
      return namespacedPath('/network/networkpolicies', ref)
    case 'ingressclass':
      return namespacedPath('/network/ingressclasses', ref)
    case 'gatewayclass':
      return namespacedPath('/network/gateway-api/gatewayclasses', ref)
    case 'gateway':
      return namespacedPath('/network/gateway-api/gateways', ref)
    case 'httproute':
      return namespacedPath('/network/gateway-api/httproutes', ref)
    case 'grpcroute':
      return namespacedPath('/network/gateway-api/grpcroutes', ref)
    case 'backendtlspolicy':
      return namespacedPath('/network/gateway-api/backendtlspolicies', ref)
    case 'referencegrant':
      return namespacedPath('/network/gateway-api/referencegrants', ref)
    case 'configmap':
      return namespacedPath('/configuration/configmaps', ref)
    case 'secret':
      return namespacedPath('/configuration/secrets', ref)
    case 'resourcequota':
      return namespacedPath('/configuration/resourcequotas', ref)
    case 'limitrange':
      return namespacedPath('/configuration/limitranges', ref)
    case 'lease':
      return namespacedPath('/configuration/leases', ref)
    case 'horizontalpodautoscaler':
      return namespacedPath('/configuration/hpas', ref)
    case 'poddisruptionbudget':
      return namespacedPath('/configuration/poddisruptionbudgets', ref)
    case 'priorityclass':
      return namespacedPath('/configuration/priorityclasses', ref)
    case 'runtimeclass':
      return namespacedPath('/configuration/runtimeclasses', ref)
    case 'mutatingwebhookconfiguration':
      return namespacedPath('/configuration/mutatingwebhookconfigurations', ref)
    case 'validatingwebhookconfiguration':
      return namespacedPath('/configuration/validatingwebhookconfigurations', ref)
    case 'persistentvolumeclaim':
      return namespacedPath('/storage/persistentvolumeclaims', ref)
    case 'persistentvolume':
      return namespacedPath('/storage/persistentvolumes', ref)
    case 'storageclass':
      return namespacedPath('/storage/storageclasses', ref)
    case 'serviceaccount':
      return namespacedPath('/platform-access-control/serviceaccounts', ref)
    case 'role':
      return namespacedPath('/platform-access-control/roles', ref)
    case 'rolebinding':
      return namespacedPath('/platform-access-control/rolebindings', ref)
    case 'clusterrole':
      return namespacedPath('/platform-access-control/clusterroles', ref)
    case 'clusterrolebinding':
      return namespacedPath('/platform-access-control/clusterrolebindings', ref)
    default:
      return null
  }
}

export function buildKubernetesEventResourcePath(
  event: { involvedKind?: string; involvedName?: string; namespace?: string },
  clusterId: string | null,
) {
  const kind = event.involvedKind?.trim()
  const name = event.involvedName?.trim()
  if (!clusterId || !kind || !name) return null
  return buildKubernetesResourcePath({
    apiVersion: 'unknown',
    clusterId,
    kind,
    name,
    namespace: event.namespace?.trim() || undefined,
    scopeMode: event.namespace?.trim() ? 'namespace' : 'cluster',
  })
}

import type {
  EnvironmentServiceReferenceResolvedTarget,
  KubernetesResourceGraph,
  KubernetesResourceRef,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { getKubernetesResourceGraph, isOwnership, type RealtimeSession } from '@/features/platform'
import { api } from '@/services/api-client'
import { isApiError } from '@/services/api-error'
import { deliveryApi } from '../api'
import type { ResourceRelationsView } from '../types'

type ReferenceTarget = EnvironmentServiceReferenceResolvedTarget['target']
type NamespacedIdentity = KubernetesResourceRef & { uid: string; namespace: string }

export function referenceTargetRoots(target: ReferenceTarget): NamespacedIdentity[] {
  const helm = target.workloadKind === 'HelmRelease'
  if (!helm && target.resources.length !== 1) return []
  return target.resources.flatMap(({ resource }) => {
    if (
      !resource.uid ||
      resource.clusterId !== target.clusterId ||
      resource.namespace !== target.namespace
    )
      return []
    const matches = helm
      ? resource.apiVersion === 'apps/v1' &&
        ['Deployment', 'StatefulSet', 'DaemonSet'].includes(resource.kind)
      : resource.name === target.workloadName &&
        (resource.kind === target.workloadKind || target.workloadKind === 'CustomResource')
    return matches ? [{ ...resource, uid: resource.uid, namespace: target.namespace }] : []
  })
}

function sameRoot(observed: KubernetesResourceRef, root: NamespacedIdentity) {
  return (
    observed.uid === root.uid &&
    observed.apiVersion === root.apiVersion &&
    observed.clusterId === root.clusterId &&
    observed.namespace === root.namespace &&
    observed.kind === root.kind &&
    observed.name === root.name
  )
}

function podsInScope(resources: KubernetesResourceRef[], root: NamespacedIdentity) {
  return resources.flatMap((resource) =>
    resource.apiVersion === 'v1' &&
    resource.kind === 'Pod' &&
    resource.clusterId === root.clusterId &&
    resource.namespace === root.namespace &&
    resource.uid
      ? [{ ...resource, uid: resource.uid, namespace: root.namespace }]
      : [],
  )
}

function helmOwnedPods(graph: KubernetesResourceGraph, root: NamespacedIdentity) {
  const rootNode = graph.nodes.find(
    (node) => node.id === graph.rootId && sameRoot(node.resource, root),
  )
  if (!rootNode) return []
  const scoped = new Map(
    graph.nodes
      .filter(
        ({ resource }) =>
          resource.uid &&
          resource.clusterId === root.clusterId &&
          resource.namespace === root.namespace,
      )
      .map((node) => [node.id, node.resource]),
  )
  const children = new Map<string, string[]>()
  for (const edge of graph.edges) {
    if (!isOwnership(edge.relation) || !scoped.has(edge.sourceId) || !scoped.has(edge.targetId))
      continue
    const next = children.get(edge.sourceId)
    if (next) next.push(edge.targetId)
    else children.set(edge.sourceId, [edge.targetId])
  }
  const owned = new Set([rootNode.id])
  for (const parent of owned) {
    for (const child of children.get(parent) ?? []) owned.add(child)
  }
  return podsInScope(
    [...owned].flatMap((id) => scoped.get(id) ?? []),
    root,
  )
}

export async function readReferenceOwnedPods(
  sourceId: string,
  target: ReferenceTarget,
  root: NamespacedIdentity,
  signal?: AbortSignal,
) {
  if (target.workloadKind === 'HelmRelease') {
    const graph = await getKubernetesResourceGraph(root, root.kind, root.name)
    return helmOwnedPods(graph, root)
  }
  const relations: ResourceRelationsView = await deliveryApi.environments.targetRelations(
    sourceId,
    target.targetId,
    signal,
  )
  if (!sameRoot(relations.root, root)) return []
  return podsInScope(
    relations.nodes.filter(({ role }) => role === 'owned').map(({ resource }) => resource),
    root,
  )
}

export async function validateDeliverySessionReference(
  session: RealtimeSession,
  localeCode: string,
): Promise<void> {
  const reference = session.reference
  if (!reference) return
  const chinese = localeCode === 'zh_CN'
  const changed = chinese
    ? '引用来源或 Pod 归属已变化，请从当前服务重新打开会话。'
    : 'The reference or Pod ownership changed. Open a new session from the current service.'
  const query = new URLSearchParams({
    expectedReferenceId: reference.referenceId,
    expectedRevision: String(reference.referenceRevision),
  })
  const path =
    '/application-environments/' +
    encodeURIComponent(reference.viewEnvironmentId) +
    '/service-references/' +
    encodeURIComponent(reference.serviceId) +
    '/targets/' +
    encodeURIComponent(reference.targetId)
  try {
    const resolved = await api.getEnvelope<EnvironmentServiceReferenceResolvedTarget>(
      `${path}?${query}`,
    )
    if (
      resolved.applicationId !== reference.applicationId ||
      resolved.serviceId !== reference.serviceId ||
      resolved.referenceId !== reference.referenceId ||
      resolved.referenceRevision !== reference.referenceRevision ||
      resolved.view.applicationEnvironmentId !== reference.viewEnvironmentId ||
      resolved.origin.applicationEnvironmentId !== reference.sourceEnvironmentId ||
      resolved.target.targetId !== reference.targetId ||
      resolved.target.clusterId !== session.clusterId ||
      resolved.target.namespace !== session.namespace
    )
      throw new Error(changed)
    const root = referenceTargetRoots(resolved.target).find(({ uid }) => uid === reference.rootUid)
    if (!root) throw new Error(changed)
    const pods = await readReferenceOwnedPods(reference.sourceEnvironmentId, resolved.target, root)
    if (!pods.some((pod) => pod.uid === session.podUid && pod.name === session.podName))
      throw new Error(changed)
  } catch (error) {
    if (isApiError(error)) {
      if (error.status === 401 || error.status === 403)
        throw new Error(
          chinese
            ? '当前没有引用来源访问权限。'
            : 'Access to the reference source is no longer permitted.',
          { cause: error },
        )
      if (error.status === 404 || error.status === 409) throw new Error(changed, { cause: error })
    }
    if (error instanceof Error && error.message === changed) throw error
    throw new Error(
      chinese
        ? '无法重新校验引用来源，请重试。'
        : 'Unable to revalidate the reference source. Retry to reconnect.',
      { cause: error },
    )
  }
}

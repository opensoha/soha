import type { ManifestDeployment } from '@opensoha/contracts/gen/ts/sohaapi'
import type { RelationWorkbenchData } from '../runtime/resource-relation-workbench'

const INVENTORY_RULE_ID = 'observed inventory'
const ROOT_UID_PREFIX = 'soha-manifest:'

type InventoryRow = ManifestDeployment['status']['inventory'][number]

function identityKey(row: InventoryRow): string {
  return JSON.stringify([row.apiVersion, row.kind, row.namespace, row.name, row.uid])
}

function nameKey(row: InventoryRow): string {
  return JSON.stringify([row.apiVersion, row.kind, row.namespace, row.name])
}

function observedTime(row: InventoryRow): number {
  const timestamp = Date.parse(row.lastObservedAt)
  return Number.isFinite(timestamp) && timestamp > 0 ? timestamp : Number.NEGATIVE_INFINITY
}

function stableRowKey(row: InventoryRow): string {
  return JSON.stringify([
    row.lastObservedAt,
    row.resourceVersion,
    row.resourceGeneration,
    row.observedResourceGeneration,
    row.health,
    row.desiredObjectDigest,
    row.observedObjectDigest,
    row.finalizers,
  ])
}

function preferRow(candidate: InventoryRow, current: InventoryRow): boolean {
  const candidateTime = observedTime(candidate)
  const currentTime = observedTime(current)
  return (
    candidateTime > currentTime ||
    (candidateTime === currentTime && stableRowKey(candidate) > stableRowKey(current))
  )
}

function completeIdentity(row: InventoryRow, deploymentId: string, namespace: string) {
  return (
    row.deploymentId === deploymentId &&
    row.namespace === namespace &&
    [row.apiVersion, row.kind, row.name, row.uid].every(
      (value) => typeof value === 'string' && value.trim().length > 0,
    ) &&
    !row.deletingAt
  )
}

/**
 * Projects confirmed manifest inventory as deployed membership. The synthetic
 * ManifestDeployment root is a Soha configuration identity, not a Kubernetes
 * resource; every edge is a reference, not Kubernetes ownership or live proof.
 */
export function projectObservedManifestInventory(
  deployment: ManifestDeployment,
  scope: { clusterId: string; namespace: string },
): RelationWorkbenchData | undefined {
  const observedGeneration = deployment.status?.observedGeneration
  if (!(observedGeneration > 0)) return undefined

  const candidates = (deployment.status.inventory ?? []).filter(
    (row) =>
      completeIdentity(row, deployment.id, scope.namespace) &&
      row.generation === observedGeneration,
  )
  if (candidates.length === 0) return undefined

  const uidIdentities = new Map<string, Set<string>>()
  const nameUids = new Map<string, Set<string>>()
  for (const row of candidates) {
    const uid = row.uid as string
    const identities = uidIdentities.get(uid) ?? new Set<string>()
    identities.add(identityKey(row))
    uidIdentities.set(uid, identities)

    const key = nameKey(row)
    const uids = nameUids.get(key) ?? new Set<string>()
    uids.add(uid)
    nameUids.set(key, uids)
  }

  const unambiguous = candidates.filter((row) => {
    const uid = row.uid as string
    return uidIdentities.get(uid)?.size === 1 && nameUids.get(nameKey(row))?.size === 1
  })
  if (unambiguous.length === 0) return undefined

  const deduplicated = new Map<string, InventoryRow>()
  for (const row of unambiguous) {
    const key = identityKey(row)
    const current = deduplicated.get(key)
    if (!current || preferRow(row, current)) deduplicated.set(key, row)
  }

  const rows = [...deduplicated.entries()]
    .sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0))
    .map(([, row]) => row)
  const rootUid = `${ROOT_UID_PREFIX}${scope.clusterId}/${scope.namespace}/${deployment.id}`
  const root = {
    clusterId: scope.clusterId,
    namespace: scope.namespace,
    apiVersion: '',
    kind: 'ManifestDeployment',
    name: deployment.id,
    uid: rootUid,
    scopeMode: 'namespace' as const,
  }
  const observedAt = rows.reduce<string | undefined>((newest, row) => {
    if (!Number.isFinite(observedTime(row))) return newest
    if (!newest || observedTime(row) > Date.parse(newest)) return row.lastObservedAt
    if (observedTime(row) === Date.parse(newest) && row.lastObservedAt > newest) {
      return row.lastObservedAt
    }
    return newest
  }, undefined)

  return {
    root,
    nodes: [
      { resource: root, role: 'root', depth: 0 },
      ...rows.map((row) => ({
        resource: {
          clusterId: scope.clusterId,
          namespace: row.namespace,
          apiVersion: row.apiVersion,
          kind: row.kind,
          name: row.name,
          uid: row.uid as string,
          scopeMode: 'namespace' as const,
        },
        role: 'reference' as const,
        depth: 1,
      })),
    ],
    edges: rows.map((row) => ({
      fromUid: rootUid,
      toUid: row.uid as string,
      type: 'references' as const,
      ruleId: INVENTORY_RULE_ID,
    })),
    ...(observedAt ? { observedAt } : {}),
  }
}

import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import type { CRD } from './types'
import { isNamespacedCRD } from './utils'

export function buildCRDCatalogPath(clusterId: string) {
  return buildClusterScopedPath(clusterId, 'extensions/crds')
}

export function buildCustomResourceCollectionPath(
  clusterId: string,
  crd: CRD,
  namespace?: string | null,
) {
  return buildClusterScopedPath(
    clusterId,
    `extensions/crds/${encodeURIComponent(crd.name)}/resources`,
    isNamespacedCRD(crd) ? namespace : null,
    { version: crd.version },
  )
}

export function buildCustomResourceItemPath(
  clusterId: string,
  crd: CRD,
  resourceName: string,
  namespace?: string | null,
  suffix?: 'yaml',
) {
  const base = `extensions/crds/${encodeURIComponent(crd.name)}/resources/${encodeURIComponent(resourceName)}`
  return buildClusterScopedPath(
    clusterId,
    suffix ? `${base}/${suffix}` : base,
    isNamespacedCRD(crd) ? namespace : null,
    { version: crd.version },
  )
}

export function buildCRDApiGroupDetailPath(group: string) {
  return `/extensions/apis/${encodeURIComponent(group)}`
}

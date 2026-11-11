import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import type {
  HelmChartCatalogInput,
  HelmChartDetailInput,
  HelmChartValuesInput,
  HelmReleaseTarget,
} from './types'

export function buildHelmReleaseListPath(clusterId: string, namespace?: string | null) {
  return buildClusterScopedPath(clusterId, 'helm/releases', namespace)
}

function buildHelmReleaseResourcePath(
  target: HelmReleaseTarget,
  suffix?: string,
  params?: Record<string, string | number | boolean | null | undefined>,
) {
  const resource = `helm/releases/${encodeURIComponent(target.name)}${suffix ? `/${suffix}` : ''}`
  return buildClusterScopedPath(target.clusterId, resource, target.namespace, params)
}

export const buildHelmReleaseDetailResourcePath = (target: HelmReleaseTarget) =>
  buildHelmReleaseResourcePath(target, 'detail')
export const buildHelmReleaseValuesPath = (target: HelmReleaseTarget) =>
  buildHelmReleaseResourcePath(target, 'values')
export const buildHelmReleaseHistoryPath = (target: HelmReleaseTarget) =>
  buildHelmReleaseResourcePath(target, 'history')
export const buildHelmReleaseManifestPath = (target: HelmReleaseTarget, revision?: string) =>
  buildHelmReleaseResourcePath(target, 'manifest', { revision })
export const buildHelmReleaseRollbackPlanPath = (target: HelmReleaseTarget) =>
  buildHelmReleaseResourcePath(target, 'rollback/plan')
export const buildHelmReleaseRollbackPath = (target: HelmReleaseTarget) =>
  buildHelmReleaseResourcePath(target, 'rollback')
export const buildHelmReleaseDeletePath = (target: HelmReleaseTarget) =>
  buildHelmReleaseResourcePath(target)

export function buildHelmChartCatalogPath(input: HelmChartCatalogInput) {
  return buildClusterScopedPath(input.clusterId, 'helm/charts', null, {
    keyword: input.keyword,
    limit: input.limit,
    offset: input.offset,
  })
}

export function buildHelmChartDetailPath(input: HelmChartDetailInput) {
  return buildClusterScopedPath(
    input.clusterId,
    `helm/charts/${encodeURIComponent(input.repositoryName)}/${encodeURIComponent(input.chartName)}`,
    null,
    { version: input.version },
  )
}

export function buildHelmChartValuesPath(input: HelmChartValuesInput) {
  return buildClusterScopedPath(input.clusterId, 'helm/charts/values', null, {
    packageId: input.packageId,
    name: input.name,
    version: input.version,
  })
}

export function buildHelmChartInstallPath(clusterId: string) {
  return buildClusterScopedPath(clusterId, 'helm/charts/install')
}

export function buildHelmReleaseRoutePath(
  name: string,
  namespace?: string | null,
  extraParams?: Record<string, string | null | undefined>,
) {
  const params = new URLSearchParams()
  if (namespace) params.set('namespace', namespace)
  Object.entries(extraParams ?? {}).forEach(([key, value]) => {
    if (value) params.set(key, value)
  })
  const query = params.toString()
  const path = `/helm/releases/${encodeURIComponent(name)}`
  return query ? `${path}?${query}` : path
}

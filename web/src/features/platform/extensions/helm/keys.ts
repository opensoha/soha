import type {
  HelmChartCatalogInput,
  HelmChartDetailInput,
  HelmChartValuesInput,
  HelmReleaseTarget,
} from './types'

export const helmKeys = {
  all: ['platform', 'extensions', 'helm'] as const,
  releases: (clusterId?: string | null) =>
    [...helmKeys.all, 'releases', clusterId ?? null] as const,
  releaseList: (clusterId?: string | null, namespace?: string | null) =>
    [...helmKeys.releases(clusterId), 'list', namespace?.trim() || '__all__'] as const,
  releaseDetail: (target: HelmReleaseTarget) =>
    [...helmKeys.releases(target.clusterId), target.namespace, target.name, 'detail'] as const,
  releaseValues: (target: HelmReleaseTarget) =>
    [...helmKeys.releases(target.clusterId), target.namespace, target.name, 'values'] as const,
  releaseHistory: (target: HelmReleaseTarget) =>
    [...helmKeys.releases(target.clusterId), target.namespace, target.name, 'history'] as const,
  releaseManifest: (target: HelmReleaseTarget, revision?: string) =>
    [
      ...helmKeys.releases(target.clusterId),
      target.namespace,
      target.name,
      'manifest',
      revision || 'current',
    ] as const,
  charts: (clusterId?: string | null) => [...helmKeys.all, 'charts', clusterId ?? null] as const,
  chartCatalog: (input: HelmChartCatalogInput) =>
    [
      ...helmKeys.charts(input.clusterId),
      'catalog',
      input.keyword,
      input.offset,
      input.limit,
    ] as const,
  chartDetail: (input: HelmChartDetailInput) =>
    [
      ...helmKeys.charts(input.clusterId),
      'detail',
      input.repositoryName,
      input.chartName,
      input.version,
    ] as const,
  chartValues: (input: HelmChartValuesInput) =>
    [
      ...helmKeys.charts(input.clusterId),
      'values',
      input.packageId,
      input.name,
      input.version,
    ] as const,
  installProgress: (target: HelmReleaseTarget) =>
    [
      ...helmKeys.releases(target.clusterId),
      target.namespace,
      target.name,
      'install-progress',
    ] as const,
}

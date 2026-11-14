import { queryOptions } from '@tanstack/react-query'
import {
  getHelmChartDetail,
  getHelmChartValues,
  getHelmReleaseDetail,
  getHelmReleaseHistory,
  getHelmReleaseManifest,
  getHelmReleaseValues,
  listHelmCharts,
  listHelmReleases,
} from './api'
import { helmKeys } from './keys'
import type {
  HelmChartCatalogInput,
  HelmChartDetailInput,
  HelmChartValuesInput,
  HelmReleaseTarget,
} from './types'

export const helmQueries = {
  releases: (clusterId?: string | null, namespace?: string | null) =>
    queryOptions({
      queryKey: helmKeys.releaseList(clusterId, namespace),
      queryFn: () => listHelmReleases(clusterId!, namespace),
      enabled: Boolean(clusterId),
    }),
  releaseDetail: (target: HelmReleaseTarget | null) =>
    queryOptions({
      queryKey: target ? helmKeys.releaseDetail(target) : [...helmKeys.all, 'detail', 'disabled'],
      queryFn: () => getHelmReleaseDetail(target!),
      enabled: Boolean(target?.clusterId && target.namespace && target.name),
    }),
  releaseValues: (target: HelmReleaseTarget | null, active = true) =>
    queryOptions({
      queryKey: target ? helmKeys.releaseValues(target) : [...helmKeys.all, 'values', 'disabled'],
      queryFn: () => getHelmReleaseValues(target!),
      enabled: active && Boolean(target?.clusterId && target.namespace && target.name),
    }),
  releaseHistory: (target: HelmReleaseTarget | null, active = true) =>
    queryOptions({
      queryKey: target ? helmKeys.releaseHistory(target) : [...helmKeys.all, 'history', 'disabled'],
      queryFn: () => getHelmReleaseHistory(target!),
      enabled: active && Boolean(target?.clusterId && target.namespace && target.name),
    }),
  releaseManifest: (target: HelmReleaseTarget | null, revision?: string, active = true) =>
    queryOptions({
      queryKey: target
        ? helmKeys.releaseManifest(target, revision)
        : [...helmKeys.all, 'manifest', 'disabled'],
      queryFn: () => getHelmReleaseManifest(target!, revision),
      enabled: active && Boolean(target?.clusterId && target.namespace && target.name),
    }),
  chartCatalog: (input: HelmChartCatalogInput | null) =>
    queryOptions({
      queryKey: input ? helmKeys.chartCatalog(input) : [...helmKeys.all, 'catalog', 'disabled'],
      queryFn: () => listHelmCharts(input!),
      enabled: Boolean(input?.clusterId),
    }),
  chartDetail: (input: HelmChartDetailInput | null) =>
    queryOptions({
      queryKey: input ? helmKeys.chartDetail(input) : [...helmKeys.all, 'chart-detail', 'disabled'],
      queryFn: () => getHelmChartDetail(input!),
      enabled: Boolean(input?.clusterId && input.repositoryName && input.chartName),
    }),
  chartValues: (input: HelmChartValuesInput | null, active: boolean) =>
    queryOptions({
      queryKey: input ? helmKeys.chartValues(input) : [...helmKeys.all, 'chart-values', 'disabled'],
      queryFn: () => getHelmChartValues(input!),
      enabled: active && Boolean(input?.clusterId && input.packageId && input.version),
    }),
  installProgress: (target: HelmReleaseTarget | null, enabled: boolean) =>
    queryOptions({
      queryKey: target
        ? helmKeys.installProgress(target)
        : [...helmKeys.all, 'progress', 'disabled'],
      queryFn: () => getHelmReleaseDetail(target!),
      enabled: enabled && Boolean(target),
      retry: false,
      refetchInterval: (query) => (query.state.status === 'error' ? false : 5000),
    }),
}

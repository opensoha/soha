export type {
  HelmChart,
  HelmChartCatalog,
  HelmChartDetail,
  HelmChartInstallResource,
  HelmChartInstallResult,
  HelmChartValuesTemplate,
  HelmRelease,
  HelmReleaseDetail,
  HelmReleaseHistory,
  HelmReleaseManifest,
  HelmValues,
} from '@/types'

export interface HelmReleaseTarget {
  clusterId: string
  name: string
  namespace: string
}

export interface UpdateHelmValuesVariables extends HelmReleaseTarget {
  content: string
  expectedRevision: number
  expectedContent: string
}

export interface HelmReleaseRollbackVariables extends HelmReleaseTarget {
  revision: number
  wait: boolean
  timeoutSeconds: number
}

export interface HelmChartCatalogInput {
  clusterId: string
  keyword: string
  limit: number
  offset: number
}

export interface HelmChartDetailInput {
  chartName: string
  clusterId: string
  repositoryName: string
  version: string
}

export interface HelmChartValuesInput {
  clusterId: string
  name?: string
  packageId: string
  version: string
}

export interface HelmChartInstallFormValues {
  repositoryName?: string
  repositoryUrl: string
  chartName: string
  version: string
  releaseName: string
  namespace: string
  createNamespace: boolean
  wait: boolean
  timeoutSeconds: number
}

export interface HelmChartInstallVariables extends HelmChartInstallFormValues {
  clusterId: string
  valuesYaml: string
}

export interface HelmChartInstallTarget {
  chartName: string
  namespace: string
  releaseName: string
  timeoutSeconds?: number
  version: string
  wait?: boolean
}

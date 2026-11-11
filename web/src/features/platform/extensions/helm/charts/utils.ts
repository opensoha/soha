import type {
  HelmChart,
  HelmChartDetail,
  HelmChartInstallFormValues,
  HelmChartInstallResult,
  HelmChartInstallTarget,
  HelmReleaseDetail,
} from '../types'

export function defaultHelmReleaseName(chartName?: string) {
  const normalized = (chartName || 'release')
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return normalized || 'release'
}

export function retryHelmReleaseName(releaseName?: string, chartName?: string) {
  const base = defaultHelmReleaseName(releaseName || chartName)
  const suffix = new Date().toISOString().slice(11, 19).replace(/:/g, '')
  const maxBaseLength = Math.max(1, 53 - suffix.length - 1)
  return `${base.slice(0, maxBaseLength).replace(/-+$/g, '') || 'release'}-${suffix}`
}

export function isHelmReleaseNameConflictError(message?: string) {
  const normalized = (message || '').toLowerCase()
  return (
    normalized.includes('cannot re-use a name that is still in use') ||
    normalized.includes('already used by helm release history')
  )
}

export function formatHelmInstallError(
  message: string,
  localeCode: string,
  target?: HelmChartInstallTarget | null,
) {
  if (!isHelmReleaseNameConflictError(message)) return message
  return localeCode === 'zh_CN'
    ? `Release "${target?.releaseName || '-'}" 在命名空间 "${target?.namespace || '-'}" 已有 Helm 记录。请换一个 Release 名，或先到 Helm Releases 清理已有 release 后重试。`
    : `Release "${target?.releaseName || '-'}" in namespace "${target?.namespace || '-'}" already has Helm history. Choose another release name, or clean up the existing release in Helm Releases before retrying.`
}

function normalizeHelmCompareValue(value?: string | null) {
  return (value || '').trim().toLowerCase()
}

export function isHelmReleaseDeployed(status?: string) {
  return normalizeHelmCompareValue(status) === 'deployed'
}

export function helmReleaseMatchesInstallTarget(
  release?: HelmReleaseDetail,
  target?: HelmChartInstallTarget | null,
) {
  if (!release || !target) return false
  if (normalizeHelmCompareValue(release.name) !== normalizeHelmCompareValue(target.releaseName)) {
    return false
  }
  if (
    normalizeHelmCompareValue(release.namespace) !== normalizeHelmCompareValue(target.namespace)
  ) {
    return false
  }
  if (
    target.chartName &&
    normalizeHelmCompareValue(release.chartName) !== normalizeHelmCompareValue(target.chartName)
  ) {
    return false
  }
  return !target.version || (release.chartVersion || '').trim() === target.version.trim()
}

export function mapObservedHelmReleaseToInstallResult(
  release: HelmReleaseDetail,
  localeCode: string,
): HelmChartInstallResult {
  return {
    name: release.name,
    namespace: release.namespace,
    revision: release.revision,
    status: release.status,
    chart: release.chart,
    chartName: release.chartName,
    chartVersion: release.chartVersion,
    appVersion: release.appVersion,
    description:
      release.description ||
      (localeCode === 'zh_CN'
        ? '检测到同名 Helm Release 已部署，本次安装请求已满足。'
        : 'Detected an already deployed Helm release; this install request is already satisfied.'),
    notes: release.notes,
    resources: [],
  }
}

export function defaultHelmChartInstallForm(
  chart: HelmChart,
  namespace?: string | null,
): HelmChartInstallFormValues {
  return {
    repositoryName: chart.repositoryName,
    repositoryUrl: chart.repositoryUrl || chart.urls?.[0] || '',
    chartName: chart.name,
    version: chart.latestVersion || '',
    releaseName: defaultHelmReleaseName(chart.name),
    namespace: namespace || 'default',
    createNamespace: true,
    wait: true,
    timeoutSeconds: 300,
  }
}

export function getHelmChartVersionOptions(detail?: HelmChartDetail, chart?: HelmChart | null) {
  const versions = detail?.availableVersions?.length
    ? detail.availableVersions.map((item) => ({
        label: item.appVersion ? `${item.version} · ${item.appVersion}` : item.version,
        value: item.version,
      }))
    : (detail?.versions ?? chart?.versions ?? []).map((version) => ({
        label: version,
        value: version,
      }))
  const latest = detail?.latestVersion || chart?.latestVersion
  if (latest && !versions.some((item) => item.value === latest)) {
    versions.unshift({ label: latest, value: latest })
  }
  return versions
}

export function getHelmChartBadges(chart: HelmChart) {
  const badges: Array<{ color?: string; label: string }> = []
  if (chart.official) badges.push({ color: 'blue', label: 'Official' })
  if (chart.verifiedPublisher) badges.push({ color: 'green', label: 'Verified' })
  if (chart.cncf) badges.push({ color: 'cyan', label: 'CNCF' })
  if (chart.signed) badges.push({ color: 'purple', label: 'Signed' })
  if (chart.deprecated) badges.push({ color: 'warning', label: 'Deprecated' })
  return badges
}

export function hasHelmChartSecuritySummary(chart: HelmChart) {
  return Boolean(
    (chart.securityCritical ?? 0) +
    (chart.securityHigh ?? 0) +
    (chart.securityMedium ?? 0) +
    (chart.securityLow ?? 0) +
    (chart.securityUnknown ?? 0),
  )
}

export function formatHelmChartCount(value: number, localeCode: string) {
  return new Intl.NumberFormat(localeCode === 'zh_CN' ? 'zh-CN' : 'en-US').format(value)
}

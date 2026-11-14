import { Card, Typography } from 'antd'
import { useI18n } from '@/i18n'
import { WorkloadOverviewConfiguration } from '../../../workloads/shared/workload-overview'
import type { HelmChart, HelmChartDetail } from '../types'

function externalUrl(value?: string) {
  if (!value) return undefined
  try {
    const url = new URL(value)
    return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password
      ? url.href
      : undefined
  } catch {
    return undefined
  }
}

function ExternalLink({ value, label }: { value?: string; label?: string }) {
  const url = externalUrl(value)
  return url ? (
    <a href={url} target="_blank" rel="noopener noreferrer">
      {label || value}
    </a>
  ) : (
    <>{label || value}</>
  )
}

export function ChartPackageOverview({
  chart,
  version,
}: {
  chart: HelmChartDetail | HelmChart
  version: string
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const unavailable = zh ? '未提供' : 'Not provided'
  const detail = chart as HelmChartDetail
  const selectedVersion = detail.availableVersions?.find((item) => item.version === version)
  const links = [
    ...(chart.homeUrl || chart.home
      ? [{ name: zh ? '项目主页' : 'Project home', url: chart.homeUrl || chart.home }]
      : []),
    ...(chart.artifactHubUrl ? [{ name: 'Artifact Hub', url: chart.artifactHubUrl }] : []),
    ...(detail.links || []),
    ...(chart.sources || []).map((url) => ({ name: url, url })),
  ].filter(
    (link, index, all) => link.url && all.findIndex((item) => item.url === link.url) === index,
  )
  const security = [
    { label: 'Critical', value: chart.securityCritical },
    { label: 'High', value: chart.securityHigh },
    { label: 'Medium', value: chart.securityMedium },
    { label: 'Low', value: chart.securityLow },
    { label: zh ? '未分类' : 'Unknown severity', value: chart.securityUnknown },
  ]
  const hasSecurity = security.some((item) => typeof item.value === 'number')

  return (
    <Card size="small" className="soha-detail-card">
      <WorkloadOverviewConfiguration
        items={[
          { label: 'Chart', children: chart.name },
          {
            label: zh ? '仓库' : 'Repository',
            children: chart.repositoryDisplay || chart.repositoryName || unavailable,
          },
          {
            label: zh ? '仓库地址' : 'Repository URL',
            children: chart.repositoryUrl ? (
              <ExternalLink value={chart.repositoryUrl} />
            ) : (
              unavailable
            ),
          },
          { label: zh ? '所选版本' : 'Selected version', children: version || unavailable },
          {
            label: zh ? '应用版本' : 'App version',
            children: selectedVersion?.appVersion || chart.appVersion || unavailable,
          },
          ...(chart.kubeVersion ? [{ label: 'Kubernetes', children: chart.kubeVersion }] : []),
          {
            label: zh ? '维护者' : 'Maintainers',
            children: chart.maintainers?.length ? (
              <div style={{ display: 'grid', gap: 4 }}>
                {chart.maintainers.map((maintainer, index) => (
                  <div key={`${maintainer.name}-${maintainer.email}-${index}`}>
                    <ExternalLink
                      value={maintainer.url}
                      label={maintainer.name || maintainer.email || maintainer.url || unavailable}
                    />
                    {maintainer.name && maintainer.email ? ` · ${maintainer.email}` : null}
                  </div>
                ))}
              </div>
            ) : (
              unavailable
            ),
          },
          {
            label: zh ? '安全扫描' : 'Security scan',
            children: hasSecurity ? (
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px 16px' }}>
                {security.map(({ label, value }) => (
                  <Typography.Text
                    key={label}
                    type={
                      (label === 'Critical' || label === 'High') &&
                      typeof value === 'number' &&
                      value > 0
                        ? 'danger'
                        : undefined
                    }
                  >
                    {label}: {value ?? (zh ? '未知' : 'Unknown')}
                  </Typography.Text>
                ))}
              </div>
            ) : (
              <Typography.Text type="secondary">
                {zh ? '未提供扫描结果' : 'Scan results not provided'}
              </Typography.Text>
            ),
          },
          {
            label: zh ? '相关链接' : 'Related links',
            children: links.length ? (
              <div style={{ display: 'grid', gap: 6 }}>
                {links.map((link) => (
                  <ExternalLink key={link.url} value={link.url} label={link.name} />
                ))}
              </div>
            ) : (
              unavailable
            ),
          },
        ]}
      />
    </Card>
  )
}

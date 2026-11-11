import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { formatBytes } from '@/components/resource-metrics-format'
import { buildCompactChartSpec, compactMetricColors } from '@/components/resource-metrics-panel'
import { useI18n } from '@/i18n'
import { resolveThemeColorReference } from '@/theme/app-theme'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { ReloadOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { LineChart } from '@visactor/react-vchart'
import { Alert, Button, Card, Typography } from 'antd'
import { ProxyInstanceHeader, useProxyContext } from './context'
import { proxyQueries } from './queries'
import './styles.css'
export default function ProxyOverviewPage() {
  const { localeCode, t } = useI18n()
  const context = useProxyContext('network_access.proxy_instances.view')
  const selected = context.selected
  const traffic = useQuery(proxyQueries.traffic(selected?.id, context.allowed))
  const visibleTrafficData = traffic.isEnabled === false ? undefined : readableQueryData(traffic)
  const samples = visibleTrafficData?.samples ?? []
  const latest = samples[samples.length - 1]
  const points = samples.map((sample) => ({
    timestamp: sample.observedAt,
    upload: sample.uploadBytesPerSecond,
    download: sample.downloadBytesPerSecond,
  }))
  const rates = [
    {
      key: 'upload',
      label: t('networkAccess.proxyRuntime.uploadRate', '上传速率'),
      color: compactMetricColors.default,
      points: points.map((point) => ({ timestamp: point.timestamp, value: point.upload })),
      unit: 'B/s',
    },
    {
      key: 'download',
      label: t('networkAccess.proxyRuntime.downloadRate', '下载速率'),
      color: compactMetricColors.networkRx,
      points: points.map((point) => ({ timestamp: point.timestamp, value: point.download })),
      unit: 'B/s',
    },
  ]
  const chartSpec = buildCompactChartSpec(rates, 'B/s', localeCode)
  chartSpec.background = resolveThemeColorReference('var(--soha-bg-surface, #fff)', '#fff')
  chartSpec.axes[0].label.formatMethod = (value: unknown) => String(value).slice(-5)
  if (!context.allowed) {
    return <ManagementState kind={context.permissions.isLoading ? 'loading' : 'no-permission'} />
  }
  if (context.instances.isLoading) return <ManagementState kind="loading" />
  if (context.instances.isError && !context.instances.data) {
    return (
      <ManagementState
        kind="error"
        actions={
          <Button onClick={() => void context.instances.refetch()}>
            {t('networkAccess.refresh', '刷新')}
          </Button>
        }
      />
    )
  }
  if (!selected) {
    return (
      <ManagementState
        kind="empty"
        description={t(
          'networkAccess.proxyRuntime.noInstances',
          '尚无代理实例。请先在代理实例页面创建并登记运行时。',
        )}
      />
    )
  }
  const unavailable = !visibleTrafficData && traffic.isError
  return (
    <main className="soha-server-proxy-page">
      <ReadFeedback canRead={traffic.isEnabled} query={traffic} label="流量采样" />

      <ProxyInstanceHeader
        instances={context.instances.data ?? []}
        selected={selected}
        onSelect={context.select}
      />
      <div className="soha-server-proxy-actions">
        <Typography.Text type="secondary">
          {t('networkAccess.proxyRuntime.sampledAt', '最近采样')}：
          {latest ? formatDateTime(latest.observedAt) : '—'}
        </Typography.Text>
        <Button
          icon={<ReloadOutlined />}
          loading={traffic.isFetching}
          onClick={() => void (traffic.isEnabled === false ? undefined : traffic.refetch())}
        >
          {t('networkAccess.refresh', '刷新')}
        </Button>
      </div>
      {traffic.isError && visibleTrafficData ? (
        <Alert
          type="warning"
          showIcon
          title={t('networkAccess.proxyRuntime.stale', '刷新失败，当前显示上次采样。')}
        />
      ) : null}
      {unavailable ? (
        <ManagementState
          kind="error"
          description={t(
            'networkAccess.proxyRuntime.trafficError',
            '代理流量暂不可用，请检查运行时和控制面后重试。',
          )}
        />
      ) : traffic.isLoading ? (
        <ManagementState kind="loading" />
      ) : !visibleTrafficData?.supported ? (
        <ManagementState
          kind="empty"
          description={t(
            'networkAccess.proxyRuntime.trafficUnsupported',
            '该实例尚未报告流量能力。',
          )}
        />
      ) : (
        <>
          <div className="soha-server-proxy-metrics">
            <Card size="small">
              <span>{t('networkAccess.proxyRuntime.uploadTotal', '本次运行上传')}</span>
              <strong>{latest ? formatBytes(latest.uploadTotal) : '—'}</strong>
              <small>
                {latest
                  ? `${formatBytes(latest.uploadBytesPerSecond)}/s`
                  : t('networkAccess.proxyRuntime.noSample', '未采集')}
              </small>
            </Card>
            <Card size="small">
              <span>{t('networkAccess.proxyRuntime.downloadTotal', '本次运行下载')}</span>
              <strong>{latest ? formatBytes(latest.downloadTotal) : '—'}</strong>
              <small>
                {latest
                  ? `${formatBytes(latest.downloadBytesPerSecond)}/s`
                  : t('networkAccess.proxyRuntime.noSample', '未采集')}
              </small>
            </Card>
            <Card size="small">
              <span>{t('networkAccess.proxyRuntime.activeConnections', '活动连接')}</span>
              <strong>{latest?.activeConnections ?? '—'}</strong>
              <small>
                {selected.capabilities.includes('connections')
                  ? t('networkAccess.proxyRuntime.sampleCount', '最新快照')
                  : t('networkAccess.proxyRuntime.unsupported', '不支持逐连接采集')}
              </small>
            </Card>
            <Card size="small">
              <span>{t('networkAccess.proxyRuntime.revisions', '配置版本')}</span>
              <strong>
                {selected.observedRevision} / {selected.desiredRevision}
              </strong>
              <small>{t('networkAccess.proxyRuntime.appliedDesired', '实测 / 期望')}</small>
            </Card>
          </div>
          <Card
            className="soha-server-proxy-chart"
            title={t('networkAccess.proxyRuntime.trafficTrend', '流量速率趋势')}
            size="small"
          >
            <Typography.Text type="secondary">
              {t(
                'networkAccess.proxyRuntime.trendScope',
                '最近一小时 · 由相邻累计计数计算，运行时重启后重新计速',
              )}
            </Typography.Text>
            {samples.length >= 2 ? (
              <div
                className="soha-server-proxy-chart-canvas"
                role="img"
                aria-label={t('networkAccess.proxyRuntime.trafficTrend', '流量速率趋势')}
              >
                <LineChart spec={chartSpec} />
              </div>
            ) : (
              <ManagementState
                compact
                kind="empty"
                description={t(
                  'networkAccess.proxyRuntime.waitSamples',
                  '等待至少两个采样点后显示趋势。',
                )}
              />
            )}
          </Card>
        </>
      )}
    </main>
  )
}

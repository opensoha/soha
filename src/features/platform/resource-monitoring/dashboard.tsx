import { Card, Empty, Typography } from 'antd'
import type { UseQueryResult } from '@tanstack/react-query'
import { Link, useLocation } from 'react-router-dom'
import { localeText, useI18n } from '@/i18n'
import { StatusTag } from '@/components/status-tag'
import { formatMetricValue } from '@/components/resource-metrics-format'
import { MonitoringSparkline } from './chart'
import { dashboardMetrics, monitoringSummary } from './dashboard-model'
import { monitoringViews, qualityLabels, qualityStatus } from './model'
import type { MonitoringView, MonitoringViewName } from './types'

export interface DashboardView {
  key: MonitoringViewName
  data?: MonitoringView
  query: Pick<UseQueryResult<MonitoringView>, 'isPending' | 'isError'>
}

/** Totals always cover the complete loaded scope, independently of chart selection. */
export function MonitoringDashboard({
  views,
  historical,
}: {
  views: DashboardView[]
  historical: boolean
}) {
  const { localeCode, t } = useI18n()
  const location = useLocation()
  const source = views.find((view) => view.data)?.data
  const names = source?.nodes.map((node) => node.nodeRef.name) ?? []
  if (!source) return views.some((view) => view.query.isPending) ? <Card loading /> : null
  if (!names.length)
    return (
      <Empty description={localeText(localeCode, '当前范围没有节点', 'No nodes in this scope')} />
    )
  return (
    <section
      className="soha-monitoring-summary"
      aria-label={localeText(localeCode, '当前范围资源汇总', 'Resource totals in this scope')}
    >
      {[...dashboardMetrics]
        .sort((a, b) => {
          const order = ['cpu', 'memory', 'pods', 'conntrack']
          return order.indexOf(a.view) - order.indexOf(b.view)
        })
        .map((metric) => {
          const view =
            views.find((view) => view.key === metric.view) ??
            views.find((view) => view.key === 'overview')!
          const summary = view.data && monitoringSummary(view.data, metric, names, historical)
          const label = monitoringViews.find((item) => item.key === metric.view)![
            localeCode === 'zh_CN' ? 'label' : 'englishLabel'
          ]
          const canOpen = views.some((view) => view.key === metric.view)
          return (
            <Card key={metric.view} className={`soha-monitoring-summary-card is-${metric.tone}`}>
              <div className="soha-monitoring-metric-heading">
                {canOpen ? (
                  <Link to={`/resource-monitoring${location.search}#monitoring-${metric.view}`}>
                    {label}
                  </Link>
                ) : (
                  <Typography.Text strong>{label}</Typography.Text>
                )}
                <Typography.Text type="secondary">
                  {summary?.ratio == null ? '—' : `${summary.ratio.toFixed(1)}%`}
                </Typography.Text>
              </div>
              <div className="soha-monitoring-metric-value">
                {summary?.value == null ? '—' : formatMetricValue(summary.value, metric.unit)}
              </div>
              <div className="soha-monitoring-summary-capacity">
                <Typography.Text type="secondary">
                  {summary?.denominator == null
                    ? localeText(
                        localeCode,
                        historical && metric.view === 'cpu' ? '历史容量未提供' : '采样或分母不足',
                        historical && metric.view === 'cpu'
                          ? 'Historical capacity unavailable'
                          : 'Samples or denominator unavailable',
                      )
                    : `${localeText(localeCode, metric.view === 'pods' ? '可分配' : '容量', metric.view === 'pods' ? 'Allocatable' : 'Capacity')} ${formatMetricValue(summary.denominator, metric.unit)}`}
                </Typography.Text>
                {summary && summary.quality !== 'valid' && (
                  <StatusTag
                    value={qualityStatus[summary.quality] || 'unknown'}
                    label={t(
                      `resourceMonitoring.quality.${summary.quality}`,
                      qualityLabels[summary.quality] || summary.quality,
                    )}
                  />
                )}
              </div>
              {summary?.points.some((point) => point.value != null) ? (
                <MonitoringSparkline
                  key={`${source.clusterId}:${location.search}`}
                  points={summary.points}
                  animated={!historical}
                  label={`${label} ${localeText(localeCode, '范围合计趋势；缺失点不连线', 'scope total trend; gaps are not connected')}`}
                  tone={metric.tone}
                  unit={metric.unit}
                />
              ) : (
                <div className="soha-monitoring-sparkline soha-monitoring-no-trend">
                  {localeText(localeCode, '历史样本不足', 'Historical samples unavailable')}
                </div>
              )}
              {view.query.isError && (
                <Typography.Text type="warning">
                  {localeText(
                    localeCode,
                    '刷新失败 · 上次结果',
                    'Refresh failed · Previous result',
                  )}
                </Typography.Text>
              )}
            </Card>
          )
        })}
    </section>
  )
}

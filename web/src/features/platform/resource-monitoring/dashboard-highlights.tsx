import { Typography } from 'antd'
import { StatusTag } from '@/components/status-tag'
import { formatMetricValue } from '@/components/resource-metrics-format'
import { localeText, useI18n } from '@/i18n'
import {
  monitoringAllocations,
  monitoringPanelSummary,
  monitoringPodCapacity,
} from './dashboard-model'
import { monitoringRatio, qualityLabels, qualityStatus } from './model'
import type { MonitoringView, MonitoringViewName } from './types'

const valueText = (value: number | null | undefined, unit: string) =>
  value == null ? '—' : formatMetricValue(value, unit)

export function MonitoringHighlights({
  data,
  view,
  names,
}: {
  data: MonitoringView
  view: MonitoringViewName
  names: string[]
}) {
  const { localeCode, t } = useI18n()
  if (view === 'cpu' || view === 'memory') {
    const unit = view === 'cpu' ? 'cores' : 'bytes'
    const totals = monitoringAllocations(data, names, view === 'cpu' ? 'cpuCores' : 'memoryBytes')
    return (
      <dl className="soha-monitoring-highlights">
        {(['requests', 'limits'] as const).map((key) => {
          const ratio = monitoringRatio(totals[key], totals.allocatable)
          return (
            <div key={key}>
              <dt>
                {localeText(localeCode, `当前 ${key} / 可分配`, `Current ${key} / allocatable`)}
              </dt>
              <dd>
                {key === 'limits' && totals.limits == null ? (
                  localeText(
                    localeCode,
                    '上限未完整设置或采集不足',
                    'Unlimited resources / incomplete coverage',
                  )
                ) : (
                  <Typography.Text type={ratio != null && ratio > 100 ? 'danger' : undefined}>
                    {valueText(totals[key], unit)} / {valueText(totals.allocatable, unit)}
                    {ratio != null && ` · ${ratio.toFixed(1)}%`}
                  </Typography.Text>
                )}
              </dd>
            </div>
          )
        })}
      </dl>
    )
  }
  if (view === 'conntrack') {
    return (
      <dl className="soha-monitoring-highlights">
        {['drop', 'early_drop', 'insert_failed'].map((event) => {
          const summary = monitoringPanelSummary(data, `conntrack.${event}.rate`, names)
          return (
            <div key={event}>
              <dt>{event} /s</dt>
              <dd>
                {valueText(summary.value, 'events/s')}{' '}
                {summary.quality !== 'valid' && (
                  <StatusTag
                    value={qualityStatus[summary.quality] || 'unknown'}
                    label={t(
                      `resourceMonitoring.quality.${summary.quality}`,
                      qualityLabels[summary.quality] || summary.quality,
                    )}
                  />
                )}
              </dd>
            </div>
          )
        })}
      </dl>
    )
  }
  if (view === 'pods') {
    const totals = monitoringPodCapacity(data, names)
    return (
      <dl className="soha-monitoring-highlights">
        <div>
          <dt>
            {localeText(
              localeCode,
              '当前已选节点绑定 Pending',
              'Current bound Pending on selected nodes',
            )}
          </dt>
          <dd>{valueText(totals.pending, 'count')}</dd>
        </div>
        <div>
          <dt>
            {localeText(localeCode, '当前剩余 / 溢出槽位', 'Current remaining / overflow slots')}
          </dt>
          <dd>
            {valueText(totals.remaining, 'count')} / {valueText(totals.overflow, 'count')}
          </dd>
        </div>
        <div>
          <dt>
            {localeText(localeCode, '当前集群未调度 Pending', 'Current cluster unbound Pending')}
          </dt>
          <dd>{valueText(data.unscheduledPendingPods, 'count')}</dd>
        </div>
      </dl>
    )
  }
  return null
}

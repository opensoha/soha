import { buildCompactChartSpec, type CompactChartLine } from '@/components/resource-metrics-panel'
import { useI18n } from '@/i18n'
import { LineChart } from '@visactor/react-vchart'
import { Card, Segmented, theme, Typography } from 'antd'
import { formatJobDuration } from '../shared/job-execution'
import { cronExecutionMetrics } from './cronjob-detail-model'
import type { CronJobChildJob } from './types'
import '../shared/detail-observability.css'

export function CronJobDetailMetrics({
  jobs,
  activeJobs,
  rangeMinutes,
  observedAt,
  onRangeChange,
}: {
  jobs?: CronJobChildJob[]
  activeJobs?: number
  rangeMinutes: number
  observedAt: number
  onRangeChange: (value: number) => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { token } = theme.useToken()
  const metrics = cronExecutionMetrics(jobs, rangeMinutes, observedAt)
  const cards = [
    {
      key: 'started',
      title: zh ? '执行开始' : 'Executions started',
      value: metrics.started,
      points: metrics.points('started'),
      unit: 'count',
    },
    {
      key: 'success',
      title: zh ? '执行成功' : 'Succeeded executions',
      value: metrics.succeeded,
      points: metrics.points('succeeded'),
      unit: 'count',
    },
    {
      key: 'failure',
      title: zh ? '执行失败' : 'Failed executions',
      value: metrics.failed,
      points: metrics.points('failed'),
      unit: 'count',
    },
    {
      key: 'duration',
      title: zh ? '平均执行耗时' : 'Mean execution duration',
      value: metrics.meanDuration,
      points: metrics.durations || [],
      unit: 'seconds',
    },
    {
      key: 'rate',
      title: zh ? '执行成功率' : 'Execution success rate',
      value: metrics.rate,
      points: metrics.points('rate'),
      unit: '%',
    },
    {
      key: 'active',
      title: zh ? '当前活跃 Jobs' : 'Current active Jobs',
      value: activeJobs,
      points: [],
      unit: 'count',
    },
  ]
  return (
    <div className="soha-pod-detail-metrics soha-cronjob-metrics">
      <div className="soha-pod-detail-metrics-toolbar">
        <Typography.Text type="secondary" className="soha-workload-metric-scope">
          {zh
            ? `当前 UID 保留的 ${jobs?.length ?? '—'} 个 Jobs · 所选窗口内累计`
            : `${jobs?.length ?? '—'} retained Jobs for current UID · Cumulative in selected window`}
        </Typography.Text>
        <Segmented
          aria-label={zh ? '执行统计时间范围' : 'Execution metric range'}
          value={rangeMinutes}
          onChange={(v) => onRangeChange(Number(v))}
          options={[
            { value: 15, label: zh ? '15 分钟' : '15 min' },
            { value: 60, label: zh ? '1 小时' : '1 hour' },
            { value: 360, label: zh ? '6 小时' : '6 hours' },
            { value: 1440, label: zh ? '24 小时' : '24 hours' },
          ]}
        />
      </div>
      <div className="soha-pod-detail-metrics-grid">
        {cards.map((card) => {
          const format = (value: number) =>
            card.unit === 'seconds'
              ? formatJobDuration(value)
              : card.unit === '%'
                ? `${value.toFixed(1)}%`
                : String(value)
          const lines: CompactChartLine[] = card.points.length
            ? [
                {
                  key: card.key,
                  label: card.title,
                  color:
                    card.key === 'failure'
                      ? 'var(--soha-danger)'
                      : card.key === 'success'
                        ? 'var(--soha-success)'
                        : 'var(--soha-primary)',
                  unit: card.unit,
                  points: card.points,
                },
              ]
            : []
          const chart = lines.length ? buildCompactChartSpec(lines, card.unit, localeCode) : null
          if (chart) {
            chart.point.visible = card.points.length === 1
            chart.background = 'transparent'
            chart.height = 166
            chart.legends = { visible: false }
            chart.padding = { top: 8, right: 8, bottom: 24, left: 0 }
            chart.axes[0].tickCount = 3
            chart.axes[0].label.formatMethod = (label: string) =>
              rangeMinutes >= 1440 ? label : label.split(' ').at(-1) || label
            chart.axes[1].min = 0
            chart.axes[1].label.formatMethod = (sample: unknown) => format(Number(sample))
            if (card.unit === '%') chart.axes[1].max = 100
            chart.axes.forEach((axis: { label: { style: { fill: string } } }) => {
              axis.label.style.fill = token.colorTextSecondary
            })
            chart.tooltip.dimension.content[0].value = (datum: {
              rawValue?: number
              value?: number
            }) => format(Number(datum.rawValue ?? datum.value))
          }
          return (
            <Card key={card.key} className="soha-pod-detail-metric-card" title={card.title}>
              <strong className="soha-cronjob-metric-value">
                {card.value === undefined ? '—' : format(card.value)}
              </strong>
              {chart ? (
                <LineChart spec={chart} />
              ) : (
                <div className="soha-cronjob-metric-empty">
                  <Typography.Text type="secondary">
                    {card.key === 'active'
                      ? zh
                        ? '当前调度器快照，未采集历史活跃时序'
                        : 'Current scheduler snapshot; no historical active series'
                      : zh
                        ? '当前窗口没有可用的执行时间记录'
                        : 'No execution time records in this window'}
                  </Typography.Text>
                </div>
              )}
            </Card>
          )
        })}
      </div>
      <Typography.Paragraph type="secondary">
        {zh
          ? '按原生开始与终态时间统计，已清理的 Jobs 不计入；无终态不推断成功/失败，失败 Pods 数量不计作失败执行。'
          : 'Uses native start and terminal times. Deleted Jobs are excluded; missing terminal conditions are not inferred from Pod counts.'}
        {Boolean(metrics.unknown) &&
          (zh
            ? ` ${metrics.unknown} 条执行缺少开始时间、终态时间或状态。`
            : ` ${metrics.unknown} executions lack a start time, terminal time, or state.`)}
      </Typography.Paragraph>
    </div>
  )
}

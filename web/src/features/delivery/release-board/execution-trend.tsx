import { Button, Tooltip, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useI18n } from '@/i18n'
import { formatStatusLabel } from '@/i18n/status'
import { formatDateTime } from '@/utils/time'
import { deliveryQueries } from '../queries'
import type { WorkflowCatalogEntry } from '../types'
import {
  durationLabel,
  executionPoints,
  executionTone,
  type ExecutionPoint,
} from './execution-trend-model'

export function definitionHistoryPath(card: WorkflowCatalogEntry) {
  const search = new URLSearchParams()
  if (card.sourceKind === 'delivery_workflow') search.set('workflowId', card.sourceId)
  else {
    search.set('applicationId', card.scopes[0].applicationId)
    search.set(
      card.sourceKind === 'build_source' ? 'buildSourceId' : 'applicationEnvironmentId',
      card.sourceId,
    )
  }
  return `/execution-history?${search}`
}

export function ExecutionTrendPlot({ points }: { points: ExecutionPoint[] }) {
  const { localeCode } = useI18n()
  const english = localeCode === 'en_US'
  const maximum = Math.max(1, ...points.map((point) => point.duration ?? 0))
  return (
    <div
      className="soha-execution-trend__plot"
      role="group"
      aria-label={english ? 'Recent executions, oldest to newest' : '最近执行，从旧到新'}
    >
      {points.map((point, index) => {
        const label = `${index + 1} · ${formatStatusLabel(point.status, localeCode)} · ${durationLabel(point.duration, english)} · ${formatDateTime(point.createdAt)}${point.version ? ` · ${point.version}` : ''}`
        return (
          <Tooltip key={point.id} title={label} trigger={['hover', 'focus']}>
            <Link
              to={point.href}
              aria-label={label}
              className={`soha-execution-trend__point soha-execution-trend--${executionTone(point.status)}`}
            >
              <span
                aria-hidden="true"
                className="soha-execution-trend__fill"
                style={{
                  height:
                    point.duration === null ? 42 : Math.max(4, (point.duration / maximum) * 48),
                }}
              />
            </Link>
          </Tooltip>
        )
      })}
    </div>
  )
}

export function ExecutionTrend({ card }: { card: WorkflowCatalogEntry }) {
  const english = useI18n().localeCode === 'en_US'
  const applicationId = card.scopes[0]?.applicationId
  const builds = useQuery({
    ...deliveryQueries.builds.list(
      { applicationId, buildSourceId: card.sourceId, limit: 10 },
      card.sourceKind === 'build_source',
    ),
    refetchInterval: 5000,
  })
  const workflows = useQuery(
    deliveryQueries.workflows.list(
      { applicationId, applicationEnvironmentId: card.sourceId, limit: 10 },
      { enabled: card.sourceKind === 'application_workflow', refetchInterval: 5000 },
    ),
  )
  const batches = useQuery(
    deliveryQueries.batches.list(
      { workflowId: card.sourceId, limit: 10 },
      { enabled: card.sourceKind === 'delivery_workflow', refetchInterval: 5000 },
    ),
  )
  const query =
    card.sourceKind === 'build_source'
      ? builds
      : card.sourceKind === 'application_workflow'
        ? workflows
        : batches
  const points = executionPoints(query.data ?? [], Date.now())
  return (
    <div className="soha-execution-trend">
      <div className="soha-execution-trend__heading">
        <Typography.Text type="secondary">
          {english ? 'Last 10 executions' : '最近 10 次执行'}
        </Typography.Text>
      </div>
      {query.isPending ? (
        <div className="soha-execution-trend__empty">
          {english ? 'Loading history…' : '正在读取记录…'}
        </div>
      ) : query.isError ? (
        <div className="soha-execution-trend__empty">
          {english ? 'History unavailable' : '记录加载失败'}
          <Button type="link" size="small" onClick={() => void query.refetch()}>
            {english ? 'Retry' : '重试'}
          </Button>
        </div>
      ) : points.length ? (
        <ExecutionTrendPlot points={points} />
      ) : (
        <div className="soha-execution-trend__empty">
          {english ? 'No executions yet' : '尚未执行'}
        </div>
      )}
    </div>
  )
}

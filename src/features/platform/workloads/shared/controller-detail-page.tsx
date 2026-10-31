import { useI18n } from '@/i18n'
import { useInlineApiError } from '@/components/request-feedback'
import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Spin } from 'antd'
import { lazy, Suspense } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { useWorkloadDetailScope } from './detail-scope'
import { WorkloadDetailShell } from './detail-shell'
import { ControllerOverview } from './controller-overview'
import { WorkloadDetailEvents } from './detail-events'
import { workloadQueries } from './queries'
import type { WorkloadKind } from './types'

const Metrics = lazy(async () => ({
  default: (await import('./detail-metrics')).WorkloadDetailMetrics,
}))

/** The six controller resources share reading behavior while their actual facts stay resource-specific. */
export function ControllerDetailPage({
  resource,
  title,
  paramKey,
  metrics = false,
}: {
  resource: Exclude<WorkloadKind, 'pods' | 'deployments'>
  title: string
  paramKey: string
  metrics?: boolean
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const name = useParams()[paramKey] || ''
  const scope = useWorkloadDetailScope()
  const [active, changeTab] = useResourceDetailTab()
  const [params, setParams] = useSearchParams()
  const requestedRange = Number(params.get('range') || 60)
  const range = [15, 60, 360, 1440].includes(requestedRange) ? requestedRange : 60
  const eventsOptions = workloadQueries.events(resource, scope, name)
  const events = useQuery({
    ...eventsOptions,
    enabled: Boolean(eventsOptions.enabled && active === 'events'),
  })
  const metricsOptions = workloadQueries.metrics(resource, scope, name, range)
  const metricQuery = useQuery({
    ...metricsOptions,
    enabled: Boolean(metrics && metricsOptions.enabled && active === 'metrics'),
  })
  useInlineApiError(active === 'events' ? events.error : null)
  useInlineApiError(active === 'metrics' ? metricQuery.error : null)
  const identity = JSON.stringify([resource, scope.clusterId, scope.namespace, name])
  return (
    <WorkloadDetailShell
      title={title}
      resource={resource}
      paramKey={paramKey}
      activeTabKey={active}
      onTabChange={changeTab}
      yamlLast
      extraOverview={(detail) => (
        <ControllerOverview detail={detail} resource={resource} title={title} />
      )}
      extraTabPanes={[
        {
          key: 'events',
          label: zh ? '事件' : 'Events',
          children:
            active === 'events' ? (
              <WorkloadDetailEvents
                key={identity}
                resourceLabel={title}
                events={readableQueryData(events) || []}
                observedAt={events.dataUpdatedAt || Date.now()}
                loading={events.isLoading}
                error={events.error?.message}
                onRefresh={() => void events.refetch()}
              />
            ) : null,
        },
        ...(metrics
          ? [
              {
                key: 'metrics',
                label: zh ? '指标' : 'Metrics',
                children:
                  active === 'metrics' ? (
                    <Suspense fallback={<Spin />}>
                      <Metrics
                        mode="workload"
                        data={readableQueryData(metricQuery)}
                        loading={metricQuery.isLoading}
                        errorMessage={metricQuery.error?.message}
                        rangeMinutes={range}
                        onRangeChange={(value) =>
                          setParams(
                            (previous) => {
                              const next = new URLSearchParams(previous)
                              next.set('range', String(value))
                              return next
                            },
                            { replace: true },
                          )
                        }
                        onRefresh={() => void metricQuery.refetch()}
                      />
                    </Suspense>
                  ) : null,
              },
            ]
          : []),
      ]}
    />
  )
}

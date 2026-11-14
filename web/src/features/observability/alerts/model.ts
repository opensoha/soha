import type { ObservabilityQuerySnapshot } from '@opensoha/contracts/gen/ts/sohaapi'
import { buildLogExplorerPath } from '../logs/model'
import type { AlertEvent } from './types'

export function alertDisplayStatus(alert?: { status?: string; currentState?: string }) {
  return alert?.currentState || alert?.status || ''
}

export function stringifyAlertPayload(payload: unknown) {
  return JSON.stringify(payload ?? {}, null, 2)
}

export function alertDiagnosticPaths(event: AlertEvent, snapshot?: ObservabilityQuerySnapshot) {
  const scope = snapshot?.context.scope
  const filter = snapshot?.context.filter
  const range = snapshot?.context.timeRange
  const workload =
    scope?.workload || event.labels?.workload || event.labels?.deployment || event.labels?.app
  const service = scope?.service || event.labels?.service
  const traceId = snapshot?.traceId || filter?.traceId
  const spanId = snapshot?.spanId || filter?.spanId
  const params = new URLSearchParams()
  for (const [key, value] of [
    ['cluster', scope?.clusterId || event.clusterId],
    ['namespace', scope?.namespace || event.namespace],
    ['environment', scope?.environment],
    ['service', service],
    ['workload', workload],
    ['dataSourceId', snapshot?.dataSourceId],
    ['from', range?.from || event.startsAt],
    ['to', range?.to || event.lastSeenAt],
    ['metricKey', snapshot?.metricKey],
    ['traceId', traceId],
    ['spanId', spanId],
  ] as const) {
    if (value) params.set(key, value)
  }
  const search = params.toString()
  const path = (name: string) => `/monitoring-workbench/${name}${search ? `?${search}` : ''}`

  return {
    dashboards: path('dashboards'),
    logs: buildLogExplorerPath({
      clusterId: scope?.clusterId || event.clusterId,
      namespace: scope?.namespace || event.namespace,
      environmentId: scope?.environment,
      service,
      workloadName: workload,
      text: filter?.text,
      traceId,
      spanId,
      from: range?.from || event.startsAt,
      to: range?.to || event.lastSeenAt,
    }),
    metrics: path('metrics'),
    traces: path('traces'),
  }
}

import type {
  ObservabilityQueryScope,
  ObservabilityTraceSpan,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type ExploreSignal = 'logs' | 'metrics' | 'traces'

export function observabilityScope(
  clusterId: string | null,
  namespace: string | null,
  service?: string,
  workload?: string,
): ObservabilityQueryScope | undefined {
  const scope = {
    clusterId: clusterId?.trim() || undefined,
    namespace: namespace?.trim() || undefined,
    service: service?.trim() || undefined,
    workload: workload?.trim() || undefined,
  }
  return Object.values(scope).some(Boolean) ? scope : undefined
}

export function signalSearchParams(
  current: URLSearchParams,
  values: Record<string, number | null | string | undefined>,
) {
  const next = new URLSearchParams(current)
  for (const [key, value] of Object.entries(values)) {
    const normalized = String(value ?? '').trim()
    if (normalized) next.set(key, normalized)
    else next.delete(key)
  }
  return next
}

export interface TraceWaterfallRow {
  leftPercent: number
  span: ObservabilityTraceSpan
  widthPercent: number
}

export function traceWaterfallRows(
  spans: ObservabilityTraceSpan[],
  traceId: string,
): TraceWaterfallRow[] {
  const selected = spans
    .filter((span) => span.traceId === traceId && Number.isFinite(Date.parse(span.startTime)))
    .sort((left, right) => Date.parse(left.startTime) - Date.parse(right.startTime))
  if (selected.length === 0) return []
  const start = Date.parse(selected[0]!.startTime)
  const end = Math.max(
    ...selected.map((span) => Date.parse(span.startTime) + Math.max(0, span.durationMs)),
  )
  const duration = Math.max(1, end - start)
  return selected.map((span) => {
    const leftPercent = ((Date.parse(span.startTime) - start) / duration) * 100
    return {
      span,
      leftPercent,
      widthPercent: Math.min(
        100 - leftPercent,
        Math.max(0.6, (Math.max(0, span.durationMs) / duration) * 100),
      ),
    }
  })
}

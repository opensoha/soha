import type {
  ObservabilityDashboardPanel,
  ObservabilityDashboardPanelQueryInput,
  ObservabilityDashboardVariable,
} from '@opensoha/contracts/gen/ts/sohaapi'

const defaultRangeMinutes = 60
const maxRangeMs = 7 * 24 * 60 * 60 * 1000

export interface DashboardPlaybackWindow {
  from: string
  rangeMinutes: number
  to: string
}

export function readDashboardPlayback(
  params: URLSearchParams,
  now = Date.now(),
): DashboardPlaybackWindow {
  const from = Date.parse(params.get('from') ?? '')
  const to = Date.parse(params.get('to') ?? '')
  if (Number.isFinite(from) && Number.isFinite(to) && from < to && to - from <= maxRangeMs) {
    return {
      from: new Date(from).toISOString(),
      rangeMinutes: Math.max(1, Math.round((to - from) / 60_000)),
      to: new Date(to).toISOString(),
    }
  }
  const requestedRange = Number(params.get('range'))
  const rangeMinutes = [15, 60, 360, 1440].includes(requestedRange)
    ? requestedRange
    : defaultRangeMinutes
  return {
    from: new Date(now - rangeMinutes * 60_000).toISOString(),
    rangeMinutes,
    to: new Date(now).toISOString(),
  }
}

export function shiftDashboardPlayback(
  window: DashboardPlaybackWindow,
  direction: -1 | 1,
  now = Date.now(),
): DashboardPlaybackWindow {
  const from = Date.parse(window.from)
  const to = Date.parse(window.to)
  const duration = to - from
  const nextTo = direction > 0 ? Math.min(now, to + duration) : to - duration
  return {
    from: new Date(nextTo - duration).toISOString(),
    rangeMinutes: window.rangeMinutes,
    to: new Date(nextTo).toISOString(),
  }
}

export function dashboardVariableValues(
  definitions: ObservabilityDashboardVariable[],
  params: URLSearchParams,
) {
  return Object.fromEntries(
    definitions.map((definition) => {
      const requested = params.get(`var-${definition.name}`) ?? ''
      const value = definition.options.includes(requested)
        ? requested
        : definition.options.includes(definition.current ?? '')
          ? (definition.current ?? '')
          : (definition.options[0] ?? '')
      return [definition.name, value]
    }),
  )
}

export function dashboardPlaybackParams(
  current: URLSearchParams,
  window: DashboardPlaybackWindow,
  variables: Record<string, string>,
) {
  const next = new URLSearchParams(current)
  next.set('from', window.from)
  next.set('to', window.to)
  next.set('range', String(window.rangeMinutes))
  for (const [name, value] of Object.entries(variables)) {
    if (value) next.set(`var-${name}`, value)
    else next.delete(`var-${name}`)
  }
  return next
}

export function dashboardPanelQueryInput(
  window: DashboardPlaybackWindow,
  variables: Record<string, string>,
  requestedStepSeconds?: number,
): ObservabilityDashboardPanelQueryInput {
  const defaultStepSeconds = window.rangeMinutes <= 60 ? 60 : 300
  const stepSeconds =
    Number.isInteger(requestedStepSeconds) &&
    requestedStepSeconds !== undefined &&
    requestedStepSeconds >= 1 &&
    requestedStepSeconds <= 3600
      ? requestedStepSeconds
      : defaultStepSeconds
  return {
    timeFrom: window.from,
    timeTo: window.to,
    stepSeconds,
    variables,
  }
}

export function dashboardPanelAlertRulePath(
  dashboardName: string,
  dataSourceId: string,
  panel: ObservabilityDashboardPanel,
  input: ObservabilityDashboardPanelQueryInput,
) {
  const rangeMs = Date.parse(input.timeTo) - Date.parse(input.timeFrom)
  const query = expandGrafanaBuiltins(
    expandDashboardVariables(panel.targets[0]?.expression ?? '', input.variables ?? {}),
    rangeMs,
    input.stepSeconds ?? 60,
  )
  const rangeMinutes = Math.max(1, Math.round(rangeMs / 60_000))
  const params = new URLSearchParams({
    create: 'dashboard-panel',
    dataSourceId,
    name: `${dashboardName} / ${panel.title}`.slice(0, 200),
    query,
    stepSeconds: String(input.stepSeconds ?? 60),
    windowMinutes: String(rangeMinutes),
  })
  return `/monitoring-workbench/rules?${params.toString()}`
}

export function dashboardURLVariables(params: URLSearchParams) {
  return Object.fromEntries(
    [...params.entries()]
      .filter(([key, value]) => key.startsWith('var-') && key.length > 4 && value)
      .map(([key, value]) => [key.slice(4), value]),
  )
}

function expandDashboardVariables(expression: string, variables: Record<string, string>) {
  return expression.replace(
    /\$(?:\{([A-Za-z_][A-Za-z0-9_]*)\}|([A-Za-z_][A-Za-z0-9_]*))/g,
    (match, braced: string | undefined, plain: string | undefined) =>
      variables[braced ?? plain ?? ''] ?? match,
  )
}

function expandGrafanaBuiltins(expression: string, rangeMs: number, stepSeconds: number) {
  const rangeSeconds = Math.max(1, Math.round(rangeMs / 1000))
  const interval = prometheusDuration(Math.max(1, stepSeconds))
  const rateInterval = prometheusDuration(Math.max(60, stepSeconds * 4))
  return [
    ['${__rate_interval}', rateInterval],
    ['$__rate_interval', rateInterval],
    ['${__range_ms}', String(rangeSeconds * 1000)],
    ['$__range_ms', String(rangeSeconds * 1000)],
    ['${__range_s}', String(rangeSeconds)],
    ['$__range_s', String(rangeSeconds)],
    ['${__interval}', interval],
    ['$__interval', interval],
    ['${__range}', prometheusDuration(rangeSeconds)],
    ['$__range', prometheusDuration(rangeSeconds)],
  ].reduce((result, [from, to]) => result.split(from!).join(to!), expression)
}

function prometheusDuration(seconds: number) {
  if (seconds % 3600 === 0) return `${seconds / 3600}h`
  if (seconds % 60 === 0) return `${seconds / 60}m`
  return `${seconds}s`
}

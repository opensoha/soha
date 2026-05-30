import type { WorkbenchSession } from './workbench-types'

export type AIWorkbenchMode = NonNullable<WorkbenchSession['metadata']>['mode']

const AI_WORKBENCH_CONTEXT_KEYS = [
  'session',
  'clusterId',
  'namespace',
  'workload',
  'alertId',
  'timeRangeMinutes',
  'rootCauseRunId',
  'agentRunId',
  'inspectionRunId',
  'artifactRunId',
] as const

function asSearchParams(search?: string | URLSearchParams | null) {
  if (search instanceof URLSearchParams) {
    return new URLSearchParams(search)
  }
  if (!search) {
    return new URLSearchParams()
  }
  return new URLSearchParams(search.startsWith('?') ? search.slice(1) : search)
}

export function normalizeAIWorkbenchMode(mode?: string | null): AIWorkbenchMode {
  switch (mode) {
    case 'root_cause':
    case 'performance':
    case 'trace':
    case 'inspection_review':
      return mode
    default:
      return 'general'
  }
}

export function buildAIWorkbenchContextSearch(search?: string | URLSearchParams | null) {
  const source = asSearchParams(search)
  const next = new URLSearchParams()
  AI_WORKBENCH_CONTEXT_KEYS.forEach((key) => {
    const value = source.get(key)
    if (value) {
      next.set(key, value)
    }
  })
  return next
}

export function pathWithSearch(pathname: string, search?: string | URLSearchParams | null) {
  const params = asSearchParams(search)
  const suffix = params.toString()
  return suffix ? `${pathname}?${suffix}` : pathname
}

export function getAIWorkbenchPathForMode(
  mode?: string | null,
  search?: string | URLSearchParams | null,
) {
  const next = buildAIWorkbenchContextSearch(search)
  const normalizedMode = normalizeAIWorkbenchMode(mode ?? asSearchParams(search).get('mode'))

  if (normalizedMode === 'root_cause') {
    next.delete('mode')
    return pathWithSearch('/ai-workbench/root-cause', next)
  }
  if (normalizedMode === 'performance') {
    next.delete('mode')
    return pathWithSearch('/ai-workbench/performance', next)
  }
  if (normalizedMode === 'trace' || normalizedMode === 'inspection_review') {
    next.set('mode', normalizedMode)
  } else {
    next.delete('mode')
  }
  return pathWithSearch('/ai-workbench/chat', next)
}

export function getAIWorkbenchPathForSession(
  session: Pick<WorkbenchSession, 'id' | 'metadata'>,
  search?: string | URLSearchParams | null,
) {
  const next = buildAIWorkbenchContextSearch(search)
  next.set('session', session.id)
  return getAIWorkbenchPathForMode(session.metadata?.mode, next)
}

export function getAIOperationsPath(search?: string | URLSearchParams | null) {
  return pathWithSearch('/ai-workbench/inspection', buildAIWorkbenchContextSearch(search))
}

export function getAIToolsPath(search?: string | URLSearchParams | null) {
  return pathWithSearch('/ai-workbench/tool-settings', buildAIWorkbenchContextSearch(search))
}

export function getAIModelSettingsPath(search?: string | URLSearchParams | null) {
  return pathWithSearch('/ai-workbench/model-settings', buildAIWorkbenchContextSearch(search))
}

import type {
  WorkbenchLaunchContext as ContractWorkbenchLaunchContext,
  WorkbenchSelectionContext as ContractWorkbenchSelectionContext,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type { WorkbenchSessionScope } from '../workbench/types'

export type AIWorkbenchSource = ContractWorkbenchLaunchContext['sourceWorkbench']

export type AISelectedTextKind = ContractWorkbenchSelectionContext['kind']

export interface AIPageContext extends ContractWorkbenchLaunchContext {
  selectedText?: string
  selectedTextKind?: AISelectedTextKind
  promptHint?: string
}

export type AISelectionContext = ContractWorkbenchSelectionContext

export type AIGlobalAssistantAction =
  | 'open'
  | 'analyze-page'
  | 'troubleshoot-resource'
  | 'explain-selection'
  | 'troubleshoot-selection'
  | 'summarize-selection'
  | 'next-steps-selection'
  | 'freeform'

export interface AIGlobalAssistantLaunchRequest {
  action: AIGlobalAssistantAction
  prompt?: string
  contextOverride?: Partial<AIPageContext>
  selection?: AISelectionContext
}

export interface AIGlobalAssistantMessage {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  status?: 'loading' | 'success' | 'error' | 'abort'
}

export const DEFAULT_AI_PAGE_CONTEXT: AIPageContext = {
  sourceWorkbench: 'ai',
  sourceRoute: '/',
  sourceTitle: 'Soha',
}

export const MAX_AI_SELECTION_TEXT_LENGTH = 12000
export const AI_CONTEXT_DATA_ATTRIBUTE = 'data-ai-context'
const RESOURCE_CONTEXT_KEYS = [
  'workload',
  'service',
  'pod',
  'node',
  'alertId',
  'applicationId',
  'releaseBundleId',
  'dockerHostId',
  'dockerServiceId',
  'virtualizationConnectionId',
  'vmId',
] as const

export function compactRecord<T extends Record<string, unknown>>(record: T): Partial<T> {
  return Object.fromEntries(
    Object.entries(record).filter(
      ([, value]) => value !== undefined && value !== null && value !== '',
    ),
  ) as Partial<T>
}

export function workbenchScopeFromAIContext(context: AIPageContext): WorkbenchSessionScope {
  return compactRecord({
    clusterId: context.clusterId,
    namespace: context.namespace,
    workload: context.workload,
    service: context.service,
    pod: context.pod,
    node: context.node,
    alertId: context.alertId,
    timeRangeMinutes: context.timeRangeMinutes,
  }) as WorkbenchSessionScope
}

export function pinnedContextFromAIContext(context: AIPageContext) {
  return compactRecord({
    sourceWorkbench: context.sourceWorkbench,
    sourceRoute: context.sourceRoute,
    sourceTitle: context.sourceTitle,
    entityKind: context.entityKind,
    entityName: context.entityName,
    clusterId: context.clusterId,
    namespace: context.namespace,
    workload: context.workload,
    service: context.service,
    pod: context.pod,
    node: context.node,
    alertId: context.alertId,
    applicationId: context.applicationId,
    releaseBundleId: context.releaseBundleId,
    dockerHostId: context.dockerHostId,
    dockerServiceId: context.dockerServiceId,
    virtualizationConnectionId: context.virtualizationConnectionId,
    vmId: context.vmId,
    timeRangeMinutes: context.timeRangeMinutes,
    visibleFilters: context.visibleFilters,
    pinnedData: context.pinnedData,
  }) as Record<string, unknown>
}

export function mergeAIPageContext(
  base: AIPageContext | undefined,
  override?: Partial<AIPageContext>,
): AIPageContext {
  const merged = {
    ...DEFAULT_AI_PAGE_CONTEXT,
    ...(base ?? {}),
    ...(override ?? {}),
    pinnedData: compactRecord({
      ...(base?.pinnedData ?? {}),
      ...(override?.pinnedData ?? {}),
    }),
    visibleFilters: compactRecord({
      ...(base?.visibleFilters ?? {}),
      ...(override?.visibleFilters ?? {}),
    }),
  }
  const hasResourceOverride = override
    ? RESOURCE_CONTEXT_KEYS.some((key) => Object.prototype.hasOwnProperty.call(override, key)) ||
      Object.prototype.hasOwnProperty.call(override, 'entityKind') ||
      Object.prototype.hasOwnProperty.call(override, 'entityName')
    : false

  if (hasResourceOverride) {
    RESOURCE_CONTEXT_KEYS.forEach((key) => {
      if (!Object.prototype.hasOwnProperty.call(override, key)) {
        delete merged[key]
      }
    })
  }

  return merged
}

export function sanitizeSelectionText(text: string, maxLength = MAX_AI_SELECTION_TEXT_LENGTH) {
  const normalized = text
    .replace(/\u00a0/g, ' ')
    .replace(/\r\n?/g, '\n')
    .trim()
    .replace(/(authorization\s*:\s*bearer\s+)[^\s]+/gi, '$1[REDACTED]')
    .replace(
      /((?:password|passwd|secret|token|api[_-]?key)\s*[:=]\s*)["']?[^"'\s,;]+["']?/gi,
      '$1[REDACTED]',
    )

  if (normalized.length <= maxLength) {
    return normalized
  }
  return `${normalized.slice(0, maxLength)}\n... [truncated ${normalized.length - maxLength} chars]`
}

export function inferSelectionKind(text: string): AISelectedTextKind {
  const value = text.trim()
  if (!value) return 'plain'
  if (/^(\s*apiVersion:|\s*kind:|\s*metadata:)/m.test(value)) return 'yaml'
  if (/(error|exception|panic|failed|timeout|crashloopbackoff|oomkilled)/i.test(value))
    return 'error'
  if (/(warning|normal|reason:|lastTimestamp|event)/i.test(value)) return 'event'
  if (/(p\d{2}|latency|duration|qps|rps|cpu|memory|metric|rate\(|sum\()/i.test(value))
    return 'metric'
  if (/\d{4}-\d{2}-\d{2}T|\b(INFO|WARN|ERROR|DEBUG|TRACE)\b/.test(value)) return 'log'
  return 'plain'
}

export function contextDisplayName(context: AIPageContext) {
  return context.entityName || context.sourceTitle || context.sourceRoute || '当前页面'
}

export function contextIdentityKey(context: AIPageContext) {
  return JSON.stringify(
    compactRecord({
      sourceWorkbench: context.sourceWorkbench,
      sourceRoute: context.sourceRoute,
      entityKind: context.entityKind,
      entityName: context.entityName,
      clusterId: context.clusterId,
      namespace: context.namespace,
      workload: context.workload,
      service: context.service,
      pod: context.pod,
      node: context.node,
      alertId: context.alertId,
      applicationId: context.applicationId,
      releaseBundleId: context.releaseBundleId,
      dockerHostId: context.dockerHostId,
      dockerServiceId: context.dockerServiceId,
      virtualizationConnectionId: context.virtualizationConnectionId,
      vmId: context.vmId,
    }),
  )
}

export function encodeAIContextForElement(context: Partial<AIPageContext>) {
  return JSON.stringify(compactRecord(context as Record<string, unknown>))
}

export function decodeAIContextFromElement(
  element: Element | null,
): Partial<AIPageContext> | undefined {
  const raw = element?.getAttribute(AI_CONTEXT_DATA_ATTRIBUTE)
  if (!raw) return undefined
  try {
    const parsed = JSON.parse(raw) as unknown
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return undefined
    return parsed as Partial<AIPageContext>
  } catch {
    return undefined
  }
}

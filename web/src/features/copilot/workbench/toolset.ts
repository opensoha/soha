import { z } from 'zod'

import type {
  WorkbenchAdapter,
  WorkbenchDataSource,
  WorkbenchSessionScope,
  WorkbenchSessionToolset,
} from './types'

export const DEFAULT_RECOMMENDED_ADAPTER_IDS = [
  'platform-native.v1',
  'logs.v1',
  'metrics.v1',
  'traces.v1',
]

export const TOOLSET_BUDGET_FIELDS = [
  {
    key: 'timeoutSeconds',
    label: 'Timeout Seconds',
    suffix: 's',
    description: '单次分析工具运行超时。',
  },
  {
    key: 'maxEvidenceItems',
    label: 'Max Evidence Items',
    suffix: 'items',
    description: '进入分析工件的证据数量上限。',
  },
  {
    key: 'maxQueries',
    label: 'Max Queries',
    suffix: 'queries',
    description: '预留给后续多工具编排的查询次数预算。',
  },
  {
    key: 'maxLogBytes',
    label: 'Max Log Bytes',
    suffix: 'bytes',
    description: '预留给日志工具的读取字节预算。',
  },
] as const

const toolsetStringListSchema = z.array(z.string().trim().min(1))
const toolsetBudgetOverridesSchema = z
  .object({
    timeoutSeconds: z.number().positive().finite().optional(),
    maxEvidenceItems: z.number().positive().finite().optional(),
    maxQueries: z.number().positive().finite().optional(),
    maxLogBytes: z.number().positive().finite().optional(),
  })
  .strict()
const toolsetScopeOverridesSchema = z
  .object({
    clusterId: z.string().trim().min(1).optional(),
    namespace: z.string().trim().min(1).optional(),
    workload: z.string().trim().min(1).optional(),
    service: z.string().trim().min(1).optional(),
    alertId: z.string().trim().min(1).optional(),
    timeRangeMinutes: z.number().positive().finite().optional(),
  })
  .strict()
const workbenchToolsetPayloadSchema = z
  .object({
    enabledAdapterIds: toolsetStringListSchema,
    enabledSkillIds: toolsetStringListSchema,
    disabledToolNames: toolsetStringListSchema,
    budgetOverrides: toolsetBudgetOverridesSchema,
    scopeOverrides: toolsetScopeOverridesSchema,
  })
  .strict()

export function validateWorkbenchToolsetPayload(
  toolset: WorkbenchSessionToolset,
): WorkbenchSessionToolset {
  return workbenchToolsetPayloadSchema.parse(toolset)
}

export function numberRecord(value?: Record<string, unknown>) {
  const out: Record<string, number> = {}
  Object.entries(value ?? {}).forEach(([key, raw]) => {
    const numberValue = Number(raw)
    if (Number.isFinite(numberValue) && numberValue > 0) {
      out[key] = numberValue
    }
  })
  return out
}

export function scopeOverrideState(value?: Record<string, unknown>) {
  const out: Partial<WorkbenchSessionScope> = {}
  const clusterId = String(value?.clusterId ?? '').trim()
  const namespace = String(value?.namespace ?? '').trim()
  const workload = String(value?.workload ?? '').trim()
  const service = String(value?.service ?? '').trim()
  const alertId = String(value?.alertId ?? '').trim()
  const timeRangeMinutes = Number(value?.timeRangeMinutes)
  if (clusterId) out.clusterId = clusterId
  if (namespace) out.namespace = namespace
  if (workload) out.workload = workload
  if (service) out.service = service
  if (alertId) out.alertId = alertId
  if (Number.isFinite(timeRangeMinutes) && timeRangeMinutes > 0)
    out.timeRangeMinutes = timeRangeMinutes
  return out
}

export function pruneEmptyStringList(items: string[]) {
  return [...new Set(items.map((item) => item.trim()).filter(Boolean))]
}

export function qualifiedToolName(adapterId: string, toolName: string) {
  return `${adapterId}.${toolName}`
}

function isQualifiedToolName(value: string, adapters: WorkbenchAdapter[]) {
  return adapters.some((adapter) => value.startsWith(`${adapter.id}.`))
}

export function canonicalDisabledToolNames(values: string[], adapters: WorkbenchAdapter[]) {
  return pruneEmptyStringList(values).map((value) => {
    if (isQualifiedToolName(value, adapters)) return value
    const matches = adapters.filter((adapter) =>
      (adapter.tools ?? []).some((tool) => tool.name === value),
    )
    return matches.length === 1 ? qualifiedToolName(matches[0].id, value) : value
  })
}

export function buildDisabledToolOptions(adapters: WorkbenchAdapter[]) {
  return adapters.flatMap((adapter) =>
    (adapter.tools ?? []).map((tool) => ({
      value: qualifiedToolName(adapter.id, tool.name),
      label: `${adapter.name} / ${tool.name}`,
    })),
  )
}

export function recommendedAdapterIds(
  adapters: WorkbenchAdapter[],
  dataSources: WorkbenchDataSource[],
) {
  const registered = new Set(adapters.map((item) => item.id))
  const enabledAdapterSet = new Set(
    dataSources.filter((item) => item.enabled && item.mcpAdapter).map((item) => item.mcpAdapter),
  )
  const fromDataSources = [...enabledAdapterSet]
  const platformNative = registered.has('platform-native.v1') ? ['platform-native.v1'] : []
  return pruneEmptyStringList([
    ...platformNative,
    ...fromDataSources,
    ...DEFAULT_RECOMMENDED_ADAPTER_IDS.filter(
      (item) =>
        item !== 'platform-native.v1' && registered.has(item) && enabledAdapterSet.has(item),
    ),
  ])
}

export function cleanToolsetPayload(toolset: WorkbenchSessionToolset): WorkbenchSessionToolset {
  const budgetOverrides = numberRecord(toolset.budgetOverrides)
  const scopeOverrides = scopeOverrideState(toolset.scopeOverrides)
  return validateWorkbenchToolsetPayload({
    enabledAdapterIds: pruneEmptyStringList(toolset.enabledAdapterIds ?? []),
    enabledSkillIds: pruneEmptyStringList(toolset.enabledSkillIds ?? []),
    disabledToolNames: pruneEmptyStringList(toolset.disabledToolNames ?? []),
    budgetOverrides,
    scopeOverrides,
  })
}

export function countObjectKeys(value?: Record<string, unknown>) {
  return Object.keys(value ?? {}).filter((key) => {
    const raw = value?.[key]
    return raw !== undefined && raw !== null && String(raw).trim() !== ''
  }).length
}

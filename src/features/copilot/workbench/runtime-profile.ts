import type { WorkbenchModelPreferences } from '@opensoha/contracts/gen/ts/sohaapi'

// Additive fields from the Workbench runtime profile API until the pinned SDK includes them.
export type WorkbenchRuntimeScope = 'chat' | 'heya'

export interface WorkbenchRuntimeAssetReference {
  kind: 'mcp' | 'skill' | 'hook'
  id: string
  version: string
  required: boolean
}

export interface WorkbenchRuntimeProfile {
  id: string
  revision: number
  name: string
  scope: WorkbenchRuntimeScope
  enabled: boolean
  assets: WorkbenchRuntimeAssetReference[]
  modelPreferences?: WorkbenchModelPreferences
  maxOutputTokens?: number
}

export type WorkbenchRuntimeProfileUpdate = Omit<WorkbenchRuntimeProfile, 'id' | 'revision'> & {
  expectedRevision: number
}

type RuntimeAssetStatus = Omit<WorkbenchRuntimeAssetReference, 'required'> & {
  status: 'loaded' | 'unavailable'
  code?: string
  capabilities?: string[]
}

export interface WorkbenchRuntimeAssembly {
  profileId: string
  revision: number
  status: 'preparing' | 'confirmed' | 'degraded' | 'rejected'
  assets: RuntimeAssetStatus[]
}

export const runtimeAssemblyLabels: Record<WorkbenchRuntimeAssembly['status'], string> = {
  preparing: '准备中',
  confirmed: '装配已确认',
  degraded: '已降级',
  rejected: '已拒绝',
}

export const runtimeHookOutcomeLabels = {
  completed: '已完成',
  denied: '已拒绝',
  failed: '失败',
  canceled: '已取消',
  unknown: '结果未知',
}

interface RuntimeHookOutcome {
  id: string
  version: string
  callId: string
  toolName: string
  outcome: keyof typeof runtimeHookOutcomeLabels
}

export function runtimeHookOutcomes(value: unknown): RuntimeHookOutcome[] {
  if (!Array.isArray(value)) return []
  return value.filter(
    (item): item is RuntimeHookOutcome =>
      item &&
      typeof item.id === 'string' &&
      /^[a-z0-9][a-z0-9-]{0,47}$/.test(item.id) &&
      typeof item.version === 'string' &&
      /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(item.version) &&
      typeof item.callId === 'string' &&
      item.callId.length > 0 &&
      typeof item.toolName === 'string' &&
      /^[A-Za-z0-9_.:/-]{1,128}$/.test(item.toolName) &&
      typeof item.outcome === 'string' &&
      Object.prototype.hasOwnProperty.call(runtimeHookOutcomeLabels, item.outcome),
  )
}

export function runtimeAssemblyFromMetadata(value: unknown): WorkbenchRuntimeAssembly | undefined {
  if (!value || typeof value !== 'object') return
  const assembly = value as Partial<WorkbenchRuntimeAssembly>
  if (
    typeof assembly.profileId === 'string' &&
    Number.isSafeInteger(assembly.revision) &&
    typeof assembly.status === 'string' &&
    Object.prototype.hasOwnProperty.call(runtimeAssemblyLabels, assembly.status) &&
    Array.isArray(assembly.assets) &&
    assembly.assets.every(
      (asset) =>
        asset &&
        ['mcp', 'skill', 'hook'].includes(asset.kind) &&
        typeof asset.id === 'string' &&
        typeof asset.version === 'string' &&
        ['loaded', 'unavailable'].includes(asset.status),
    )
  ) {
    return assembly as WorkbenchRuntimeAssembly
  }
}

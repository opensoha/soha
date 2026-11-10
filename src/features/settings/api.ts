import { api } from '@/services/api-client'
import { ApiError } from '@/services/api-error'
import type { ApiResponse, BrandingSettings } from '@/types'
import type { LLMModelRoute, WorkbenchAgentRun, WorkbenchCatalog } from '@/features/copilot'
import type { WorkbenchRuntimeProfile, WorkbenchRuntimeProfileUpdate } from '@/features/copilot'
import type {
  AISettings,
  AISkillSetting,
  AIWorkbenchModelSettings,
  AnalysisProfile,
  DataSource,
} from './ai-settings-model'
import type {
  DataSourceCapability,
  IdentitySettingsResponse,
  SaveIdentitySettingsInput,
  WorkbenchRuntimeAsset,
  WorkbenchRuntimeAssetProbe,
} from './types'

async function unwrap<T>(request: Promise<ApiResponse<T>>): Promise<T> {
  const response = await request
  return response.data
}

export interface UpsertSettingsRecordInput {
  id?: string
  values: Record<string, unknown>
}

const runtimeAssetsPath = '/settings/ai/workbench-runtime-assets'
type RuntimeAssetIdentity = Pick<WorkbenchRuntimeAsset, 'kind' | 'id' | 'version'>

function runtimeAssetPath({ kind, id, version }: RuntimeAssetIdentity) {
  return `${runtimeAssetsPath}/${kind}/${encodeURIComponent(id)}/${encodeURIComponent(version)}`
}

export const settingsApi = {
  branding: {
    get: () => unwrap(api.get<ApiResponse<BrandingSettings>>('/settings/branding')),
    save: (values: BrandingSettings) => api.put<void>('/settings/branding', values),
    upload: (formData: FormData) =>
      unwrap(api.upload<ApiResponse<{ url: string }>>('/settings/branding/upload', formData)),
  },
  identity: {
    get: () => unwrap(api.get<ApiResponse<IdentitySettingsResponse>>('/settings/identity')),
    save: (input: SaveIdentitySettingsInput) =>
      api.put<void>('/settings/identity/providers', input.values),
  },
  ai: {
    runtimeAssets: async () => {
      const items = await unwrap(api.get<ApiResponse<WorkbenchRuntimeAsset[]>>(runtimeAssetsPath))
      if (!Array.isArray(items)) {
        throw new ApiError(502, '未收到有效的运行资产目录', {
          method: 'GET',
          path: runtimeAssetsPath,
        })
      }
      return items
    },
    setRuntimeAssetEnabled: async ({
      enabled,
      ...asset
    }: RuntimeAssetIdentity & { enabled: boolean }) => {
      const path = runtimeAssetPath(asset)
      const updated = await unwrap(api.put<ApiResponse<WorkbenchRuntimeAsset>>(path, { enabled }))
      if (
        updated?.kind !== asset.kind ||
        updated.id !== asset.id ||
        updated.version !== asset.version ||
        updated.enabled !== enabled
      ) {
        throw new ApiError(502, '保存回执与资产版本不匹配', { method: 'PUT', path })
      }
      return updated
    },
    probeRuntimeMCP: async ({
      scope,
      ...asset
    }: Pick<RuntimeAssetIdentity, 'id' | 'version'> & { scope: 'chat' | 'heya' }) => {
      const path = `${runtimeAssetPath({ ...asset, kind: 'mcp' })}/probe`
      const result = await unwrap(
        api.post<ApiResponse<WorkbenchRuntimeAssetProbe>>(path, { scope }),
      )
      if (!result || !['ready', 'unavailable', 'cleanup_unknown'].includes(result.status)) {
        throw new ApiError(502, '未收到有效的探测回执', { method: 'POST', path })
      }
      return result
    },
    runtimeProfileHistory: (id: string) =>
      unwrap(api.get<ApiResponse<WorkbenchRuntimeProfile[]>>(
        `/settings/ai/workbench-runtime-profiles/${encodeURIComponent(id)}`,
      )),
    publishRuntimeProfile: async ({ id, profile }: { id: string; profile: WorkbenchRuntimeProfileUpdate }) => {
      const path = `/settings/ai/workbench-runtime-profiles/${encodeURIComponent(id)}`
      const published = await unwrap(api.put<ApiResponse<WorkbenchRuntimeProfile>>(path, profile))
      if (published?.id !== id || published.revision !== profile.expectedRevision + 1) {
        throw new ApiError(502, '发布回执与运行配置版本不匹配', { method: 'PUT', path })
      }
      return published
    },
    get: () => unwrap(api.get<ApiResponse<AISettings>>('/settings/ai')),
    skills: () =>
      unwrap(
        api.get<ApiResponse<{ skillsRegistry: AISkillSetting[] }>>('/settings/ai/skills'),
      ),
    modelRoutes: () =>
      unwrap(
        api.get<ApiResponse<LLMModelRoute[]>>(
          '/ai-gateway/relay/model-routes?includeDisabled=true',
        ),
      ),
    dataSources: () => unwrap(api.get<ApiResponse<DataSource[]>>('/copilot/data-sources')),
    analysisProfiles: () =>
      unwrap(api.get<ApiResponse<AnalysisProfile[]>>('/copilot/analysis-profiles')),
    dataSourceCapabilities: () =>
      unwrap(api.get<ApiResponse<DataSourceCapability[]>>('/copilot/data-source-capabilities')),
    workbenchCatalog: () =>
      unwrap(api.get<ApiResponse<WorkbenchCatalog>>('/copilot/workbench/catalog')),
    agentRuns: () => unwrap(api.get<ApiResponse<WorkbenchAgentRun[]>>('/copilot/agent-runs')),
    saveWorkbenchModel: (workbenchModel: AIWorkbenchModelSettings) =>
      api.put<void>('/settings/ai/workbench-model', { workbenchModel }),
    saveSkills: (skillsRegistry: AISkillSetting[]) =>
      api.put<void>('/settings/ai/skills', { skillsRegistry }),
    upsertDataSource: ({ id, values }: UpsertSettingsRecordInput) =>
      id
        ? api.put<void>(`/copilot/data-sources/${encodeURIComponent(id)}`, values)
        : api.post<void>('/copilot/data-sources', values),
    validateDataSource: (id: string) =>
      unwrap(
        api.post<ApiResponse<DataSource>>(
          `/copilot/data-sources/${encodeURIComponent(id)}/validate`,
        ),
      ),
    upsertAnalysisProfile: ({ id, values }: UpsertSettingsRecordInput) =>
      id
        ? api.put<void>(`/copilot/analysis-profiles/${encodeURIComponent(id)}`, values)
        : api.post<void>('/copilot/analysis-profiles', values),
  },
}

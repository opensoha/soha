import { queryOptions } from '@tanstack/react-query'
import { settingsApi } from './api'
import { settingsKeys } from './keys'
import { normalizeLoginProvider } from './identity/model'
import type { AISettings, AIWorkbenchModelSettings } from './ai-settings-model'

function normalizeWorkbenchModelSettings(
  item?: Partial<AIWorkbenchModelSettings> | null,
): AIWorkbenchModelSettings {
  return {
    defaultPublicModel: String(item?.defaultPublicModel || ''),
    defaultRouteId: String(item?.defaultRouteId || ''),
    defaultEndpoint: String(item?.defaultEndpoint || 'chat/completions'),
    enabled: item?.enabled ?? true,
  }
}

export const settingsQueries = {
  branding: (enabled = true) =>
    queryOptions({
      queryKey: settingsKeys.branding.detail(),
      queryFn: settingsApi.branding.get,
      enabled,
    }),
  identity: (enabled = true) =>
    queryOptions({
      queryKey: settingsKeys.identity.detail(),
      queryFn: async () => {
        const current = await settingsApi.identity.get()
        const providers = Array.isArray(current.providers)
          ? current.providers.map(normalizeLoginProvider)
          : []
        return {
          providers,
          defaultProviderId: current.defaultProviderId || providers[0]?.id || '',
          localPasswordLoginEnabled: current.localPasswordLoginEnabled ?? true,
        }
      },
      enabled,
    }),
  ai: {
    runtimeAssets: (owner: string, enabled = true) =>
      queryOptions({
        queryKey: settingsKeys.ai.runtimeAssets(owner),
        queryFn: settingsApi.ai.runtimeAssets,
        enabled: Boolean(owner) && enabled,
        retry: false,
      }),
    runtimeProfileHistory: (owner: string, id: string) =>
      queryOptions({
        queryKey: settingsKeys.ai.runtimeProfileHistory(owner, id),
        queryFn: () => settingsApi.ai.runtimeProfileHistory(id),
        enabled: Boolean(owner && id),
        retry: false,
      }),
    detail: (enabled = true) =>
      queryOptions({
        queryKey: settingsKeys.ai.detail(),
        queryFn: async () => {
          const current = await settingsApi.ai.get()
          return {
            workbenchModel: normalizeWorkbenchModelSettings(current.workbenchModel),
            skillsRegistry: current.skillsRegistry ?? [],
          } satisfies AISettings
        },
        enabled,
      }),
    skills: (enabled = true) =>
      queryOptions({
        queryKey: settingsKeys.ai.skills(),
        queryFn: async () => (await settingsApi.ai.skills()).skillsRegistry ?? [],
        enabled,
      }),
    modelRoutes: (enabled: boolean) =>
      queryOptions({
        queryKey: settingsKeys.ai.modelRoutes(),
        queryFn: settingsApi.ai.modelRoutes,
        enabled,
      }),
    dataSources: (enabled = true) =>
      queryOptions({
        queryKey: settingsKeys.ai.dataSources(),
        queryFn: settingsApi.ai.dataSources,
        enabled,
      }),
    analysisProfiles: (enabled = true) =>
      queryOptions({
        queryKey: settingsKeys.ai.analysisProfiles(),
        queryFn: settingsApi.ai.analysisProfiles,
        enabled,
      }),
    dataSourceCapabilities: (enabled = true) =>
      queryOptions({
        queryKey: settingsKeys.ai.dataSourceCapabilities(),
        queryFn: settingsApi.ai.dataSourceCapabilities,
        enabled,
      }),
    workbenchCatalog: (enabled: boolean) =>
      queryOptions({
        queryKey: settingsKeys.ai.workbenchCatalog(),
        queryFn: settingsApi.ai.workbenchCatalog,
        enabled,
      }),
    agentRuns: (enabled: boolean) =>
      queryOptions({
        queryKey: settingsKeys.ai.agentRuns(),
        queryFn: settingsApi.ai.agentRuns,
        enabled,
      }),
  },
}

export { normalizeWorkbenchModelSettings }

import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  MarketplacePluginListEnvelope,
  PluginUpdateListEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type {
  InstalledPlugin,
  MarketplacePlugin,
  PluginConfigRequest,
  PluginExtensionRecord,
  PluginInstallRequest,
  PluginManifest,
  PluginMarketplaceFilters,
  PluginRepositoryInstallRequest,
  PluginRepositoryPreview,
  PluginRepositoryPreviewRequest,
  PluginRepositoryRefList,
  PluginRepositoryRefsRequest,
  PluginRepositoryUpgradeRequest,
} from './plugin-model'
import { normalizePluginMarketplaceFilters } from './plugin-model'

function withQuery(path: string, filters: PluginMarketplaceFilters = {}) {
  const params = new URLSearchParams()
  const normalized = normalizePluginMarketplaceFilters(filters)
  if (normalized.query) params.set('q', normalized.query)
  if (normalized.type) params.set('type', normalized.type)
  if (normalized.publisher) params.set('publisher', normalized.publisher)
  if (normalized.sourceId) params.set('sourceId', normalized.sourceId)
  if (normalized.marketplaceUrl) params.set('marketplaceUrl', normalized.marketplaceUrl)
  if (normalized.version) params.set('version', normalized.version)
  const query = params.toString()
  return query ? `${path}?${query}` : path
}

async function unwrap<T>(request: Promise<ApiResponse<T>>): Promise<T> {
  const response = await request
  return response.data
}

export const pluginApi = {
  marketplace: (filters: PluginMarketplaceFilters = {}) =>
    unwrap(api.get<ApiResponse<MarketplacePlugin[]>>(withQuery('/plugins/marketplace', filters))),
  marketplaceWithMeta: async (
    filters: PluginMarketplaceFilters = {},
  ): Promise<MarketplacePluginListEnvelope> => {
    const response = await api.getEnvelope<
      MarketplacePluginListEnvelope | ApiResponse<MarketplacePlugin[]>
    >(withQuery('/plugins/marketplace', filters))
    return 'items' in response ? response : { items: response.data }
  },
  marketplaceDetail: (pluginId: string, filters: PluginMarketplaceFilters = {}) =>
    unwrap(
      api.get<ApiResponse<MarketplacePlugin>>(
        withQuery(`/plugins/marketplace/${encodeURIComponent(pluginId)}`, filters),
      ),
    ),
  installed: () => unwrap(api.get<ApiResponse<InstalledPlugin[]>>('/plugins/installed')),
  updates: async (pluginIds: readonly string[]) => {
    const params = new URLSearchParams(pluginIds.map((id) => ['pluginId', id]))
    return (await api.getEnvelope<PluginUpdateListEnvelope>(`/plugins/updates?${params}`)).items
  },
  installedDetail: (pluginId: string) =>
    unwrap(api.get<ApiResponse<InstalledPlugin>>(`/plugins/${encodeURIComponent(pluginId)}`)),
  manifest: (pluginId: string) =>
    unwrap(
      api.get<ApiResponse<PluginManifest>>(`/plugins/${encodeURIComponent(pluginId)}/manifest`),
    ),
  install: (input: PluginInstallRequest) =>
    unwrap(api.post<ApiResponse<InstalledPlugin>>('/plugins/install', input)),
  repositoryPreview: (input: PluginRepositoryPreviewRequest) =>
    unwrap(api.post<ApiResponse<PluginRepositoryPreview>>('/plugins/repository/preview', input)),
  repositoryRefs: (input: PluginRepositoryRefsRequest, signal: AbortSignal) =>
    unwrap(
      api.postWithSignal<ApiResponse<PluginRepositoryRefList>>(
        '/plugins/repository/refs',
        input,
        signal,
      ),
    ),
  repositoryInstall: (input: PluginRepositoryInstallRequest) =>
    unwrap(api.post<ApiResponse<InstalledPlugin>>('/plugins/repository/install', input)),
  repositoryUpgrade: (pluginId: string, input: PluginRepositoryUpgradeRequest) =>
    unwrap(
      api.post<ApiResponse<InstalledPlugin>>(
        `/plugins/${encodeURIComponent(pluginId)}/repository/upgrade`,
        input,
      ),
    ),
  enable: (pluginId: string) =>
    unwrap(
      api.post<ApiResponse<InstalledPlugin>>(`/plugins/${encodeURIComponent(pluginId)}/enable`),
    ),
  disable: (pluginId: string) =>
    unwrap(
      api.post<ApiResponse<InstalledPlugin>>(`/plugins/${encodeURIComponent(pluginId)}/disable`),
    ),
  upgrade: (pluginId: string, input: PluginInstallRequest) =>
    unwrap(
      api.post<ApiResponse<InstalledPlugin>>(
        `/plugins/${encodeURIComponent(pluginId)}/upgrade`,
        input,
      ),
    ),
  configure: (pluginId: string, input: PluginConfigRequest) =>
    unwrap(
      api.put<ApiResponse<InstalledPlugin>>(
        `/plugins/${encodeURIComponent(pluginId)}/config`,
        input,
      ),
    ),
  remove: (pluginId: string) =>
    unwrap(api.delete<ApiResponse<{ status: string }>>(`/plugins/${encodeURIComponent(pluginId)}`)),
  extensions: (scope = 'runtime') =>
    unwrap(
      api.get<ApiResponse<PluginExtensionRecord[]>>(`/extensions/${encodeURIComponent(scope)}`),
    ),
}

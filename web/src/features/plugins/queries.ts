import { queryOptions } from '@tanstack/react-query'
import { pluginApi } from './plugin-api'
import { pluginKeys } from './keys'
import type { PluginMarketplaceFilters } from './plugin-model'
import { normalizePluginMarketplaceFilters } from './plugin-model'

export const pluginQueries = {
  marketplace: (filters: PluginMarketplaceFilters = {}) => {
    const normalized = normalizePluginMarketplaceFilters(filters)
    return queryOptions({
      queryKey: pluginKeys.marketplaceList(normalized),
      queryFn: () => pluginApi.marketplace(normalized),
    })
  },
  marketplaceDetail: (pluginId: string, filters: PluginMarketplaceFilters = {}, enabled = true) => {
    const id = pluginId.trim()
    const normalized = normalizePluginMarketplaceFilters(filters)
    return queryOptions({
      queryKey: pluginKeys.marketplaceDetail(id, normalized),
      queryFn: () => pluginApi.marketplaceDetail(id, normalized),
      enabled: enabled && Boolean(id),
    })
  },
  installed: (enabled = true) =>
    queryOptions({
      queryKey: pluginKeys.installedList(),
      queryFn: pluginApi.installed,
      enabled,
    }),
  installedDetail: (pluginId: string, enabled = true) => {
    const id = pluginId.trim()
    return queryOptions({
      queryKey: pluginKeys.installedDetail(id),
      queryFn: () => pluginApi.installedDetail(id),
      enabled: enabled && Boolean(id),
    })
  },
  manifest: (pluginId: string, enabled = true) => {
    const id = pluginId.trim()
    return queryOptions({
      queryKey: pluginKeys.manifest(id),
      queryFn: () => pluginApi.manifest(id),
      enabled: enabled && Boolean(id),
    })
  },
  extensions: (scope = 'runtime', enabled = true) => {
    const normalizedScope = scope.trim() || 'runtime'
    return queryOptions({
      queryKey: pluginKeys.extensions(normalizedScope),
      queryFn: () => pluginApi.extensions(normalizedScope),
      enabled,
    })
  },
}

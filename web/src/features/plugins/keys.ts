import type { PluginMarketplaceFilters } from './plugin-model'
import { normalizePluginMarketplaceFilters } from './plugin-model'

export const pluginKeys = {
  all: ['plugins'] as const,
  marketplace: () => [...pluginKeys.all, 'marketplace'] as const,
  marketplaceList: (filters: PluginMarketplaceFilters = {}) =>
    [...pluginKeys.marketplace(), 'list', normalizePluginMarketplaceFilters(filters)] as const,
  marketplaceDetail: (pluginId: string, filters: PluginMarketplaceFilters = {}) =>
    [
      ...pluginKeys.marketplace(),
      'detail',
      pluginId.trim(),
      normalizePluginMarketplaceFilters(filters),
    ] as const,
  installed: () => [...pluginKeys.all, 'installed'] as const,
  installedList: () => [...pluginKeys.installed(), 'list'] as const,
  installedDetail: (pluginId: string) =>
    [...pluginKeys.installed(), 'detail', pluginId.trim()] as const,
  manifest: (pluginId: string) => [...pluginKeys.all, 'manifest', pluginId.trim()] as const,
  extensions: (scope: string) => [...pluginKeys.all, 'extensions', scope.trim()] as const,
}

export const pluginMutationKeys = {
  install: () => [...pluginKeys.all, 'mutation', 'install'] as const,
  lifecycle: (action: 'disable' | 'enable' | 'remove' | 'upgrade') =>
    [...pluginKeys.all, 'mutation', 'lifecycle', action] as const,
  configure: () => [...pluginKeys.all, 'mutation', 'configure'] as const,
}

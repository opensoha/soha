import type {
  InstalledPlugin,
  MarketplacePlugin,
  MarketplacePluginVersion,
  PluginConfigRequest,
  PluginExtensionPoints,
  PluginInstallRequest,
  PluginManifest,
  PluginPermissionRequest,
  PluginSecretRequirement,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type {
  InstalledPlugin,
  MarketplacePlugin,
  MarketplacePluginVersion,
  PluginConfigRequest,
  PluginExtensionPoints,
  PluginInstallRequest,
  PluginManifest,
  PluginPermissionRequest,
  PluginSecretRequirement,
}

export interface PluginMarketplaceFilters {
  marketplaceUrl?: string
  publisher?: string
  query?: string
  sourceId?: string
  type?: string
  version?: string
}

export interface PluginExtensionRecord {
  id: string
  pluginId: string
  pluginName: string
  pluginVersion: string
  point: string
  scope: string
  label?: string
  description?: string
  actionRef?: string
  resourceKinds?: string[]
  permissionKeys?: string[]
  runtimeMode?: string
  status: string
  configured: boolean
  metadata?: Record<string, unknown>
}

export const pluginTypeOptions = [
  { value: 'skill', label: 'Skill' },
  { value: 'skill-pack', label: 'Skill Pack' },
  { value: 'mcp-preset', label: 'MCP Preset' },
  { value: 'connector', label: 'Connector' },
  { value: 'ai-provider-adapter', label: 'AI Provider' },
  { value: 'agent-profile', label: 'Agent Profile' },
  { value: 'gateway-policy-pack', label: 'Policy Pack' },
  { value: 'diagnostic', label: 'Diagnostic' },
  { value: 'resource-extension', label: 'Resource Extension' },
  { value: 'metric-extension', label: 'Metric Extension' },
  { value: 'notification-channel', label: 'Notification Channel' },
  { value: 'identity-template', label: 'Identity Template' },
  { value: 'ui-extension', label: 'UI Extension' },
  { value: 'companion-pack', label: 'Companion Pack' },
] as const

export const pluginRiskLabels: Record<string, string> = {
  read: '只读',
  write: '写入',
  execute: '执行',
  high: '高风险',
  mutate: '变更',
}

export function normalizePluginMarketplaceFilters(
  filters: PluginMarketplaceFilters = {},
): PluginMarketplaceFilters {
  return Object.fromEntries(
    Object.entries(filters)
      .map(([key, value]) => [key, value?.trim()])
      .filter((entry): entry is [string, string] => Boolean(entry[1])),
  )
}

export function pluginTypeLabel(type?: string) {
  const value = String(type ?? '').trim()
  return pluginTypeOptions.find((item) => item.value === value)?.label ?? (value || '-')
}

export function requestedPermissionValues(permissions?: PluginPermissionRequest | null) {
  return [...(permissions?.required ?? []), ...(permissions?.domain ?? [])].filter(Boolean)
}

export function requiredSecretValues(secrets?: { required?: PluginSecretRequirement[] } | null) {
  return (secrets?.required ?? []).filter((item) => item?.name)
}

export function manifestAssetCount(manifest?: PluginManifest | null) {
  const assets = manifest?.assets
  return [
    assets?.skills,
    assets?.mcpPresets,
    assets?.connectors,
    assets?.agentProfiles,
    assets?.gatewayPolicyPacks,
  ].reduce((total, values) => total + (values?.length ?? 0), 0)
}

export function manifestCapabilityCount(manifest?: PluginManifest | null) {
  const capabilities = manifest?.capabilities
  return [
    capabilities?.tools,
    capabilities?.resources,
    capabilities?.prompts,
    capabilities?.skills,
  ].reduce((total, values) => total + (values?.length ?? 0), 0)
}

export function manifestExtensionCount(manifest?: PluginManifest | null) {
  const points = manifest?.extensionPoints
  if (!points) return 0
  return Object.values(points).reduce<number>((total, group) => {
    if (!group || typeof group !== 'object') return total
    return (
      total +
      Object.values(group as Record<string, unknown>).reduce<number>(
        (groupTotal, contributions) =>
          groupTotal + (Array.isArray(contributions) ? contributions.length : 0),
        0,
      )
    )
  }, 0)
}

export function latestMarketplaceVersion(
  plugin: MarketplacePlugin,
): MarketplacePluginVersion | undefined {
  const target = plugin.latestVersion || plugin.version
  return plugin.versions?.find((item) => item.version === target) ?? plugin.versions?.[0]
}

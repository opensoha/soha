import type { RuntimeConfigItem, RuntimeConfigValue } from './types'

export const COMPUTE_CHILD_CONFIG_KEYS = [
  'modules.docker.enabled',
  'modules.virtualization.enabled',
] as const

export type ComputeModuleState = 'disabled' | 'partial' | 'enabled'

export function isComputeChildConfig(key: string) {
  return COMPUTE_CHILD_CONFIG_KEYS.includes(key as (typeof COMPUTE_CHILD_CONFIG_KEYS)[number])
}

export function computeModuleState(
  items: RuntimeConfigItem[],
  draft: Record<string, RuntimeConfigValue>,
): ComputeModuleState {
  const enabledCount = items.filter((item) =>
    Boolean(draft[item.key] ?? item.effectiveValue),
  ).length
  if (enabledCount === 0) return 'disabled'
  if (enabledCount === items.length && items.length === COMPUTE_CHILD_CONFIG_KEYS.length) {
    return 'enabled'
  }
  return 'partial'
}

export function computeModuleDraft(enabled: boolean) {
  return Object.fromEntries(COMPUTE_CHILD_CONFIG_KEYS.map((key) => [key, enabled])) as Record<
    string,
    RuntimeConfigValue
  >
}

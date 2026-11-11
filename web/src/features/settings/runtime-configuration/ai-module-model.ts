import type { RuntimeConfigItem, RuntimeConfigValue } from './types'

export const AI_WORKBENCH_CONFIG_KEY = 'modules.ai.enabled'
export const GLOBAL_ASSISTANT_CONFIG_KEY = 'modules.ai.features.assistant.global'

export const AI_WORKBENCH_CONFIG_KEYS = [
  AI_WORKBENCH_CONFIG_KEY,
  GLOBAL_ASSISTANT_CONFIG_KEY,
] as const

export function isAIWorkbenchConfig(key: string) {
  return AI_WORKBENCH_CONFIG_KEYS.includes(key as (typeof AI_WORKBENCH_CONFIG_KEYS)[number])
}

export function isAIWorkbenchChildConfig(key: string) {
  return key === GLOBAL_ASSISTANT_CONFIG_KEY
}

export function aiWorkbenchEnabled(
  items: RuntimeConfigItem[],
  draft: Record<string, RuntimeConfigValue>,
) {
  const workbench = items.find((item) => item.key === AI_WORKBENCH_CONFIG_KEY)
  return Boolean(draft[AI_WORKBENCH_CONFIG_KEY] ?? workbench?.effectiveValue)
}

export function aiWorkbenchDraft(enabled: boolean): Record<string, RuntimeConfigValue> {
  if (enabled) return { [AI_WORKBENCH_CONFIG_KEY]: true }
  return {
    [AI_WORKBENCH_CONFIG_KEY]: false,
    [GLOBAL_ASSISTANT_CONFIG_KEY]: false,
  }
}

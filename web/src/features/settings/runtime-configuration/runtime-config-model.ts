import type {
  RuntimeConfigApplyMode,
  RuntimeConfigChange,
  RuntimeConfigItem,
  RuntimeConfigValue,
} from './types'

function valuesEqual(left: RuntimeConfigValue | undefined, right: RuntimeConfigValue | undefined) {
  return JSON.stringify(left) === JSON.stringify(right)
}

export function buildRuntimeConfigChanges(
  draft: Record<string, RuntimeConfigValue>,
  resetKeys: ReadonlySet<string>,
  items: RuntimeConfigItem[],
): RuntimeConfigChange[] {
  const itemByKey = new Map(items.map((item) => [item.key, item]))
  const valueChanges = Object.entries(draft)
    .filter(
      ([key, value]) =>
        !resetKeys.has(key) && !valuesEqual(value, itemByKey.get(key)?.effectiveValue),
    )
    .map(([key, value]) => ({ key, value }))
  return [...Array.from(resetKeys, (key) => ({ key, reset: true })), ...valueChanges]
}

export function summarizeRuntimeConfigChanges(
  changes: RuntimeConfigChange[],
  items: RuntimeConfigItem[],
) {
  const itemByKey = new Map(items.map((item) => [item.key, item]))
  const summary: Record<RuntimeConfigApplyMode, number> = {
    hot: 0,
    reconfigure: 0,
    lifecycle: 0,
    restart: 0,
  }
  changes.forEach((change) => {
    const applyMode = itemByKey.get(change.key)?.applyMode
    if (applyMode) summary[applyMode] += 1
  })
  return summary
}

/**
 * Prometheus is configured per cluster. These legacy global keys may still be
 * returned by older server revisions, but must not appear in the global
 * runtime configuration page.
 */
export function isGlobalPrometheusConfig(key: string) {
  return (
    key.startsWith('monitoring.prometheus.') ||
    key === 'monitoring.prometheus_url' ||
    key === 'monitoring.prometheus_bearer_token' ||
    key === 'monitoring.prometheus_default_range_minutes' ||
    key === 'monitoring.prometheus_step_seconds' ||
    key === 'monitoring.prometheus_cluster_label' ||
    key === 'monitoring.grafana_base_url'
  )
}

export function visibleRuntimeConfigItems(items: RuntimeConfigItem[]) {
  return items.filter((item) => !isGlobalPrometheusConfig(item.key))
}

import { describe, expect, it } from 'vitest'
import {
  buildRuntimeConfigChanges,
  isGlobalPrometheusConfig,
  summarizeRuntimeConfigChanges,
  visibleRuntimeConfigItems,
} from './runtime-config-model'
import type { RuntimeConfigItem } from './types'

describe('runtime configuration visibility', () => {
  it('recognizes global and legacy Prometheus keys', () => {
    expect(isGlobalPrometheusConfig('monitoring.prometheus.base_url')).toBe(true)
    expect(isGlobalPrometheusConfig('monitoring.prometheus.bearer_token')).toBe(true)
    expect(isGlobalPrometheusConfig('monitoring.prometheus_url')).toBe(true)
    expect(isGlobalPrometheusConfig('monitoring.grafana_base_url')).toBe(true)
    expect(isGlobalPrometheusConfig('monitoring.enabled')).toBe(false)
    expect(isGlobalPrometheusConfig('monitoring.webhook_token')).toBe(false)
  })

  it('keeps non-Prometheus runtime settings visible', () => {
    const items = [
      { key: 'monitoring.prometheus.base_url' },
      { key: 'monitoring.prometheus.bearer_token' },
      { key: 'modules.monitoring.enabled' },
      { key: 'modules.ai.enabled' },
    ] as RuntimeConfigItem[]

    expect(visibleRuntimeConfigItems(items).map((item) => item.key)).toEqual([
      'modules.monitoring.enabled',
      'modules.ai.enabled',
    ])
  })

  it('emits reset changes without retaining a draft value', () => {
    const items = [
      { key: 'modules.docker.enabled', effectiveValue: true, source: 'runtime_override' },
      { key: 'modules.ai.enabled', effectiveValue: false, source: 'default' },
    ] as RuntimeConfigItem[]

    expect(
      buildRuntimeConfigChanges(
        { 'modules.docker.enabled': false, 'modules.ai.enabled': true },
        new Set(['modules.docker.enabled']),
        items,
      ),
    ).toEqual([
      { key: 'modules.docker.enabled', reset: true },
      { key: 'modules.ai.enabled', value: true },
    ])
  })

  it('summarizes changed settings by apply mode', () => {
    const items = [
      { key: 'hot', applyMode: 'hot' },
      { key: 'module', applyMode: 'lifecycle' },
      { key: 'restart', applyMode: 'restart' },
    ] as RuntimeConfigItem[]

    expect(
      summarizeRuntimeConfigChanges(
        [
          { key: 'hot', value: true },
          { key: 'module', reset: true },
          { key: 'restart', value: 1 },
        ],
        items,
      ),
    ).toEqual({ hot: 1, reconfigure: 0, lifecycle: 1, restart: 1 })
  })
})

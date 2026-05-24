import { describe, expect, it } from 'vitest'
import {
  canonicalDisabledToolNames,
  cleanToolsetPayload,
  recommendedAdapterIds,
} from './workbench-toolset'
import type { WorkbenchAdapter, WorkbenchDataSource } from './workbench-types'

const adapters: WorkbenchAdapter[] = [
  {
    id: 'metrics.v1',
    name: 'Metrics',
    description: 'Prometheus metrics',
    sourceKind: 'metrics',
    tools: [{ name: 'metrics.anomaly_summary', description: 'Run anomaly summary' }],
  },
  {
    id: 'logs.v1',
    name: 'Logs',
    description: 'Loki logs',
    sourceKind: 'logs',
    tools: [{ name: 'logs.error_signatures', description: 'Find error signatures' }],
  },
]

describe('workbench toolset helpers', () => {
  it('normalizes disabled tools, budget overrides, and scope overrides for session persistence', () => {
    expect(canonicalDisabledToolNames(['metrics.anomaly_summary', 'logs.v1.logs.error_signatures'], adapters)).toEqual([
      'metrics.v1.metrics.anomaly_summary',
      'logs.v1.logs.error_signatures',
    ])

    expect(cleanToolsetPayload({
      enabledAdapterIds: [' metrics.v1 ', 'metrics.v1', ''],
      enabledSkillIds: ['root-cause-skill', ''],
      disabledToolNames: ['metrics.v1.metrics.anomaly_summary', ' '],
      budgetOverrides: { timeoutSeconds: '45', maxEvidenceItems: 12, maxQueries: 0 },
      scopeOverrides: { namespace: ' payments-shadow ', workload: '', timeRangeMinutes: '30' },
    })).toEqual({
      enabledAdapterIds: ['metrics.v1'],
      enabledSkillIds: ['root-cause-skill'],
      disabledToolNames: ['metrics.v1.metrics.anomaly_summary'],
      budgetOverrides: { timeoutSeconds: 45, maxEvidenceItems: 12 },
      scopeOverrides: { namespace: 'payments-shadow', timeRangeMinutes: 30 },
    })
  })

  it('recommends registered adapters that are backed by enabled data sources', () => {
    const dataSources: WorkbenchDataSource[] = [
      { id: 'ds-1', name: 'Prometheus', sourceKind: 'metrics', backendType: 'prometheus', enabled: true, mcpAdapter: 'metrics.v1' },
      { id: 'ds-2', name: 'Disabled Loki', sourceKind: 'logs', backendType: 'loki', enabled: false, mcpAdapter: 'logs.v1' },
    ]

    expect(recommendedAdapterIds(adapters, dataSources)).toEqual(['metrics.v1'])
  })
})

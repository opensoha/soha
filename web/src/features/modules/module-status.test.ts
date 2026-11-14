import { describe, expect, it } from 'vitest'
import type { RuntimeMenuNode } from '@/types'
import {
  areWorkbenchModuleFeaturesEnabled,
  filterMenuByModuleFeatures,
  type ModuleStatusEnvelope,
} from './module-status'

const node = (id: string): RuntimeMenuNode => ({
  id,
  path: `/${id}`,
  labelZh: id,
  labelEn: id,
  iconKey: 'menu',
  section: 'ai-engineering',
  sortOrder: 1,
  enabled: true,
})

const status = (features: Record<string, boolean>): ModuleStatusEnvelope =>
  ({
    data: [
      {
        descriptor: { id: 'ai' },
        enabled: true,
        features,
      },
    ],
  }) as ModuleStatusEnvelope

describe('AI module feature visibility', () => {
  it('requires every feature used by a combined workbench leaf', () => {
    const response = status({
      'agent.fleet_rollout': true,
      'agent.conformance_suite': false,
    })
    expect(
      areWorkbenchModuleFeaturesEnabled(response, 'ai', [
        'agent.fleet_rollout',
        'agent.conformance_suite',
      ]),
    ).toBe(false)
    expect(
      filterMenuByModuleFeatures(
        [node('ai-workbench-provider-fleet'), node('ai-workbench-overview')],
        response,
      ).map((item) => item.id),
    ).toEqual(['ai-workbench-overview'])
  })

  it('keeps the leaf once all required features are enabled', () => {
    const response = status({
      'agent.fleet_rollout': true,
      'agent.conformance_suite': true,
    })
    expect(
      filterMenuByModuleFeatures([node('ai-workbench-provider-fleet')], response),
    ).toHaveLength(1)
  })

  it('fails closed for known feature-gated leaves before module status is available', () => {
    expect(
      filterMenuByModuleFeatures(
        [
          node('ai-workbench-memory'),
          node('ai-workbench-environments'),
          node('ai-workbench-overview'),
        ],
        undefined,
      ).map((item) => item.id),
    ).toEqual(['ai-workbench-overview'])
  })

  it('uses the runtime menu ids for memory and environments', () => {
    const response = status({
      'memory.long_term': false,
      'agent.environment_management': false,
    })
    expect(
      filterMenuByModuleFeatures(
        [node('ai-workbench-memory'), node('ai-workbench-environments')],
        response,
      ),
    ).toEqual([])
  })
})

import { expect, it } from 'vitest'
import { relationRulesPayload } from './relation-rules'
import type { ApplicationEnvironment, ResourceRelationConfig } from '../types'

it('freezes the binding revision and preserves other targets and settings', () => {
  const binding: ApplicationEnvironment = {
    id: 'binding',
    createdAt: '2026-10-22T00:00:00Z',
    applicationId: 'app',
    environmentId: 'test',
    updatedAt: '2026-10-22T00:00:00Z',
    workflowTemplateId: 'workflow',
    workflowTemplateVersion: 3,
    buildPolicy: { refType: 'branch', variables: { region: 'test' } },
    targets: [
      {
        id: 'one',
        clusterId: 'cluster',
        namespace: 'test',
        workloadKind: 'CustomResource',
        workloadName: 'one',
        enabled: true,
        metadata: { importSource: 'kubernetes', other: 'keep' },
      },
      {
        id: 'two',
        clusterId: 'cluster',
        namespace: 'test',
        workloadKind: 'Deployment',
        workloadName: 'two',
        enabled: true,
        metadata: { other: 'untouched' },
      },
    ],
  }
  const config: ResourceRelationConfig = { version: 'v1', rootUid: 'root', sources: [], rules: [] }
  const result = relationRulesPayload(binding, 'one', config)
  expect(result.expectedUpdatedAt).toBe(binding.updatedAt)
  expect(result.workflowTemplateId).toBe('workflow')
  expect(result.workflowTemplateVersion).toBe(3)
  expect(result.buildPolicy).toEqual(binding.buildPolicy)
  expect(result.targets?.[0].metadata).toEqual({
    importSource: 'kubernetes',
    other: 'keep',
    resourceRelations: config,
  })
  expect(result.targets?.[1].metadata).toEqual({ other: 'untouched' })
  expect(binding.targets?.[0].metadata).not.toHaveProperty('resourceRelations')
  expect(() => relationRulesPayload({ ...binding, updatedAt: '' }, 'one', config)).toThrow()
  expect(() => relationRulesPayload(binding, 'removed', config)).toThrow()
})

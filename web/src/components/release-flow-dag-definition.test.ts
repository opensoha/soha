import { describe, expect, it } from 'vitest'
import {
  analyzeReleaseDagDefinition,
  createDefaultReleaseDagDefinition,
  createNodeConfig,
  normalizeReleaseDagDefinition,
} from './release-flow-dag-definition'

describe('release flow DAG definition', () => {
  it('stores approval policy fields inside manual approval nodes', () => {
    expect(createNodeConfig('manual_approval')).toEqual({
      approvalMode: 'any',
      approverRoles: ['release-manager'],
      approverUsers: [],
      approverTeams: [],
      required: true,
      requiredApprovals: 1,
      slaMinutes: 60,
      onTimeout: 'block',
      rejectAction: 'stop',
      changeWindow: {
        enabled: false,
        timezone: 'Asia/Shanghai',
        startsAt: '',
        endsAt: '',
      },
    })
  })

  it('creates default release templates with approval node gate configuration', () => {
    const definition = createDefaultReleaseDagDefinition()
    const approvalNode = definition.nodes.find((node) => node.type === 'manual_approval')

    expect(approvalNode?.config).toMatchObject({
      approvalMode: 'any',
      requiredApprovals: 1,
      slaMinutes: 60,
      onTimeout: 'block',
      rejectAction: 'stop',
    })
  })

  it('preserves delivery_dag preview fields while remaining release compatible', () => {
    const definition = normalizeReleaseDagDefinition({
      schemaVersion: 2,
      mode: 'delivery_dag',
      nodes: [
        {
          id: 'build',
          type: 'build',
          name: 'Build image',
          executorKind: 'mcp',
          targetKind: 'ai_test',
          capabilityRef: 'testing.ui.run',
          providerRef: 'external-test-platform',
          inputs: ['source'],
          outputs: ['image'],
          serviceSelector: { keys: ['web'], matchLabels: { service: 'checkout' } },
          inputMapping: { baseUrl: '${environment.url}' },
          artifactKinds: ['test_report', 'screenshot', 'video', 'junit'],
          artifactOutputs: [{ name: 'image', kind: 'image', required: true }],
          runCondition: 'branch == main',
          failurePolicy: 'rollback',
          observability: { events: ['started', 'completed'] },
          config: {},
        },
        { id: 'deploy', type: 'deploy_update_image', name: 'Deploy', targetSelector: { key: 'prod' }, config: {} },
      ],
      edges: [{ id: 'edge-build-deploy', source: 'build', target: 'deploy', condition: 'success' }],
    })
    const analysis = analyzeReleaseDagDefinition(definition)

    expect(definition.mode).toBe('delivery_dag')
    expect(definition.nodes[0]).toMatchObject({
      inputs: ['source'],
      outputs: ['image'],
      executorKind: 'mcp',
      targetKind: 'ai_test',
      capabilityRef: 'testing.ui.run',
      providerRef: 'external-test-platform',
      serviceSelector: { keys: ['web'], matchLabels: { service: 'checkout' } },
      inputMapping: { baseUrl: '${environment.url}' },
      artifactKinds: ['test_report', 'screenshot', 'video', 'junit'],
      artifactOutputs: [{ name: 'image', kind: 'image', required: true }],
      runCondition: 'branch == main',
      failurePolicy: 'rollback',
      observability: { events: ['started', 'completed'] },
    })
    expect(analysis.isDeliveryDag).toBe(true)
    expect(analysis.isReleaseDagCompatible).toBe(true)
    expect(analysis.artifactOutputCount).toBe(1)
    expect(analysis.selectorNodeCount).toBe(2)
    expect(analysis.conditionalNodeCount).toBe(1)
  })
})

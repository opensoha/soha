import { describe, expect, it } from 'vitest'
import { evaluateClusterCapability } from './cluster-capabilities'
import type { ClusterCapabilityMatrixEntry } from '@/types'

const matrix: ClusterCapabilityMatrixEntry[] = [
  {
    key: 'resource.yaml.apply',
    label: 'YAML apply and delete',
    category: 'configuration',
    requiredScopes: ['cluster', 'namespace'],
    riskLevel: 'mutate',
    requiresApproval: true,
    docsUrl: '/operations/agent-runtime',
    direct: { status: 'available' },
    agent: {
      status: 'unsupported',
      reason: 'Agent mode cannot apply YAML yet',
      notes: ['YAML apply and delete are not supported for agent-connected clusters yet'],
    },
  },
  {
    key: 'workload.mutations',
    label: 'Workload mutations',
    category: 'workloads',
    requiredScopes: ['cluster', 'namespace'],
    riskLevel: 'mutate',
    requiresApproval: true,
    docsUrl: '/architecture/authorization',
    direct: { status: 'available' },
    agent: {
      status: 'partial',
      notes: ['deployment restart, rollback, and scale are available through the agent'],
    },
  },
]

describe('evaluateClusterCapability', () => {
  it('disables unsupported agent capabilities and exposes backend notes', () => {
    const decision = evaluateClusterCapability({
      connectionMode: 'agent',
      key: 'resource.yaml.apply',
      localeCode: 'en_US',
      matrix,
    })

    expect(decision.status).toBe('unsupported')
    expect(decision.disabled).toBe(true)
    expect(decision.reason).toBe(
      'Agent mode cannot apply YAML yet / YAML apply and delete are not supported for agent-connected clusters yet',
    )
    expect(decision.requiredScopes).toEqual(['cluster', 'namespace'])
    expect(decision.riskLevel).toBe('mutate')
    expect(decision.requiresApproval).toBe(true)
    expect(decision.docsUrl).toBe('/operations/agent-runtime')
  })

  it('keeps partial capabilities enabled while preserving notes for UI hints', () => {
    const decision = evaluateClusterCapability({
      connectionMode: 'agent',
      key: 'workload.mutations',
      localeCode: 'en_US',
      matrix,
    })

    expect(decision.status).toBe('partial')
    expect(decision.disabled).toBe(false)
    expect(decision.reason).toContain('deployment restart')
    expect(decision.requiresApproval).toBe(true)
  })

  it('preserves known-key partial limits in the Chinese interface', () => {
    const decision = evaluateClusterCapability({
      connectionMode: 'agent',
      key: 'workload.mutations',
      localeCode: 'zh_CN',
      matrix,
    })

    expect(decision.disabled).toBe(false)
    expect(decision.reason).toBe(matrix[1].agent.notes?.[0])
    expect(decision.reason).not.toContain('Pod 删除')
  })

  it.each([
    ['zh_CN', 'partial'],
    ['zh_CN', 'unsupported'],
    ['zh_CN', 'available'],
    ['en_US', 'partial'],
    ['en_US', 'unsupported'],
    ['en_US', 'available'],
  ] as const)(
    'preserves %s %s Agent upgrade and action-policy diagnostics',
    (localeCode, status) => {
      const decision = evaluateClusterCapability({
        connectionMode: 'agent',
        key: 'workload.mutations',
        localeCode,
        matrix: matrix.map((entry) => ({
          ...entry,
          agent: {
            status,
            reason: '  Upgrade the connected Agent  ',
            notes: [
              ' Upgrade the connected Agent ',
              '',
              ' Action platform.pods.delete is denied by the Agent policy ',
            ],
          },
        })),
      })

      expect(decision.status).toBe(status)
      expect(decision.disabled).toBe(status === 'unsupported')
      expect(decision.reason).toBe(
        'Upgrade the connected Agent / Action platform.pods.delete is denied by the Agent policy',
      )
      expect(decision.reason).not.toContain('Pod 删除')
      expect(decision.notes).toContain('Action platform.pods.delete is denied by the Agent policy')
    },
  )

  it('localizes an available Agent capability only when backend diagnostics are absent', () => {
    const decision = evaluateClusterCapability({
      connectionMode: 'agent',
      key: 'workload.mutations',
      localeCode: 'zh_CN',
      matrix: matrix.map((entry) => ({ ...entry, agent: { status: 'available' } })),
    })

    expect(decision.status).toBe('available')
    expect(decision.disabled).toBe(false)
    expect(decision.reason).toContain('Pod 删除')
  })

  it.each([
    ['zh_CN', 'partial', '当前集群连接模式仅部分支持该能力。'],
    [
      'en_US',
      'partial',
      'The current cluster connection mode only partially supports this capability.',
    ],
    ['zh_CN', 'unsupported', '当前集群连接模式暂不支持该操作。'],
    [
      'en_US',
      'unsupported',
      'The current cluster connection mode does not support this operation.',
    ],
  ] as const)(
    'uses a truthful %s %s fallback without backend diagnostics',
    (localeCode, status, reason) => {
      const decision = evaluateClusterCapability({
        connectionMode: 'agent',
        key: 'workload.mutations',
        localeCode,
        matrix: matrix.map((entry) => ({ ...entry, agent: { status, reason: ' ', notes: [' '] } })),
      })

      expect(decision.reason).toBe(reason)
      expect(decision.disabled).toBe(status === 'unsupported')
      expect(decision.notes).toEqual([])
    },
  )

  it.each([
    ['available', 'Direct backend supports this capability'],
    ['partial', 'Direct backend supports only namespaced resources'],
  ] as const)('preserves direct %s backend reasons in Chinese', (status, reason) => {
    const decision = evaluateClusterCapability({
      connectionMode: 'direct',
      key: 'direct.capability',
      localeCode: 'zh_CN',
      matrix: [
        {
          key: 'direct.capability',
          label: 'Direct capability',
          category: 'configuration',
          riskLevel: 'read',
          requiresApproval: false,
          direct: { status, reason },
          agent: { status: 'unsupported' },
        },
      ],
    })

    expect(decision.reason).toBe(reason)
  })

  it('disables unknown capabilities until the matrix is available', () => {
    const decision = evaluateClusterCapability({
      connectionMode: 'agent',
      key: 'missing.capability',
      localeCode: 'zh_CN',
      matrix,
    })

    expect(decision.status).toBe('unknown')
    expect(decision.disabled).toBe(true)
    expect(decision.reason).toContain('暂时无法确认')
    expect(decision.requiredScopes).toEqual([])
    expect(decision.requiresApproval).toBe(false)
  })

  it('disables cached capabilities when the capability query fails', () => {
    const decision = evaluateClusterCapability({
      connectionMode: 'direct_kubeconfig',
      key: 'workload.mutations',
      localeCode: 'en_US',
      matrix,
      isError: true,
    })

    expect(decision.status).toBe('unknown')
    expect(decision.disabled).toBe(true)
    expect(decision.reason).toContain('Unable to confirm')
    expect(decision.reason).not.toContain('deployment restart')
    expect(decision.notes).toEqual([])
  })

  it('disables actions while capability data is loading', () => {
    const decision = evaluateClusterCapability({
      connectionMode: 'agent',
      key: 'workload.mutations',
      localeCode: 'en_US',
      matrix,
      isLoading: true,
    })

    expect(decision.disabled).toBe(true)
    expect(decision.isLoading).toBe(true)
    expect(decision.reason).toContain('Checking')
  })

  it('does not treat an unrecognized connection mode as direct access', () => {
    const decision = evaluateClusterCapability({
      connectionMode: 'future-mode',
      key: 'workload.mutations',
      localeCode: 'en_US',
      matrix,
    })

    expect(decision.status).toBe('unknown')
    expect(decision.disabled).toBe(true)
  })
})

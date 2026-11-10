import { expect, it } from 'vitest'
import {
  canonicalAgentProviderId,
  agentUnavailableReason,
  agentConnectionUnavailableReason,
  preferredExternalAgent,
  taskAgentUnavailableReason,
} from './agent-selection'
import type { WorkbenchAgentProvider } from './types'
const provider: WorkbenchAgentProvider = {
  id: 'hermes',
  kind: 'hermes-api',
  name: 'Hermes',
  enabled: true,
  capabilities: ['general'],
  runtimeStatus: { state: 'ready', queuedRuns: 0, runningRuns: 0, recentFailures: 0 },
}
it('keeps connection readiness separate from task capability and personal environment', () => {
  expect(agentConnectionUnavailableReason(undefined)).toBe('助手未启用')
  expect(agentConnectionUnavailableReason({ ...provider, enabled: false })).toBe('助手未启用')
  expect(agentConnectionUnavailableReason({ ...provider, capabilities: ['root_cause'] })).toBe('')
  expect(
    agentConnectionUnavailableReason({ ...provider, id: 'internal', runtimeStatus: undefined }),
  ).toBe('')
  expect(agentConnectionUnavailableReason({ ...provider, runtimeStatus: undefined })).toBe(
    '运行器尚未就绪',
  )
  expect(
    agentConnectionUnavailableReason({
      ...provider,
      runtimeStatus: { ...provider.runtimeStatus!, state: 'stale' },
    }),
  ).toBe('运行器尚未就绪')
})
it('selects only a ready external provider supporting the requested mode', () => {
  expect(preferredExternalAgent([{ ...provider, id: 'internal', default: true }, provider])).toBe(
    provider,
  )
  expect(preferredExternalAgent([{ ...provider, enabled: false }])).toBeUndefined()
  expect(preferredExternalAgent([{ ...provider, runtimeStatus: undefined }])).toBeUndefined()
  expect(preferredExternalAgent([provider], 'root_cause')).toBeUndefined()
  expect(agentUnavailableReason(provider)).toBe('')
})
it('checks every requested capability and rejects unsupported combinations or query parameters', () => {
  const external = { ...provider, capabilities: ['performance', 'trace'] }
  expect(taskAgentUnavailableReason(external, ['performance'])).toBe('')
  expect(taskAgentUnavailableReason(external, ['performance', 'trace'])).toContain('内置助手')
  expect(taskAgentUnavailableReason(external, ['performance'], true)).toContain('查询参数')
  expect(taskAgentUnavailableReason(external, ['root_cause'])).toBe('不支持当前任务')
  expect(
    taskAgentUnavailableReason({ ...external, id: 'internal' }, ['performance', 'trace'], true),
  ).toBe('')
})

it('canonicalizes new default requests while preserving explicit Hermes providers', () => {
  for (const id of [undefined, 'internal', 'native', 'dsh'])
    expect(canonicalAgentProviderId(id)).toBe('dsh')
  expect(canonicalAgentProviderId('hermes')).toBe('hermes')
})

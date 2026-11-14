import { isLegacyAgentRun } from './agent-run-replay'
import { describe, expect, it } from 'vitest'
import {
  agentRunReplayMessage,
  isFinalAgentRunMessage,
  usesRunEventJournal,
  isRunningAgentRun,
} from './agent-run-replay'
import { createWorkbenchStreamState } from './stream'
import type { WorkbenchAgentRun } from './types'

describe.each(['native', 'dsh'])('durable %s replay', (providerKind) => {
  const run: WorkbenchAgentRun = {
    id: 'native',
    sessionId: 'session',
    providerId: 'internal',
    providerKind,
    capabilityId: 'general',
    status: 'queued',
    queuedAt: '2026-10-08T00:00:00Z',
  }
  it('keeps a queued reply after the user request before a worker starts', () => {
    expect(usesRunEventJournal(run)).toBe(true)
    expect(isRunningAgentRun(run)).toBe(providerKind !== 'native')
    expect(
      new Date(agentRunReplayMessage(run, createWorkbenchStreamState()).createdAt).getTime(),
    ).toBeGreaterThan(new Date(run.queuedAt!).getTime())
  })
  it('keeps progress out of reply text and preserves streamed content while cancelling', () => {
    const state = createWorkbenchStreamState()
    const message = agentRunReplayMessage({ ...run, status: 'running', phase: 'cancelling' }, state)
    expect(message.content).toBe('')
    expect(message.metadata?.agentPhase).toBe('cancelling')
    expect(message.deliveryStatus).toBe(providerKind === 'native' ? 'success' : 'loading')
    expect(message.metadata?.executionReadOnly).toBe(providerKind === 'native' ? true : undefined)
    expect(message.metadata?.agentStatus).toMatchObject({ status: 'running' })
    state.message.content = '已经输出的正文'
    expect(
      agentRunReplayMessage({ ...run, status: 'running', phase: 'cancelling' }, state).content,
    ).toBe('已经输出的正文')
  })
  it('does not mistake the request or a running response for a final projection', () => {
    const message = {
      id: 'request',
      sessionId: 'session',
      role: 'user' as const,
      content: 'Question',
      createdAt: run.queuedAt!,
      metadata: { agentRunId: run.id },
    }
    expect(isFinalAgentRunMessage(message)).toBe(false)
    expect(
      isFinalAgentRunMessage({
        ...message,
        role: 'assistant',
        metadata: {
          agentStatus: { providerId: 'internal', providerKind: 'native', status: 'running' },
        },
      }),
    ).toBe(false)
    expect(
      isFinalAgentRunMessage({
        ...message,
        role: 'system',
        metadata: {
          agentStatus: { providerId: 'internal', providerKind: 'native', status: 'canceled' },
        },
      }),
    ).toBe(true)
  })
})

it('keeps Native checkpoint history read-only without marking dsh external runs as legacy', () => {
  expect(
    isLegacyAgentRun({ providerKind: 'native', executionOwner: 'native' } as WorkbenchAgentRun),
  ).toBe(true)
  expect(
    isLegacyAgentRun({ providerKind: 'dsh', executionOwner: 'external' } as WorkbenchAgentRun),
  ).toBe(false)
})

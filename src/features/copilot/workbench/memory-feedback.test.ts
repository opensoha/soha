import { expect, it } from 'vitest'
import { hasMemoryDegradation } from './memory-feedback'
import { createWorkbenchStreamState, reduceWorkbenchStreamState } from './stream'
import { streamMessageMetadata } from './stream-presentation'

it('preserves a current run memory degradation through status updates and clears for another run', () => {
  const event = {
    id: '1',
    type: 'agent.status' as const,
    sessionId: 'session',
    runId: 'run',
    sequence: 1,
    createdAt: '2026-11-08T00:00:00Z',
    providerId: 'internal',
    providerKind: 'dsh' as const,
    status: 'running' as const,
    metadata: { memoryStatus: 'degraded' },
  }
  let state = reduceWorkbenchStreamState(createWorkbenchStreamState(), event)
  state = reduceWorkbenchStreamState(state, { ...event, id: '2', metadata: undefined })
  expect(hasMemoryDegradation(streamMessageMetadata(undefined, state))).toBe(true)
  state = reduceWorkbenchStreamState(state, {
    ...event,
    id: '3',
    runId: 'next',
    metadata: undefined,
  })
  expect(hasMemoryDegradation(streamMessageMetadata(undefined, state))).toBe(false)
})

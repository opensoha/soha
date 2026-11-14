import { beforeEach, describe, expect, it, vi } from 'vitest'

const apiMocks = vi.hoisted(() => ({
  sessions: {
    all: vi.fn(),
    detail: vi.fn(),
    messages: vi.fn(),
    create: vi.fn(),
    patch: vi.fn(),
    archive: vi.fn(),
    createInspectionTask: vi.fn(),
  },
  catalog: vi.fn(),
  agentRuns: {
    all: vi.fn(),
    session: vi.fn(),
  },
}))

vi.mock('./api', () => ({ workbenchApi: apiMocks }))

import { workbenchMutations } from './mutations'
import { workbenchQueries } from './queries'

describe('workbench data options', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('binds query keys to the canonical API delegates', async () => {
    const sessions = workbenchQueries.sessions.all()
    const detail = workbenchQueries.sessions.detail('session-1')
    const messages = workbenchQueries.sessions.messages('session-1')
    const catalog = workbenchQueries.catalog()
    const agentRuns = workbenchQueries.agentRuns.session('session-1')

    await sessions.queryFn?.({} as never)
    await detail.queryFn?.({} as never)
    await messages.queryFn?.({} as never)
    await catalog.queryFn?.({} as never)
    await agentRuns.queryFn?.({} as never)

    expect(detail.queryKey).toEqual(['copilot-workbench-session-detail', 'session-1'])
    expect(messages.queryKey).toEqual(['copilot-workbench-messages', 'session-1'])
    expect(agentRuns.queryKey).toEqual(['copilot-agent-runs', 'session-1'])
    expect(apiMocks.sessions.all).toHaveBeenCalledOnce()
    expect(apiMocks.sessions.detail).toHaveBeenCalledWith('session-1')
    expect(apiMocks.sessions.messages).toHaveBeenCalledWith('session-1')
    expect(apiMocks.catalog).toHaveBeenCalledOnce()
    expect(apiMocks.agentRuns.session).toHaveBeenCalledWith('session-1')
  })

  it('disables session-scoped queries without a session id', () => {
    expect(workbenchQueries.sessions.all(false).enabled).toBe(false)
    expect(workbenchQueries.sessions.detail().enabled).toBe(false)
    expect(workbenchQueries.sessions.messages().enabled).toBe(false)
    expect(workbenchQueries.agentRuns.session().enabled).toBe(false)
  })

  it('delegates mutation inputs without changing their contracts', async () => {
    const createInput = {
      title: '',
      mode: 'general' as const,
      agentProviderId: 'internal',
      scope: {},
      tags: [],
    }
    const patchInput = { sessionId: 'session-1', body: { mode: 'trace' } }

    await workbenchMutations.sessions.create().mutationFn?.(createInput, {} as never)
    await workbenchMutations.sessions.patch().mutationFn?.(patchInput, {} as never)
    await workbenchMutations.sessions.archive().mutationFn?.('session-1', {} as never)

    expect(apiMocks.sessions.create).toHaveBeenCalledWith(createInput)
    expect(apiMocks.sessions.patch).toHaveBeenCalledWith(patchInput)
    expect(apiMocks.sessions.archive).toHaveBeenCalledWith('session-1')
  })
})

import { describe, expect, it } from 'vitest'
import { QueryClient } from '@tanstack/react-query'

import { workbenchKeys } from './keys'

describe('workbenchKeys', () => {
  it('invalidates Heya request lists without sharing owner-specific access receipts', async () => {
    const client = new QueryClient()
    const requests = workbenchKeys.personalAgentAccessRequests('one')
    const access = workbenchKeys.personalAgentAccess('one')
    client.setQueryData(requests, [])
    client.setQueryData(access, {})
    await client.invalidateQueries({ queryKey: workbenchKeys.personalAgentAccessRequests() })
    expect(client.getQueryState(requests)?.isInvalidated).toBe(true)
    expect(client.getQueryState(access)?.isInvalidated).toBe(false)
    expect(access).not.toEqual(workbenchKeys.personalAgentAccess('two'))
    client.clear()
  })
  it('invalidates account lists with a scope prefix without invalidating other scopes', async () => {
    const client = new QueryClient()
    const first = workbenchKeys.sessions.all(true, 'one')
    const second = workbenchKeys.sessions.all(true, 'two')
    const ordinary = workbenchKeys.sessions.all(false, 'one')
    for (const key of [first, second, ordinary]) client.setQueryData(key, [])
    await client.invalidateQueries({ queryKey: workbenchKeys.sessions.all(true) })
    expect(client.getQueryState(first)?.isInvalidated).toBe(true)
    expect(client.getQueryState(second)?.isInvalidated).toBe(true)
    expect(client.getQueryState(ordinary)?.isInvalidated).toBe(false)
    client.clear()
  })
  it('separates workspace and account caches', () => {
    expect(workbenchKeys.sessions.all(false, 'one')).not.toEqual(
      workbenchKeys.sessions.all(true, 'one'),
    )
    expect(workbenchKeys.sessions.all(true, 'one')).not.toEqual(
      workbenchKeys.sessions.all(true, 'two'),
    )
    expect(workbenchKeys.sessions.detail('id', 'one')).not.toEqual(
      workbenchKeys.sessions.detail('id', 'two'),
    )
    expect(workbenchKeys.sessions.messages('id', 'one')).not.toEqual(
      workbenchKeys.sessions.messages('id', 'two'),
    )
  })
  it('preserves the existing session query tuples', () => {
    expect(workbenchKeys.sessions.all()).toEqual(['copilot-workbench-sessions'])
    expect(workbenchKeys.sessions.detail('session-1')).toEqual([
      'copilot-workbench-session-detail',
      'session-1',
    ])
    expect(workbenchKeys.sessions.messages('session-1')).toEqual([
      'copilot-workbench-messages',
      'session-1',
    ])
  })

  it('preserves catalog and agent run query tuples', () => {
    expect(workbenchKeys.catalog()).toEqual(['copilot-workbench-catalog'])
    expect(workbenchKeys.agentRuns.all()).toEqual(['copilot-agent-runs'])
    expect(workbenchKeys.agentRuns.session('session-1')).toEqual([
      'copilot-agent-runs',
      'session-1',
    ])
  })
})

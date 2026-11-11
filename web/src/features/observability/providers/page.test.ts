import { describe, expect, it } from 'vitest'
import { providerSignalCounts } from './page'

describe('observability provider catalog', () => {
  it('counts each signal exposed by a provider', () => {
    expect(
      providerSignalCounts([
        {
          providerKey: 'a',
          displayName: 'A',
          protocolVersion: 'v1',
          runtimeMode: 'builtin',
          builtIn: true,
          status: 'supported',
          signals: ['logs', 'traces'],
          capabilities: [],
        },
        {
          providerKey: 'b',
          displayName: 'B',
          protocolVersion: 'v1',
          runtimeMode: 'builtin',
          builtIn: true,
          status: 'supported',
          signals: ['logs'],
          capabilities: [],
        },
      ]),
    ).toEqual({ logs: 2, traces: 1 })
  })
})

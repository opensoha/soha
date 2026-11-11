import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { observabilityKeys } from '../keys'
import { observabilityLogQueries } from './queries'

const mocks = vi.hoisted(() => ({
  queryLogs: vi.fn(),
  listLogDataSources: vi.fn(),
  logTargetKey: vi.fn(() => 'cluster:cluster-a:apps'),
}))
vi.mock('./api', () => mocks)

describe('observability log query options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('binds the normalized query and cluster to the canonical key', async () => {
    const query = { selector: { namespace: 'apps' }, sourceMode: 'runtime' as const }
    const page = { entries: [], partial: false, truncated: false, scopeRestricted: false }
    mocks.queryLogs.mockResolvedValueOnce(page)
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const target = { kind: 'cluster' as const, clusterId: 'cluster-a', namespace: 'apps' }

    await expect(
      client.fetchQuery(observabilityLogQueries.snapshot(target, query)),
    ).resolves.toEqual(page)
    expect(
      client.getQueryData(observabilityKeys.logs.snapshot('cluster:cluster-a:apps', query)),
    ).toEqual(page)
    expect(mocks.queryLogs).toHaveBeenCalledWith(target, query, expect.any(AbortSignal))
  })

  it('loads log data sources with the canonical observability key', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mocks.listLogDataSources.mockResolvedValueOnce([{ id: 'ds-1' }])
    await expect(client.fetchQuery(observabilityLogQueries.dataSources())).resolves.toEqual([
      { id: 'ds-1' },
    ])
    expect(client.getQueryData(observabilityKeys.logs.dataSources())).toEqual([{ id: 'ds-1' }])
  })
})

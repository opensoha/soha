import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  buildLogStreamURL,
  buildClusterLogStreamURL,
  createLogDataSource,
  issueLogStreamTicket,
  issueClusterLogStreamTicket,
  listLogDataSources,
  queryLogs,
  queryClusterLogs,
  updateLogDataSource,
  validateLogDataSource,
} from './api'

const mocks = vi.hoisted(() => ({
  post: vi.fn(),
  postWithSignal: vi.fn(),
  get: vi.fn(),
  put: vi.fn(),
  buildSameOriginStreamURL: vi.fn(
    () => new URL('ws://soha.local/api/v1/clusters/cluster-a/logs/stream'),
  ),
}))

vi.mock('@/services/api-client', () => ({
  api: { get: mocks.get, post: mocks.post, postWithSignal: mocks.postWithSignal, put: mocks.put },
}))
vi.mock('@/features/auth', () => ({ buildSameOriginStreamURL: mocks.buildSameOriginStreamURL }))

describe('observability log api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('queries the cluster endpoint with cancellation', async () => {
    const controller = new AbortController()
    const query = { selector: { namespace: 'apps' }, sourceMode: 'runtime' as const }
    const page = { entries: [], partial: false, truncated: false, scopeRestricted: false }
    mocks.postWithSignal.mockResolvedValueOnce({ data: page })

    await expect(queryClusterLogs('cluster/a', query, controller.signal)).resolves.toEqual(page)
    expect(mocks.postWithSignal).toHaveBeenCalledWith(
      '/clusters/cluster%2Fa/logs/query',
      query,
      controller.signal,
    )
  })

  it('issues a query-bound ticket and builds the websocket URL', async () => {
    const query = { selector: { namespace: 'apps' } }
    mocks.post.mockResolvedValueOnce({
      data: { ticket: 'one-use', expiresAt: '2026-07-31T02:01:00Z' },
    })

    await expect(issueClusterLogStreamTicket('cluster-a', query)).resolves.toMatchObject({
      ticket: 'one-use',
    })
    expect(mocks.post).toHaveBeenCalledWith('/clusters/cluster-a/logs/stream-ticket', query)
    expect(buildClusterLogStreamURL('cluster-a', 'one-use')).toBe(
      'ws://soha.local/api/v1/clusters/cluster-a/logs/stream?stream_ticket=one-use',
    )
  })

  it('uses the delivery environment log endpoints', async () => {
    const target = {
      kind: 'delivery' as const,
      applicationId: 'app/a',
      environmentId: 'binding/1',
      namespace: '',
    }
    const query = { selector: { namespace: '' }, sourceMode: 'runtime' as const }
    const controller = new AbortController()
    mocks.postWithSignal.mockResolvedValueOnce({ data: { entries: [] } })
    mocks.post.mockResolvedValueOnce({
      data: { ticket: 'delivery-ticket', expiresAt: '2026-07-31T02:01:00Z' },
    })

    await queryLogs(target, query, controller.signal)
    await issueLogStreamTicket(target, query)
    buildLogStreamURL(target, 'delivery-ticket')

    expect(mocks.postWithSignal).toHaveBeenCalledWith(
      '/delivery/applications/app%2Fa/environments/binding%2F1/logs/query',
      query,
      controller.signal,
    )
    expect(mocks.post).toHaveBeenCalledWith(
      '/delivery/applications/app%2Fa/environments/binding%2F1/logs/stream-ticket',
      query,
    )
    expect(mocks.buildSameOriginStreamURL).toHaveBeenCalledWith(
      '/api/v1/delivery/applications/app%2Fa/environments/binding%2F1/logs/stream',
      'ws',
    )
  })

  it('uses the observability data-source endpoints', async () => {
    const input = {
      name: 'logs',
      backendType: 'loki' as const,
      enabled: true,
      config: { endpoint: 'https://logs.example', labelKeys: {} },
    }
    const item = { id: 'ds/1', ...input }
    mocks.get.mockResolvedValueOnce({ data: [item] })
    mocks.post.mockResolvedValue({ data: item })
    mocks.put.mockResolvedValueOnce({ data: item })

    await expect(listLogDataSources()).resolves.toEqual([item])
    await createLogDataSource(input)
    await updateLogDataSource({ id: 'ds/1', input })
    await validateLogDataSource('ds/1')

    expect(mocks.get).toHaveBeenCalledWith('/observability/data-sources')
    expect(mocks.post).toHaveBeenCalledWith('/observability/data-sources', input)
    expect(mocks.put).toHaveBeenCalledWith('/observability/data-sources/ds%2F1', input)
    expect(mocks.post).toHaveBeenCalledWith('/observability/data-sources/ds%2F1/validate')
  })
})

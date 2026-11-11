import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  getService,
  getServiceTopology,
  listMetricCatalog,
  listServices,
  queryMetrics,
  queryTraces,
} from './api'

const post = vi.hoisted(() => vi.fn())
const get = vi.hoisted(() => vi.fn())
const getEnvelope = vi.hoisted(() => vi.fn())
vi.mock('@/services/api-client', () => ({ api: { get, getEnvelope, post } }))

describe('observability signal api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses the normalized Core query gateways', async () => {
    const metricInput = {
      metricKey: 'cpu_usage' as const,
      timeFrom: '2026-08-03T00:00:00Z',
      timeTo: '2026-08-03T00:15:00Z',
      stepSeconds: 60,
    }
    const traceInput = {
      timeFrom: '2026-08-03T00:00:00Z',
      timeTo: '2026-08-03T00:15:00Z',
      minDurationMs: 100,
      limit: 100,
    }
    post.mockResolvedValueOnce({ data: { series: [] } })
    post.mockResolvedValueOnce({ data: { services: [], spans: [] } })

    await queryMetrics(metricInput)
    await queryTraces(traceInput)

    expect(post).toHaveBeenNthCalledWith(1, '/observability/metrics/query', metricInput)
    expect(post).toHaveBeenNthCalledWith(2, '/observability/traces/query', traceInput)
  })

  it('uses the public catalog and scoped SkyWalking service endpoints', async () => {
    const query = {
      clusterId: 'cluster-a',
      namespace: 'apps',
      timeFrom: '2026-08-03T00:00:00Z',
      timeTo: '2026-08-03T00:15:00Z',
    }
    get.mockResolvedValue({ data: [] })
    getEnvelope.mockResolvedValue({ items: [], meta: { state: 'empty' } })

    await listMetricCatalog()
    await listServices(query)
    await getService('service/id', query)
    await getServiceTopology('service/id', query)

    expect(get).toHaveBeenCalledWith('/observability/metrics/catalog')
    expect(getEnvelope).toHaveBeenNthCalledWith(
      1,
      '/observability/services?clusterId=cluster-a&namespace=apps&timeFrom=2026-08-03T00%3A00%3A00Z&timeTo=2026-08-03T00%3A15%3A00Z',
    )
    expect(getEnvelope).toHaveBeenNthCalledWith(
      2,
      expect.stringContaining('/observability/services/service%2Fid?'),
    )
    expect(getEnvelope).toHaveBeenNthCalledWith(
      3,
      expect.stringContaining('/observability/services/service%2Fid/topology?'),
    )
  })
})

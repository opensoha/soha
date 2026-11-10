/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { expect, it, vi } from 'vitest'
import { PodMetricsDetail } from './service-pod-workspace'
import { podQueries } from '@/features/platform'
import type { ResourceMetrics } from '@/types'

const state = vi.hoisted(() => ({ read: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api: { get: state.read } }))
vi.mock('@/components/resource-metrics-panel', () => ({
  ResourceMetricsPanel: ({ data, loading }: { data?: ResourceMetrics; loading: boolean }) => (
    <div>{loading ? 'reading' : data?.series?.[0]?.label}</div>
  ),
}))

it('partitions name-based metric caches by Pod UID and hides stale samples during a fresh read', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: 30_000, retry: false } },
  })
  const scope = { clusterId: 'cluster', namespace: 'team' }
  const base = podQueries.metrics(scope, 'same-pod', 60).queryKey
  const metrics = (label: string): ResourceMetrics => ({
    configured: true,
    source: 'prometheus',
    stepSeconds: 30,
    resourceKind: 'Pod',
    resourceName: 'same-pod',
    namespace: 'team',
    rangeMinutes: 60,
    generatedAt: '2026-10-22T00:00:00Z',
    series: [{ key: 'cpu', label, unit: 'cores', latest: 1, points: [] }],
  })
  client.setQueryData(base, metrics('old-name-cache'))
  client.setQueryData(
    podQueries.metrics(scope, 'same-pod', 60, 'new-uid').queryKey,
    metrics('stale-new-uid-cache'),
  )
  let finish: (value: { data: ResourceMetrics }) => void = () => {}
  state.read.mockReturnValue(
    new Promise((resolve) => {
      finish = resolve
    }),
  )
  try {
    await act(async () =>
      root.render(
        <QueryClientProvider client={client}>
          <PodMetricsDetail
            row={{
              pod: { name: 'same-pod', namespace: 'team', uid: 'new-uid' },
              workload: { clusterId: 'cluster' },
            }}
            range={60}
            onRangeChange={() => {}}
          />
        </QueryClientProvider>,
      ),
    )
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20))
    })
    expect(state.read).toHaveBeenCalledTimes(1)
    expect(host.textContent).not.toContain('old-name-cache')
    expect(host.textContent).not.toContain('stale-new-uid-cache')
    await act(async () => finish({ data: metrics('fresh-response') }))
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20))
    })
    expect(host.textContent).toContain('fresh-response')
  } finally {
    await act(async () => root.unmount())
    client.clear()
    host.remove()
    vi.unstubAllGlobals()
  }
})

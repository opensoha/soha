import { QueryClient, type QueryKey, type QueryObserverOptions } from '@tanstack/react-query'
import { expect, it } from 'vitest'
import { clusterQueries } from '../clusters/queries'
import { crdQueries } from '../extensions/crds/queries'
import { helmQueries } from '../extensions/helm/queries'
import { platformOverviewQueries } from '../overview/queries'

function expectStopsOnFailure<T, TKey extends QueryKey>(
  options: QueryObserverOptions<T, Error, T, T, TKey>,
  data: unknown,
  interval: number,
) {
  const client = new QueryClient()
  const query = client.getQueryCache().build(client, options)
  const cadence = options.refetchInterval
  if (typeof cadence !== 'function') throw new Error('Expected state-aware polling')
  query.setData(data as T)
  expect(cadence(query)).toBe(interval)
  query.setState({ status: 'error', error: new Error('Read unavailable') })
  expect(cadence(query)).toBe(false)
  client.clear()
}

it('pauses active Platform polling after a failed read until an explicit retry succeeds', () => {
  expectStopsOnFailure(crdQueries.catalog('cluster-a'), [{ deletingAt: '2026-10-16' }], 3000)
  const collection = clusterQueries.logCollection({ clusterId: 'cluster-a', namespace: null })
  expectStopsOnFailure(collection, { status: 'installing' }, 2000)
  const summary = platformOverviewQueries.monitoringSummary(true)
  expectStopsOnFailure(summary, {}, 15000)
  const release = helmQueries.installProgress(
    { clusterId: 'cluster-a', name: 'api', namespace: 'monitoring' },
    true,
  )
  expectStopsOnFailure(release, {}, 5000)
})

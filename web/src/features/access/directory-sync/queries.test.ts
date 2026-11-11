import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'
import { directorySyncKeys, directorySyncQueries } from './queries'

describe('directory sync queries', () => {
  it('keeps runtime status and events under connection-scoped keys', () => {
    expect(directorySyncKeys.runtimeStatus('dir-1')).toEqual([
      'access',
      'directory-sync',
      'runtime-status',
      'dir-1',
    ])
    expect(directorySyncKeys.events('dir-1')).toEqual([
      'access',
      'directory-sync',
      'events',
      'dir-1',
    ])
    expect(directorySyncQueries.runtimeStatus('').enabled).toBe(false)
    for (const options of [
      directorySyncQueries.events('dir-1'),
      directorySyncQueries.runtimeStatus('dir-1'),
    ]) {
      const client = new QueryClient()
      client.setQueryData(options.queryKey, [])
      const query = client.getQueryCache().find({ queryKey: options.queryKey })!
      expect(
        typeof options.refetchInterval === 'function' && options.refetchInterval(query as never),
      ).toBe(5000)
      query.setState({ status: 'error' })
      expect(
        typeof options.refetchInterval === 'function' && options.refetchInterval(query as never),
      ).toBe(false)
      client.clear()
    }
  })
})

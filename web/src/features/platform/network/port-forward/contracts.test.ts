import { beforeEach, describe, expect, it, vi } from 'vitest'
import { toScopeKey } from '@/types'
import { listPortForwards, registerPortForward, stopPortForward } from './api'
import { portForwardKeys } from './keys'

const apiMocks = vi.hoisted(() => ({ delete: vi.fn(), get: vi.fn(), post: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('Port Forward contracts', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps list and register responses without sending scope in the payload', async () => {
    const scope = toScopeKey('cluster-a', null)
    const session = { sessionId: 'session-a' }
    apiMocks.get.mockResolvedValue({ data: [session] })
    apiMocks.post.mockResolvedValue({ data: session })
    const draft = {
      scope,
      targetKind: 'Pod',
      targetName: 'api',
      namespace: 'team-a',
      localPort: 8080,
      remotePort: 80,
    }

    await expect(listPortForwards(scope)).resolves.toEqual([session])
    await expect(registerPortForward(draft)).resolves.toBe(session)
    expect(apiMocks.post).toHaveBeenCalledWith('/clusters/cluster-a/network/port-forwards', {
      targetKind: 'Pod',
      targetName: 'api',
      namespace: 'team-a',
      localPort: 8080,
      remotePort: 80,
    })
    expect(portForwardKeys.list(scope)).toEqual([
      'platform',
      'network',
      'port-forwards',
      { clusterId: 'cluster-a', namespace: null },
    ])
  })

  it('encodes the stopped session id', async () => {
    const scope = toScopeKey('cluster-a', null)
    apiMocks.delete.mockResolvedValue(undefined)
    await stopPortForward({ scope, sessionId: 'session/a' })
    expect(apiMocks.delete).toHaveBeenCalledWith(
      '/clusters/cluster-a/network/port-forwards/session%2Fa',
    )
  })
})

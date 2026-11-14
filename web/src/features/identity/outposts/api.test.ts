import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createIdentityOutpost,
  deleteIdentityOutpost,
  getIdentityOutpost,
  getIdentityOutpostRuntime,
  mutateIdentityOutpostRuntime,
  listIdentityOutposts,
  rotateIdentityOutpostToken,
  updateIdentityOutpost,
} from './api'
import type { IdentityOutpost, IdentityOutpostInput } from './types'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const outpost: IdentityOutpost = {
  id: 'edge/id',
  name: 'Edge Grafana',
  mode: 'embedded',
  token: 'shown-once',
  status: 'offline',
  runtimeStatus: 'available',
  configurationVersion: 0,
  metadata: { region: 'cn-east' },
  createdAt: '2026-07-10T00:00:00Z',
  updatedAt: '2026-07-10T00:00:00Z',
}

const input: IdentityOutpostInput = {
  name: outpost.name,
  mode: outpost.mode,
  endpoint: '',
  status: outpost.status,
  version: '',
  metadata: outpost.metadata ?? {},
}

describe('identity outposts api', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('normalizes list filters, supports pagination, and unwraps collection data', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: [outpost] })

    await expect(
      listIdentityOutposts({
        mode: 'embedded',
        status: 'offline',
        limit: 25.9,
        offset: 50,
      }),
    ).resolves.toEqual([outpost])

    expect(apiMocks.get).toHaveBeenCalledWith(
      '/identity/outposts?mode=embedded&status=offline&limit=25&offset=50',
    )
  })

  it('tolerates an empty list envelope', async () => {
    apiMocks.get.mockResolvedValueOnce({})

    await expect(listIdentityOutposts()).resolves.toEqual([])
    expect(apiMocks.get).toHaveBeenCalledWith('/identity/outposts')
  })

  it('unwraps detail/create/update values and encodes trimmed ids', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: outpost })
    apiMocks.post.mockResolvedValueOnce({ data: outpost })
    apiMocks.put.mockResolvedValueOnce({ data: outpost })

    await expect(getIdentityOutpost(' edge/id ')).resolves.toBe(outpost)
    await expect(createIdentityOutpost(input)).resolves.toBe(outpost)
    await expect(updateIdentityOutpost({ outpostId: ' edge/id ', input })).resolves.toBe(outpost)

    expect(apiMocks.get).toHaveBeenCalledWith('/identity/outposts/edge%2Fid')
    expect(apiMocks.post).toHaveBeenCalledWith('/identity/outposts', input)
    expect(apiMocks.put).toHaveBeenCalledWith('/identity/outposts/edge%2Fid', input)
  })

  it('keeps delete transport details out of the domain return value', async () => {
    apiMocks.delete.mockResolvedValueOnce({ data: { status: 'ok' } })

    await expect(deleteIdentityOutpost(' edge/id ')).resolves.toBeUndefined()
    expect(apiMocks.delete).toHaveBeenCalledWith('/identity/outposts/edge%2Fid')
  })

  it('uses the runtime endpoint and preserves stable intent fields for retry', async () => {
    const runtime = { owner: 'external', generation: 4 }
    apiMocks.get.mockResolvedValueOnce({ data: runtime })
    expect(await getIdentityOutpostRuntime(' edge/id ')).toBe(runtime)
    expect(apiMocks.get).toHaveBeenCalledWith('/identity/outposts/edge%2Fid/runtime')
    const bind = {
      outpostId: ' edge/id ',
      kind: 'bind' as const,
      input: {
        slotId: 'approved',
        expectedGeneration: 4,
        requestId: 'stable',
        explicitAdoption: true,
      },
    }
    apiMocks.put
      .mockRejectedValueOnce(new Error('network'))
      .mockResolvedValueOnce({ data: { runtime } })
    await expect(mutateIdentityOutpostRuntime(bind)).rejects.toThrow('network')
    await expect(mutateIdentityOutpostRuntime(bind)).resolves.toEqual({ runtime })
    expect(apiMocks.put.mock.calls[0]).toEqual(apiMocks.put.mock.calls[1])
    const input = {
      action: 'stop' as const,
      expectedGeneration: 4,
      requestId: 'stop-id',
      drainTimeoutSeconds: 30,
    }
    apiMocks.post.mockResolvedValueOnce({ data: { runtime } })
    await mutateIdentityOutpostRuntime({ outpostId: 'edge/id', kind: 'control', input })
    expect(apiMocks.post).toHaveBeenCalledWith(
      '/identity/outposts/edge%2Fid/runtime/control',
      input,
    )
  })

  it('returns a rotated token only from the one-time response', async () => {
    apiMocks.post.mockResolvedValueOnce({ data: outpost })

    await expect(rotateIdentityOutpostToken(' edge/id ')).resolves.toBe(outpost)
    expect(apiMocks.post).toHaveBeenCalledWith('/identity/outposts/edge%2Fid/token/rotate')
  })
})

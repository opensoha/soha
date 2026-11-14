import { beforeEach, describe, expect, it, vi } from 'vitest'
import { listInstances } from './api'

const apiMocks = vi.hoisted(() => ({ getEnvelope: vi.fn() }))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('proxy runtime API', () => {
  beforeEach(() => vi.clearAllMocks())

  it('reads the instance list envelope and sends query filters', async () => {
    const instances = [{ id: 'proxy-mihomo', name: 'Mihomo' }]
    apiMocks.getEnvelope.mockResolvedValue({ items: instances })

    await expect(listInstances({ search: ' middleware ', engine: 'mihomo' })).resolves.toEqual(
      instances,
    )
    expect(apiMocks.getEnvelope).toHaveBeenCalledWith(
      '/network-access/proxy-instances?limit=200&search=middleware&engine=mihomo',
    )
  })
})
